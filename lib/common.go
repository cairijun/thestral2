package lib

import (
	"fmt"
	"net"
	"os"
	"runtime"
	"strconv"
	"sync/atomic"

	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// ThestralVersion is an external string variable identifying the version
// of this binary.
var ThestralVersion = "v1.3.2"

// ThestralBuiltTime is an external string variable identifying the built time
// of this binary.
var ThestralBuiltTime = "UNKNOWN"

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

// ParseAddress tries to parse a string into an Address.
func ParseAddress(s string) (Address, error) {
	h, p, err := net.SplitHostPort(s)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	port, err := strconv.Atoi(p)
	if err != nil {
		port, err = net.LookupPort("tcp", p)
	}
	if err != nil {
		return nil, errors.WithStack(err)
	}

	if ip := net.ParseIP(h); ip != nil {
		if ip.To4() != nil {
			return &TCP4Addr{ip, uint16(port)}, nil
		}
		return &TCP6Addr{ip, uint16(port)}, nil
	}
	return &DomainNameAddr{h, uint16(port)}, nil
}

// CreateLogger creates a zap SugaredLogger from given configuration.
func CreateLogger(config LoggingConfig) (*zap.SugaredLogger, error) {
	zapCfg := zap.NewProductionConfig()
	zapCfg.Sampling = nil // disable sampling as it is useless in our scale
	if config.File != "" {
		zapCfg.OutputPaths = []string{config.File}
	}
	if config.Format != "" {
		if config.Format == "console_rich" {
			zapCfg.Encoding = "console"
			zapCfg.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
		} else {
			zapCfg.Encoding = config.Format
		}
	}
	if zapCfg.Encoding == "console" {
		zapCfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
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
	case "fatal":
		zapCfg.Level.SetLevel(zap.FatalLevel)
	default:
		return nil, errors.New("unknown logging level: " + config.Level)
	}

	logger, err := zapCfg.Build()
	if err != nil {
		return nil, err
	}
	return logger.Sugar(), nil
}

// GetHomePath returns the home path of the current user.
func GetHomePath() string {
	if runtime.GOOS == "windows" {
		if p := os.Getenv("HOME"); p != "" {
			return p
		} else if p := os.Getenv("USERPROFILE"); p != "" {
			return p
		}
		d := os.Getenv("HOMEDRIVE")
		p := os.Getenv("HOMEPATH")
		if d != "" && p != "" {
			return d + p
		}
		return ""
	}
	return os.Getenv("HOME")
}

// BytesHumanized generates a human-friendly representation of a byte count.
func BytesHumanized(bytes uint64) string {
	var format string
	number := float32(bytes)
	if bytes < 100 {
		format = "%.0f B"
	} else if bytes < 1024*1024 {
		format, number = "%.2f KiB", number/1024
	} else if bytes < 1024*1024*1024 {
		format, number = "%.2f MiB", number/(1024*1024)
	} else if bytes < 1024*1024*1024*1024 {
		format, number = "%.2f GiB", number/(1024*1024*1024)
	} else if bytes < 1024*1024*1024*1024*1024 {
		format, number = "%.2f TiB", number/(1024*1024*1024*1024)
	}
	return fmt.Sprintf(format, number)
}

// SpinMutex is a spin mutex as its name suggests.
type SpinMutex struct {
	locked uint32
}

// Lock the spin mutex. Recursive locking is not allowed.
func (m *SpinMutex) Lock() {
	for {
		if atomic.CompareAndSwapUint32(&m.locked, 0, 1) {
			break
		}
		runtime.Gosched()
	}
}

// Unlock the spin mutex.
func (m *SpinMutex) Unlock() {
	if !atomic.CompareAndSwapUint32(&m.locked, 1, 0) {
		panic("the spin mutex is not locked")
	}
}
