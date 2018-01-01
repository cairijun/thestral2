package lib

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

var gKCPServerConfig = &KCPConfig{
	Mode:     "fast2",
	Optimize: "send",
	FEC:      true,
	FECDist:  "10, 2",
}
var gKCPClientConfig = &KCPConfig{
	Mode:     "fast2",
	Optimize: "receive",
	FEC:      true,
}
var gTLSClientConfig = &TLSConfig{
	Cert: "../test_files/test.pem",
	Key:  "../test_files/test.key.pem",
	CAs:  []string{"../test_files/ca.pem"},
}
var gTLSServerConfig = &TLSConfig{
	Cert:         "../test_files/test.server.pem",
	Key:          "../test_files/test.server.key.pem",
	VerifyClient: true,
	ClientCAs:    []string{"../test_files/ca.pem"},
}

func TestTransportDefault(t *testing.T) {
	doTestWithTransConf(t, nil, nil)
}

func TestTransport(t *testing.T) {
	for _, compMethod := range []string{"", "lz4", "snappy", "deflate"} {
		for _, tls := range []bool{false, true} {
			for _, kcp := range []bool{false, true} {
				name := fmt.Sprintf(
					"compMethod-%s/tls-%v/kcp-%v", compMethod, tls, kcp)
				t.Run(name, func(t *testing.T) {
					svrConfig := &TransportConfig{Compression: compMethod}
					cliConfig := &TransportConfig{Compression: compMethod}

					if tls {
						svrConfig.TLS = gTLSServerConfig
						cliConfig.TLS = gTLSClientConfig
					}

					if kcp {
						svrConfig.KCP = gKCPServerConfig
						cliConfig.KCP = gKCPClientConfig
					}

					doTestWithTransConf(t, svrConfig, cliConfig)
				})
			}
		}
	}
}

func doTestWithTransConf(t *testing.T, svrConfig, cliConfig *TransportConfig) {
	svrTrans, err := CreateTransport(svrConfig)
	require.NoError(t, err)
	cliTrans, err := CreateTransport(cliConfig)
	require.NoError(t, err)

	address := "127.0.0.1:" + strconv.Itoa(50000+(rand.Intn(2048)))
	listener, err := svrTrans.Listen(address)
	require.NoError(t, err)

	exit := make(chan struct{})
	var svrWg sync.WaitGroup
	svrWg.Add(1)
	go runEchoServer(t, listener, exit, &svrWg)

	var cliWg sync.WaitGroup
	for i := 0; i < 10; i++ {
		cliWg.Add(1)

		go func() {
			defer cliWg.Done()
			client, err := cliTrans.Dial(context.Background(), address)
			require.NoError(t, err)
			defer client.Close() // nolint: errcheck
			_ = client.SetDeadline(time.Now().Add(30 * time.Second))

			for _, data := range getRandomData(16) {
				_, err := client.Write(data)
				require.NoError(t, err)

				buf := make([]byte, len(data))
				_, err = io.ReadFull(client, buf)
				require.NoError(t, err)

				assert.True(t, bytes.Equal(buf, data))
			}
		}()
	}

	cliWg.Wait()
	close(exit)
	time.Sleep(time.Millisecond * 50) // ensure kcpClose packets are sent
	_ = listener.Close()
	svrWg.Wait()
}

func getRandomData(n int) [][]byte {
	data := make([][]byte, n)
	for i := range data {
		n := rand.Intn(1024 * 64)
		data[i] = make([]byte, n)
		_, _ = rand.Read(data[i])
	}
	return data
}

func runEchoServer(
	t *testing.T, listener net.Listener, exit <-chan struct{},
	wg *sync.WaitGroup) {
	defer wg.Done()
	clientProc := func(client net.Conn) {
		defer client.Close() // nolint: errcheck
		defer wg.Done()
		var buf [1024 * 32]byte
		for {
			n, err := client.Read(buf[:])
			if err != nil {
				assert.Equal(t, io.EOF, err)
				break
			}

			_, err = client.Write(buf[:n])
			if !assert.NoError(t, err) {
				break
			}
		}
	}

	for {
		conn, err := listener.Accept()
		select {
		case <-exit:
			return
		default:
		}
		if assert.NoError(t, err) {
			wg.Add(1)
			go clientProc(conn)
		}
	}
}

type KCPKeepAliveTestSuite struct {
	suite.Suite
	svrTrans, cliTrans   *KCPTransport
	origCloseSendTimeout time.Duration
}

func (s *KCPKeepAliveTestSuite) SetupSuite() {
	s.origCloseSendTimeout = kcpCloseSendTimeout
	kcpCloseSendTimeout = 50 * time.Millisecond
}

func (s *KCPKeepAliveTestSuite) TearDownSuite() {
	kcpCloseSendTimeout = s.origCloseSendTimeout
}

func (s *KCPKeepAliveTestSuite) SetupTest() {
	var err error
	s.svrTrans, err = NewKCPTransport(KCPConfig{
		Mode:              "fast2",
		Optimize:          "_test_small",
		FEC:               true,
		FECDist:           "10, 2",
		KeepAliveInterval: "50ms",
		KeepAliveTimeout:  "150ms",
	})
	s.Require().NoError(err)
	s.cliTrans, err = NewKCPTransport(KCPConfig{
		Mode:              "fast2",
		Optimize:          "_test_small",
		FEC:               true,
		FECDist:           "10, 2",
		KeepAliveInterval: "50ms",
		KeepAliveTimeout:  "150ms",
	})
	s.Require().NoError(err)
}

