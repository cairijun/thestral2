package main

import (
	"context"
	"crypto/sha1"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"io/ioutil"
	"net"
	"runtime"
	"sync"
	"time"

	"github.com/pkg/errors"
	. "github.com/richardtsai/thestral2/lib"
)

const defaultTLSHandshakeTimeout = time.Minute * 1

// TLSTransport is a Transport for TLS protocol.
type TLSTransport struct {
	inner            Transport
	tlsConfig        tls.Config
	handshakeTimeout time.Duration
}

// NewTLSTransport create a TLSTransport on top of a given inner Transport.
func NewTLSTransport(config TLSConfig, inner Transport) (*TLSTransport, error) {
	transport := &TLSTransport{inner: inner}
	tc := &transport.tlsConfig

	cert, err := tls.LoadX509KeyPair(config.Cert, config.Key)
	if err != nil {
		return nil, errors.Wrap(err, "failed to load key pair")
	}
	tc.Certificates = append(tc.Certificates, cert)

	if len(config.CAs) == 0 {
		if runtime.GOOS == "windows" {
			if len(config.ExtraCAs) > 0 {
				return nil, errors.New(
					"currently adding extra CA(s) to " +
						"system default CA pool is not supported on Windows")
			}
		} else {
			if tc.RootCAs, err = x509.SystemCertPool(); err != nil {
				return nil, errors.Wrap(err, "failed to load system CA pool")
			}
		}
	} else {
		tc.RootCAs = x509.NewCertPool()
	}
	caToAdd := append(config.CAs, config.ExtraCAs...)
	for i := range caToAdd {
		if err := addCA(tc.RootCAs, caToAdd[i]); err != nil {
			return nil, errors.Wrapf(
				err, "failed to add %s to the root ca list", caToAdd[i])
		}
	}

	if config.VerifyClient {
		tc.ClientAuth = tls.RequireAndVerifyClientCert
	}

	if len(config.ClientCAs) > 0 {
		tc.ClientCAs = x509.NewCertPool()
		for i := range config.ClientCAs {
			if err := addCA(tc.ClientCAs, config.ClientCAs[i]); err != nil {
				return nil, errors.Wrapf(err,
					"failed to add %s to the client ca list",
					config.ClientCAs[i])
			}
		}
	}

	tc.MinVersion = tls.VersionTLS11

	tc.CipherSuites = []uint16{
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
		tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305,
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,
	}

	if config.HandshakeTimeout != "" {
		t, err := time.ParseDuration(config.HandshakeTimeout)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid handshake_timeout")
		}
		if t <= 0 {
			return nil, errors.New("handshake_timeout should be > 0")
		}
		transport.handshakeTimeout = t
	} else {
		transport.handshakeTimeout = defaultTLSHandshakeTimeout
	}

	return transport, nil
}

// Dial creates a TLS connection to the given address. The hostname part
// of the address will be verified against the peer certificate.
func (t *TLSTransport) Dial(
	ctx context.Context, address string) (net.Conn, error) {
	inner, err := t.inner.Dial(ctx, address)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to dial to TLS host")
	}

	cfg := t.tlsConfig.Clone()
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, errors.Wrap(err, "invalid address for TLS: "+address)
	}
	cfg.ServerName = host
	tlsConn := tls.Client(inner, cfg)

	// the channel must be buffered to prevent the hanshaking goroutine from
	// blocking forever if the context is cancelled or timeout.
	resultCh := make(chan error, 1)
	go func() {
		_ = tlsConn.SetDeadline(time.Now().Add(t.handshakeTimeout))
		err := tlsConn.Handshake()
		_ = tlsConn.SetDeadline(time.Time{})
		resultCh <- err
	}()

	select {
	case err := <-resultCh:
		if err != nil {
			// the conn still need to be wrapped to retrieve the peer identifier
			_ = tlsConn.Close()
		}
		return wrapTLSConn(tlsConn, t.handshakeTimeout), errors.WithStack(err)
	case <-ctx.Done():
		_ = tlsConn.Close()
		return nil, errors.WithStack(ctx.Err())
	}
}

// Listen creates a TLS server listening on the given address.
func (t *TLSTransport) Listen(address string) (net.Listener, error) {
	innerListener, err := t.inner.Listen(address)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to accept client")
	}
	return &tlsListener{
		innerListener, t.tlsConfig.Clone(), t.handshakeTimeout}, nil
}

type tlsListener struct {
	net.Listener
	config           *tls.Config
	handshakeTimeout time.Duration
}

func (l *tlsListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	tlsConn := tls.Server(conn, l.config)
	return wrapTLSConn(tlsConn, l.handshakeTimeout), err
}

type tlsConnWrapper struct {
	*tls.Conn
	inited           sync.Once
	peerID           *PeerIdentifier
	handshakeTimeout time.Duration
}

func wrapTLSConn(
	conn *tls.Conn, handshakeTimeout time.Duration) *tlsConnWrapper {
	return &tlsConnWrapper{Conn: conn, handshakeTimeout: handshakeTimeout}
}

func (c *tlsConnWrapper) GetPeerIdentifiers() ([]*PeerIdentifier, error) {
	var err error
	c.inited.Do(func() {
		state := c.ConnectionState()
		if !state.HandshakeComplete {
			_ = c.SetDeadline(time.Now().Add(c.handshakeTimeout))
			err = c.Handshake()
			_ = c.SetDeadline(time.Time{})
			state = c.ConnectionState()
		}
		c.peerID = makePeerIdentifier(state)
	})
	return []*PeerIdentifier{c.peerID}, errors.WithStack(err)
}

func makePeerIdentifier(connState tls.ConnectionState) *PeerIdentifier {
	if len(connState.PeerCertificates) > 0 {
		cert := connState.PeerCertificates[0]
		fingerprint := sha1.Sum(cert.Raw)
		return &PeerIdentifier{
			Scope:    "transport.tls",
			UniqueID: hex.EncodeToString(fingerprint[:]),
			Name:     cert.Subject.CommonName,
			ExtraInfo: map[string]interface{}{
				"tlsIssuedBy":   cert.Issuer.CommonName,
				"tlsValidFrom":  cert.NotBefore,
				"tlsValidUntil": cert.NotAfter},
		}
	}
	return nil
}

func addCA(cas *x509.CertPool, file string) error {
	pemData, err := ioutil.ReadFile(file)
	if err != nil {
		return err
	}
	if !cas.AppendCertsFromPEM(pemData) {
		return errors.New("failed to parsed CA file " + file)
	}
	return nil
}
