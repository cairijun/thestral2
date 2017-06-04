package main

import (
	"context"
	"io"
	"net"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// nolint: golint
const (
	ProxyGeneralErr      = 0x01
	ProxyNotAllowed      = 0x02
	ProxyConnectFailed   = 0x05
	ProxyCmdUnsupported  = 0x07
	ProxyAddrUnsupported = 0x08
)

// ProxyError is a wrapper of a normal error along with a proxy error type code.
type ProxyError struct {
	Error   error
	ErrType byte
}

func wrapAsProxyError(err error, errType byte) *ProxyError {
	if err == nil {
		return nil
	}
	return &ProxyError{err, errType}
}

// ProxyRequest represents a proxy request sent by the client.
type ProxyRequest interface {
	WithPeerIdentifiers
	TargetAddr() Address
	Success(addr Address) io.ReadWriteCloser
	Fail(err *ProxyError)
	Logger() *zap.SugaredLogger
}

// ProxyServer is the server of some proxy protocol.
type ProxyServer interface {
	Start() (<-chan ProxyRequest, error)
	Stop()
}

// ProxyClient is the client of some proxy protocol.
type ProxyClient interface {
	Request(ctx context.Context, addr Address) (net.Conn, Address, *ProxyError)
}

// DirectTCPClient is a ProxyClient without any proxy protocol.
type DirectTCPClient struct{}

// Request establishes a direct connection to the given address.
func (DirectTCPClient) Request(
	ctx context.Context, addr Address) (
	conn net.Conn, boundAddr Address, pErr *ProxyError) {
	var reqAddr string
	switch a := addr.(type) {
	case *TCP4Addr:
		reqAddr = a.String()
	case *TCP6Addr:
		reqAddr = a.String()
	case *DomainNameAddr:
		reqAddr = a.String()
	default:
		return nil, nil, wrapAsProxyError(
			errors.Errorf("unsupported address for DirectTCPClient: %s", addr),
			ProxyAddrUnsupported)
	}

	var err error
	conn, err = TCPTransport{}.Dial(ctx, reqAddr)
	if err == nil {
		boundAddr, err = FromNetAddr(conn.LocalAddr())
	}
	pErr = wrapAsProxyError(errors.WithStack(err), ProxyConnectFailed)
	return
}

// CreateProxyServer creates a ProxyServer from the given configuration.
func CreateProxyServer(
	logger *zap.SugaredLogger, config ProxyConfig) (ProxyServer, error) {
	switch config.Protocol {
	case "socks5":
		return NewSOCKS5Server(logger, config)
	case "direct":
		return nil, errors.New("'direct' cannot be used as a proxy server")
	default:
		return nil, errors.New("unknown proxy protocol: " + config.Protocol)
	}
}

// CreateProxyClient creates a ProxyClient from the given configuration.
func CreateProxyClient(config ProxyConfig) (ProxyClient, error) {
	switch config.Protocol {
	case "direct":
		if config.Transport != nil {
			return nil, errors.New(
				"'direct' protocol should not have any transport setting")
		}
		if len(config.Settings) > 0 {
			return nil, errors.New(
				"'direct' protocol should not have any extra setting")
		}
		return DirectTCPClient{}, nil

	case "socks5":
		return NewSOCKS5Client(config)

	default:
		return nil, errors.New("unknown proxy protocol: " + config.Protocol)
	}
}
