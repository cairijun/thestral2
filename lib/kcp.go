package lib

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/pkg/errors"
	"github.com/xtaci/kcp-go"
)

// KCPTransport is a connection-aware Transport based on the KCP protocol.
// Closing a connection will notify the peer end on a best-efforts basis.
type KCPTransport struct {
	noDelay      int
	interval     int
	resend       int
	nc           int
	sndWnd       int
	rcvWnd       int
	dataShards   int
	parityShards int
}

// NewKCPTransport creates KCPTransport with a given configuration.
func NewKCPTransport(config KCPConfig) (*KCPTransport, error) {
	// var transport *KCPTransport
	t := new(KCPTransport)
	switch config.Mode {
	case "", "normal":
		t.noDelay, t.interval, t.resend, t.nc = 0, 25, 0, 0
	case "fast":
		t.noDelay, t.interval, t.resend, t.nc = 0, 25, 2, 1
	case "fast2":
		t.noDelay, t.interval, t.resend, t.nc = 1, 10, 2, 1
	default:
		return nil, errors.New("invalid KCP mode: " + config.Mode)
	}

	switch config.Optimize {
	case "", "balance":
		t.sndWnd, t.rcvWnd = 512, 512
	case "send":
		t.sndWnd, t.rcvWnd = 256, 1024
	case "receive":
		t.sndWnd, t.rcvWnd = 1024, 256
	default:
		return nil, errors.New("invalid optimization: " + config.Optimize)
	}

	if config.FEC {
		if config.FECDist == "" {
			t.dataShards = 10
			t.parityShards = 2
		} else {
			_, err := fmt.Sscanf(
				config.FECDist, "%d,%d", &t.dataShards, &t.parityShards)
			if err != nil {
				return nil, errors.Wrap(err, "invalid FEC distribution")
			}
		}
	}

	return t, nil
}

// Dial creates a KCP connection to a remote host.
func (t *KCPTransport) Dial(
	ctx context.Context, address string) (net.Conn, error) {
	type result struct {
		conn net.Conn
		err  error
	}

	resultCh := make(chan result, 1)

	go func() {
		kcpConn, err := kcp.DialWithOptions(
			address, nil, t.dataShards, t.parityShards)
		if err != nil {
			resultCh <- result{nil, err}
		} else {
			resultCh <- result{t.wrapKCPConn(kcpConn), nil}
		}
	}()

	select {
	case rst := <-resultCh:
		if rst.err != nil {
			return nil, errors.WithStack(rst.err)
		}
		return rst.conn, nil
	case <-ctx.Done():
		return nil, errors.WithStack(ctx.Err())
	}
}

// Listen creates a KCP listener on a given address.
func (t *KCPTransport) Listen(address string) (net.Listener, error) {
	listener, err := kcp.ListenWithOptions(
		address, nil, t.dataShards, t.parityShards)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return &kcpListenerWrapper{listener, t}, nil
}

type kcpConnWrapper struct {
	*kcp.UDPSession
	rdMtx      sync.Mutex
	rdDataLeft uint32
}

const (
	kcpDataPacket = 0
	kcpClose      = 1
)

func (t *KCPTransport) wrapKCPConn(kcpConn *kcp.UDPSession) *kcpConnWrapper {
	kcpConn.SetNoDelay(t.noDelay, t.interval, t.resend, t.nc)
	kcpConn.SetStreamMode(true)
	kcpConn.SetWindowSize(t.sndWnd, t.rcvWnd)
	wrapped := new(kcpConnWrapper)
	wrapped.UDPSession = kcpConn
	wrapped.rdDataLeft = 0
	return wrapped
}

func (c *kcpConnWrapper) Read(b []byte) (int, error) {
	c.rdMtx.Lock()
	defer c.rdMtx.Unlock()
	if c.rdDataLeft == 0 {
		var header [5]byte
		for i := 0; i < len(header); {
			n, err := c.UDPSession.Read(header[i:])
			if err != nil || n == 0 {
				return 0, err
			}
			if i == 0 {
				if header[0] == kcpClose {
					return 0, io.EOF
				} else if header[0] != kcpDataPacket {
					return 0, errors.New("invalid KCP header")
				}
			}
			i += n
		}

		// network byte order
		c.rdDataLeft = binary.BigEndian.Uint32(header[1:])
	}

	if len(b) > int(c.rdDataLeft) {
		b = b[:c.rdDataLeft]
	}
	n, err := c.UDPSession.Read(b)
	if err != nil {
		return 0, err
	}
	c.rdDataLeft -= uint32(n)
	return n, nil
}

func (c *kcpConnWrapper) Write(b []byte) (int, error) {
	if len(b) > 0xffffffff {
		return 0, errors.New("send buffer size exceeds limitation")
	}
	n := uint32(len(b))
	buf := GlobalBufPool.Get(uint(n + 5))
	defer GlobalBufPool.Free(buf)
	buf[0] = kcpDataPacket
	binary.BigEndian.PutUint32(buf[1:5], n)
	copy(buf[5:], b)
	return c.UDPSession.Write(buf)
}

func (c *kcpConnWrapper) Close() error {
	_, _ = c.UDPSession.Write([]byte{kcpClose})
	return c.UDPSession.Close()
}

type kcpListenerWrapper struct {
	*kcp.Listener
	kcpTransport *KCPTransport
}

func (l *kcpListenerWrapper) Accept() (net.Conn, error) {
	conn, err := l.Listener.AcceptKCP()
	if err != nil {
		return nil, err
	}
	return l.kcpTransport.wrapKCPConn(conn), nil
}

func (l *kcpListenerWrapper) AcceptKCP() (*kcp.UDPSession, error) {
	panic("should use kcpListenerWrapper.Accept() instead")
}

func (l *kcpListenerWrapper) Close() error {
	err := l.Listener.Close()
	return err
}