func (s *KCPKeepAliveTestSuite) TearDownTest() {
	// check if the connection lists are correctly emptied
	s.svrTrans.connsMtx.Lock()
	defer s.svrTrans.connsMtx.Unlock()
	s.Equal(0, s.svrTrans.conns.Len())
	s.cliTrans.connsMtx.Lock()
	defer s.cliTrans.connsMtx.Unlock()
	s.Equal(0, s.cliTrans.conns.Len())
}

func (s *KCPKeepAliveTestSuite) startServer(
	wg *sync.WaitGroup, listener net.Listener, lostImmediately, cliLost bool) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			cli, err := listener.Accept()
			if err != nil {
				break
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				if lostImmediately { // server connection lost
					c := cli.(*kcpConnWrapper)
					atomic.StoreInt64(&c.lastSend, 0)
					_ = c.UDPSession.Close()
				} else if cliLost { // client connection lost
					defer cli.Close() // nolint: errcheck
					buf := make([]byte, 1)
					_, err := cli.Read(buf)
					s.Error(err)
				} else { // normal
					defer cli.Close() // nolint: errcheck
					buf := make([]byte, 1024*32)
					for {
						n, err := cli.Read(buf)
						if err == io.EOF {
							break
						}
						s.Require().NoError(err)
						_, _ = cli.Write(buf[:n])
					}
				}
			}()
		}
	}()
}

func (s *KCPKeepAliveTestSuite) TestNormalLongIdle() {
	listener, err := s.svrTrans.Listen("127.0.0.1:0")
	s.Require().NoError(err)
	addr := listener.Addr().String()
	svrWg := sync.WaitGroup{}
	s.startServer(&svrWg, listener, false, false)

	cliWg := sync.WaitGroup{}
	for i := 0; i < 5; i++ {
		cliWg.Add(1)
		go func() {
			defer cliWg.Done()
			cli, err := s.cliTrans.Dial(context.Background(), addr)
			s.Require().NoError(err)
			defer cli.Close() // nolint: errcheck
			for _, data := range getRandomData(5) {
				time.Sleep(500 * time.Millisecond) // > KeepAliveTimeout
				_, err := cli.Write(data)
				s.Require().NoError(err)
				buf := make([]byte, len(data))
				_, err = io.ReadFull(cli, buf)
				s.Require().NoError(err)
				s.Equal(data, buf)
			}
		}()
	}

	cliWg.Wait()
	time.Sleep(100 * time.Millisecond) // let the svr conns close normally
	_ = listener.Close()
	svrWg.Wait()
	time.Sleep(100 * time.Millisecond) // ensure the conn lists are cleaned-up
}

func (s *KCPKeepAliveTestSuite) TestServerConnLost() {
	listener, err := s.svrTrans.Listen("127.0.0.1:0")
	s.Require().NoError(err)
	addr := listener.Addr().String()
	svrWg := sync.WaitGroup{}
	s.startServer(&svrWg, listener, true, false)

	cliWg := sync.WaitGroup{}
	for i := 0; i < 5; i++ {
		cliWg.Add(1)
		go func() {
			defer cliWg.Done()
			cli, err := s.cliTrans.Dial(context.Background(), addr)
			s.Require().NoError(err)
			defer cli.Close() // nolint: errcheck
			buf := make([]byte, 1)
			_, err = io.ReadFull(cli, buf)
			s.Error(err)
		}()
	}

	cliWg.Wait()
	time.Sleep(100 * time.Millisecond)
	_ = listener.Close()
	svrWg.Wait()
	time.Sleep(100 * time.Millisecond)
}

func (s *KCPKeepAliveTestSuite) TestSendBlock() {
	listener, err := s.svrTrans.Listen("127.0.0.1:0")
	s.Require().NoError(err)
	addr := listener.Addr().String()
	svrWg := sync.WaitGroup{}
	s.startServer(&svrWg, listener, true, false)

	cliWg := sync.WaitGroup{}
	for i := 0; i < 5; i++ {
		cliWg.Add(1)
		go func() {
			defer cliWg.Done()
			cli, err := s.cliTrans.Dial(context.Background(), addr)
			s.Require().NoError(err)
			defer cli.Close() // nolint: errcheck
			// send untill block, then wait until timeout
			for ; err == nil; _, err = cli.Write([]byte("hello")) {
			}
		}()
	}

	cliWg.Wait()
	time.Sleep(100 * time.Millisecond)
	_ = listener.Close()
	svrWg.Wait()
	time.Sleep(100 * time.Millisecond)
}

func (s *KCPKeepAliveTestSuite) TestClientConnLost() {
	listener, err := s.svrTrans.Listen("127.0.0.1:0")
	s.Require().NoError(err)
	addr := listener.Addr().String()
	svrWg := sync.WaitGroup{}
	s.startServer(&svrWg, listener, false, true)

	cliWg := sync.WaitGroup{}
	for i := 0; i < 5; i++ {
		cliWg.Add(1)
		go func() {
			defer cliWg.Done()
			cli, err := s.cliTrans.Dial(context.Background(), addr)
			s.Require().NoError(err)
			c := cli.(*kcpConnWrapper)
			atomic.StoreInt64(&c.lastSend, 0)
			_ = c.UDPSession.Close()
		}()
	}

	cliWg.Wait()
	time.Sleep(100 * time.Millisecond)
	_ = listener.Close()
	svrWg.Wait()
	time.Sleep(100 * time.Millisecond)
}

func TestKCPTestSuite(t *testing.T) {
	suite.Run(t, new(KCPKeepAliveTestSuite))
}
