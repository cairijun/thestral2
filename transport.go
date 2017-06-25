package main

import (
	"context"
	"net"

	"github.com/pkg/errors"
)

// Transport provides the server and client sides operation on some
// stream-oriented transport layer protocol.
type Transport interface {
	// Dial creates a connection to the given address.
	Dial(ctx context.Context, address string) (net.Conn, error)
	// Listen creates a server listening on the given address.
	Listen(address string) (net.Listener, error)
}

// TCPTransport is a Transport on the TCP protocol.
type TCPTransport struct{}

// Dial creates a connection to a TCP server.
func (TCPTransport) Dial(
	ctx context.Context, address string) (net.Conn, error) {
	conn, err := new(net.Dialer).DialContext(ctx, "tcp", address)
	return conn, errors.WithStack(err)
}

// Listen creates a TCP listener on a given address.
func (TCPTransport) Listen(address string) (net.Listener, error) {
	listener, err := net.Listen("tcp", address)
	return listener, errors.WithStack(err)
}

// CreateTransport creates a Transport according to the given configuration.
func CreateTransport(config *TransportConfig) (transport Transport, err error) {
	// default is TCP
	if config == nil {
		return TCPTransport{}, nil
	}

	// Proxied/KCP/TCP is should be the inner most layer
	if config.KCP != nil && config.Proxied != nil {
		err = errors.New("'kcp' cannot be used along with 'proxied'")
	} else if config.KCP != nil {
		transport, err = NewKCPTransport(*config.KCP)
	} else if config.Proxied != nil {
		transport, err = NewProxiedTransport(*config.Proxied)
	} else {
		transport = TCPTransport{}
	}

	// encryption wraps around the inner
	if err == nil && config.TLS != nil {
		transport, err = NewTLSTransport(*config.TLS, transport)
	}

	// compression should be the outer most layer
	if err == nil && config.Compression != "" {
		transport, err = WrapTransCompression(transport, config.Compression)
	}

	err = errors.WithMessage(err, "failed to create transport")
	return
}
