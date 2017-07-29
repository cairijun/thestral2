package lib

import (
	"context"
	"io"
	"net"
	"time"

	"github.com/pkg/errors"
)

// ProxiedTransport is a client-only Transport that dials to a remote host
// via a proxy server.
type ProxiedTransport struct {
	upstream ProxyClient
}

// NewProxiedTransport creates a ProxiedTransport from the given proxy
// configuration.
func NewProxiedTransport(config ProxyConfig) (*ProxiedTransport, error) {
	upstream, err := CreateProxyClient(config)
	if err != nil {
		return nil, errors.WithMessage(
			err, "failed to create proxy client for ProxiedTransport")
	}
	return &ProxiedTransport{upstream}, nil
}

// Listen is not implemented for ProxiedTransport.
func (t *ProxiedTransport) Listen(address string) (net.Listener, error) {
	panic("ProxiedTransport can not be used as a server-side transport")
}

// Dial creates a connection to a remote host via the proxy.
func (t *ProxiedTransport) Dial(
	ctx context.Context, address string) (net.Conn, error) {
	addr, err := ParseAddress(address)
	if err != nil {
		return nil, errors.WithMessage(err, "invalid address")
	}

	rwc, _, pErr := t.upstream.Request(ctx, addr)
	if pErr != nil {
		return nil, errors.WithMessage(pErr.Error, "connection failed")
	}

	if conn, isConn := rwc.(net.Conn); isConn {
		return conn, nil
	}
	return &proxiedConn{rwc}, nil
}

type proxiedConn struct {
	io.ReadWriteCloser
}

func (c *proxiedConn) LocalAddr() net.Addr {
	return nil
}

func (c *proxiedConn) RemoteAddr() net.Addr {
	return nil
}

func (c *proxiedConn) SetDeadline(t time.Time) error {
	return errors.New("not implemented")
}

func (c *proxiedConn) SetReadDeadline(t time.Time) error {
	return errors.New("not implemented")
}

func (c *proxiedConn) SetWriteDeadline(t time.Time) error {
	return errors.New("not implemented")
}
