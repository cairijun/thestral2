package lib

import (
	"context"
	"io"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// ProxyErrorType is the type of a proxy error. Its value is identical to those
// of SOCKS protocol.
type ProxyErrorType byte

// nolint: golint
const (
	ProxyGeneralErr      ProxyErrorType = 0x01
	ProxyNotAllowed      ProxyErrorType = 0x02
	ProxyConnectFailed   ProxyErrorType = 0x05
	ProxyCmdUnsupported  ProxyErrorType = 0x07
	ProxyAddrUnsupported ProxyErrorType = 0x08
)

//go:generate stringer -type=ProxyErrorType

var currRequestID uint64

func init() {
	currRequestID = uint64(time.Now().Unix())
}

// GetNextRequestID generates a string that can be used as the ID
// of a new ProxyRequest.
func GetNextRequestID() string {
	id := atomic.AddUint64(&currRequestID, 1)
	return strings.ToUpper(strconv.FormatUint(id, 36))
}

// ProxyError is a wrapper of a normal error along with a proxy error type code.
type ProxyError struct {
	Error   error
	ErrType ProxyErrorType
}

func wrapAsProxyError(err error, errType ProxyErrorType) *ProxyError {
	if err == nil {
		return nil
	}
	return &ProxyError{err, errType}
}

// ProxyRequest represents a proxy request sent by the client.
type ProxyRequest interface {
	WithPeerIdentifiers
	PeerAddr() string
	TargetAddr() Address
	Success(addr Address) io.ReadWriteCloser
	Fail(err *ProxyError)
	ID() string
	Logger() *zap.SugaredLogger
}

// ProxyServer is the server of some proxy protocol.
type ProxyServer interface {
	Start() (<-chan ProxyRequest, error)
	Stop()
}

// ProxyClient is the client of some proxy protocol.
type ProxyClient interface {
	Request(ctx context.Context, addr Address) (
		io.ReadWriteCloser, Address, *ProxyError)
}

// DirectTCPClient is a ProxyClient without any proxy protocol.
type DirectTCPClient struct{}

// Request establishes a direct connection to the given address.
func (DirectTCPClient) Request(ctx context.Context, addr Address) (
	io.ReadWriteCloser, Address, *ProxyError) {
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

	conn, err := TCPTransport{}.Dial(ctx, reqAddr)
	var boundAddr Address
	if err == nil {
		boundAddr, err = FromNetAddr(conn.LocalAddr())
	}
	pErr := wrapAsProxyError(errors.WithStack(err), ProxyConnectFailed)
	return conn, boundAddr, pErr
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

	case "http":
		if config.Transport != nil {
			return nil, errors.New(
				"'http' protocol should not have any transport setting")
		}
		addr, ok := config.Settings["address"]
		if !ok || len(config.Settings) != 1 {
			return nil, errors.New(
				"'http' protocol should have one and only one" +
					" extra setting 'address'")
		}
		if addrStr, ok := addr.(string); ok {
			return HTTPTunnelClient{addrStr}, nil
		}
		return nil, errors.New("a valid 'address' must be supplied")

	case "socks5":
		return NewSOCKS5Client(config)

	default:
		return nil, errors.New("unknown proxy protocol: " + config.Protocol)
	}
}
