package main

import (
	"context"
	"io"
	"math/rand"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type E2ETestSuite struct {
	suite.Suite

	// suite-level
	locAddr    string
	svrAddr    string
	locConfig  *Config
	svrConfig  *Config
	targetAddr Address
	targetSvr  net.Listener

	// test-level
	locApp       *Thestral
	svrApp       *Thestral
	appCtx       context.Context
	appCtxCancel context.CancelFunc
	cli          ProxyClient
}

func (s *E2ETestSuite) SetupSuite() {
	s.locAddr = "127.0.0.1:64892"
	s.svrAddr = "127.0.0.1:64893"

	s.locConfig = &Config{
		Downstreams: map[string]ProxyConfig{"local": {
			Protocol: "socks5",
			Settings: map[string]interface{}{"address": s.locAddr},
		}},
		Upstreams: map[string]ProxyConfig{"proxy": {
			Protocol: "socks5",
			Transport: &TransportConfig{
				Compression: "snappy",
				TLS: &TLSConfig{
					Cert: "test_files/test.pem",
					Key:  "test_files/test.key.pem",
					CAs:  []string{"test_files/ca.pem"},
				},
				KCP: &KCPConfig{Mode: "fast2", Optimize: "receive", FEC: true},
			},
			Settings: map[string]interface{}{"address": s.svrAddr, "simplified": true},
		}},
		Logging: LoggingConfig{Level: "fatal"},
	}
	s.svrConfig = &Config{
		Downstreams: map[string]ProxyConfig{"proxy": {
			Protocol: "socks5",
			Transport: &TransportConfig{
				Compression: "snappy",
				TLS: &TLSConfig{
					Cert:         "test_files/test.server.pem",
					Key:          "test_files/test.server.key.pem",
					VerifyClient: true,
					ClientCAs:    []string{"test_files/ca.pem"},
				},
				KCP: &KCPConfig{Mode: "fast2", Optimize: "send", FEC: true},
			},
			Settings: map[string]interface{}{
				"address": s.svrAddr, "simplified": true, "handshake_timeout": "1s"},
		}},
		Upstreams: map[string]ProxyConfig{"direct": {Protocol: "direct"}},
		Rules: map[string]RuleConfig{
			"reject": {Domains: []string{"will.be.rejected"}},
		},
		Logging: LoggingConfig{Level: "fatal"},
	}

	tgtAddr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	s.Require().NoError(err)
	s.targetSvr, err = net.ListenTCP("tcp", tgtAddr)
	s.Require().NoError(err)
	s.targetAddr, err = FromNetAddr(s.targetSvr.Addr())
	s.Require().NoError(err)
	go func() {
		for {
			if conn, err := s.targetSvr.Accept(); err == nil {
				_, _ = io.Copy(conn, conn)
			} else {
				break
			}
		}
	}()
}

func (s *E2ETestSuite) TearDownSuite() {
	_ = s.targetSvr.Close()
}

func (s *E2ETestSuite) SetupTest() {
	var err error
	s.appCtx, s.appCtxCancel = context.WithCancel(context.Background())
	s.svrApp, err = NewThestralApp(*s.svrConfig)
	s.Require().NoError(err)
	s.locApp, err = NewThestralApp(*s.locConfig)
	s.Require().NoError(err)

	go func() {
		s.Assert().NoError(s.svrApp.Run(s.appCtx))
	}()
	go func() {
		s.Assert().NoError(s.locApp.Run(s.appCtx))
	}()
	time.Sleep(time.Millisecond * 100) // ensure the servers are started

	s.cli, err = CreateProxyClient(s.locConfig.Downstreams["local"])
	s.Require().NoError(err)
}

func (s *E2ETestSuite) TearDownTest() {
	time.Sleep(time.Millisecond * 100) // ensure the connections are closed
	s.appCtxCancel()
	time.Sleep(time.Millisecond * 100) // ensure the servers are stopped
}

func (s *E2ETestSuite) TestRelay() {
	conn, _, pErr := s.cli.Request(context.Background(), s.targetAddr)
	s.Require().Nil(pErr)

	for i := 0; i < 10; i++ {
		l := rand.Intn(10240)
		data := make([]byte, l)
		buf := make([]byte, l)
		_, _ = rand.Read(data)

		_, err := conn.Write(data)
		if s.Assert().NoError(err) {
			_, err = io.ReadFull(conn, buf)
			if s.Assert().NoError(err) {
				s.Assert().Equal(data, buf)
			}
		}
	}

	s.Assert().NoError(conn.Close())
}

func (s *E2ETestSuite) TestRejectByRule() {
	addr := &DomainNameAddr{"will.be.rejected", 12345}
	_, _, pErr := s.cli.Request(context.Background(), addr)
	s.Require().NotNil(pErr)
	s.Assert().EqualValues(ProxyNotAllowed, pErr.ErrType)
	s.Assert().Error(pErr.Error)
}

func (s *E2ETestSuite) TestConnectFailed() {
	addr := &DomainNameAddr{"does.not.exist", 80}
	_, _, pErr := s.cli.Request(context.Background(), addr)
	s.Require().NotNil(pErr)
	s.Assert().EqualValues(ProxyConnectFailed, pErr.ErrType)
	s.Assert().Error(pErr.Error)
}

func TestE2ETestSuite(t *testing.T) {
	suite.Run(t, new(E2ETestSuite))
}
