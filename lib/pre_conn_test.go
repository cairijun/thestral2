package lib

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockTransForPreConn struct {
	mockDialCh chan *mockDial
	dialErrCh  chan error
}

type mockDial struct {
	address string
	svrConn net.Conn
	cliConn net.Conn
}

func newMockTransForPreConn() *mockTransForPreConn {
	return &mockTransForPreConn{
		mockDialCh: make(chan *mockDial, 100),
		dialErrCh:  make(chan error, 100),
	}
}

func (t *mockTransForPreConn) Dial(
	ctx context.Context, address string) (net.Conn, error) {
	select {
	case err := <-t.dialErrCh:
		return nil, err
	default:
		svrConn, cliConn := net.Pipe()
		t.mockDialCh <- &mockDial{
			address: address,
			svrConn: svrConn,
			cliConn: cliConn,
		}
		return cliConn, nil
	}
}

func (t *mockTransForPreConn) Listen(address string) (net.Listener, error) {
	panic("PreConnTransWrapper is a client-only transport")
}

func makePreConnWithMock(maxPoolSize int, lifetime string) (
	preConnTrans *PreConnTransWrapper,
	mockTrans *mockTransForPreConn,
	err error) {
	mockTrans = newMockTransForPreConn()
	preConnTrans, err = WrapAsPreConnTransport(mockTrans,
		PreConnConfig{
			MaxPoolSize: maxPoolSize,
			Lifetime:    lifetime,
		})
	return
}

func TestPreConnInvalidConfig(t *testing.T) {
	var err error
	_, _, err = makePreConnWithMock(-1, "")
	assert.Error(t, err)
	_, _, err = makePreConnWithMock(0, "invalid")
	assert.Error(t, err)
	_, _, err = makePreConnWithMock(1, "-1s")
	assert.Error(t, err)
}

func TestPreConnStarvationTriggerPreConn(t *testing.T) {
	mockTrans := newMockTransForPreConn()
	preConnTrans, err := WrapAsPreConnTransport(
		mockTrans, PreConnConfig{MaxPoolSize: 2})
	require.NoError(t, err)
	require.Empty(t, mockTrans.dialErrCh)
	addrs := []string{"addr1", "addr2", "addr3"}
	var wg sync.WaitGroup
	for _, addr := range addrs {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			_, err := preConnTrans.Dial(context.Background(), addr)
			require.NoError(t, err)
		}(addr)
	}
	wg.Wait()
	time.Sleep(100 * time.Millisecond)
	mockTrans.mockDialCh <- nil
	dialCounts := make(map[string]int)
	for dial := range mockTrans.mockDialCh {
		if dial == nil {
			break
		}
		dialCounts[dial.address]++
	}
	assert.Len(t, dialCounts, 3)
	for _, addr := range addrs {
		assert.True(t, dialCounts[addr] >= 3, "dial count not match: %s", addr)
	}
}

func TestPreConnLifetime(t *testing.T) {
	const maxPoolSize = 5
	preConnTrans, mockTrans, err := makePreConnWithMock(maxPoolSize, "400ms")
	require.NoError(t, err)
	// trigger a new preConnMgr
	_, err = preConnTrans.Dial(context.Background(), "addr")
	require.NoError(t, err)
	dial := <-mockTrans.mockDialCh
	require.Equal(t, "addr", dial.address)
	_ = dial.cliConn.Close()
	_ = dial.svrConn.Close()
	// wait till new preConns are established
	var dials [maxPoolSize]*mockDial
	for i := 0; i < maxPoolSize; i++ {
		dials[i] = <-mockTrans.mockDialCh
		require.Equal(t, "addr", dials[i].address)
	}
	assert.Empty(t, mockTrans.mockDialCh)
	// conns should still be open within lifetime
	time.Sleep(300 * time.Millisecond)
	isStillOpen := func(dial *mockDial) bool {
		const data = 'T'
		go dial.svrConn.Write([]byte{data})
		var buf [1]byte
		_, err := dial.cliConn.Read(buf[:])
		return err == nil && data == buf[0]
	}
	for _, dial := range dials {
		require.True(t, isStillOpen(dial))
	}
	// conns should be closed after lifetime
	time.Sleep(200 * time.Millisecond)
	for _, dial := range dials {
		require.False(t, isStillOpen(dial))
		_ = dial.svrConn.Close()
	}
	// there should be some new preConns
	require.Len(t, mockTrans.mockDialCh, idlePreConnPoolSize)
	for i := 0; i < idlePreConnPoolSize; i++ {
		dial := <-mockTrans.mockDialCh
		require.Equal(t, "addr", dial.address)
		require.True(t, isStillOpen(dial))
	}
}
