package main

import (
	"net"
	"strconv"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// PeerIdentifier uniquely identify a peer (a client or a server).
// It may be provided along with a connection or something else.
type PeerIdentifier struct {
	Scope     string
	UniqueID  string
	Name      string
	ExtraInfo map[string]interface{}
}

// WithPeerIdentifiers is an interface for types that have
// PeerIdentifier objects attached.
type WithPeerIdentifiers interface {
	GetPeerIdentifiers() ([]*PeerIdentifier, error)
}

// Address is the interface of all the supported address types.
type Address interface {
	isAddress()
	String() string
}

// TCP4Addr is an Address of a TCP endpoint using IPv4.
type TCP4Addr struct {
	IP   net.IP
	Port uint16
}

func (*TCP4Addr) isAddress() {}

func (a *TCP4Addr) String() string {
	return net.JoinHostPort(a.IP.String(), strconv.Itoa(int(a.Port)))
}

// TCP6Addr is an Address of a TCP endpoint using IPv6.
type TCP6Addr struct {
	IP   net.IP
	Port uint16
}

func (*TCP6Addr) isAddress() {}

func (a *TCP6Addr) String() string {
	return net.JoinHostPort(a.IP.String(), strconv.Itoa(int(a.Port)))
}

// DomainNameAddr is an Address of an endpoint using a domain name.
type DomainNameAddr struct {
	DomainName string
	Port       uint16
}

func (*DomainNameAddr) isAddress() {}

func (a *DomainNameAddr) String() string {
	return net.JoinHostPort(a.DomainName, strconv.Itoa(int(a.Port)))
}

// FromNetAddr parses a net.Addr into an Address.
func FromNetAddr(netAddr net.Addr) (Address, error) {
	if netAddr.Network() != "tcp" {
		return nil, errors.New("unknown network: " + netAddr.Network())
	}
	tcpAddr, err := net.ResolveTCPAddr("tcp", netAddr.String())
	if err != nil {
		return nil, errors.WithStack(err)
	}

	if ip := tcpAddr.IP.To4(); ip != nil {
		return &TCP4Addr{IP: ip, Port: uint16(tcpAddr.Port)}, nil
	}
	return &TCP6Addr{IP: tcpAddr.IP, Port: uint16(tcpAddr.Port)}, nil
}

// CreateLogger creates a zap SugaredLogger from given configuration.
func CreateLogger(config LoggingConfig) (*zap.SugaredLogger, error) {
	zapCfg := zap.NewProductionConfig()
	if config.File != "" {
		zapCfg.OutputPaths = []string{config.File}
	}
	if config.Format != "" {
		zapCfg.Encoding = config.Format
	}
	switch config.Level {
	case "": // no-op
	case "debug":
		zapCfg.Level.SetLevel(zap.DebugLevel)
	case "info":
		zapCfg.Level.SetLevel(zap.InfoLevel)
	case "warn":
		zapCfg.Level.SetLevel(zap.WarnLevel)
	case "error":
		zapCfg.Level.SetLevel(zap.ErrorLevel)
	default:
		return nil, errors.New("unknown logging level: " + config.Level)
	}

	logger, err := zapCfg.Build()
	if err != nil {
		return nil, err
	}
	return logger.Sugar(), nil
}
