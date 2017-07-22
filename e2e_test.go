package main

import (
	"context"
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"path"
	"testing"
	"time"

	"github.com/richardtsai/thestral2/db"
	. "github.com/richardtsai/thestral2/lib"
	"github.com/stretchr/testify/suite"
)

type E2ETestSuite struct {
	suite.Suite

	// suite-level
	dbCfg      *db.Config
	tmpDir     string
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
	if db.CheckDriver("sqlite3") {
		s.initDB()
	}

	s.locAddr = "127.0.0.1:64892"
	s.svrAddr = "127.0.0.1:64893"

	s.locConfig = &Config{
		Downstreams: map[string]ProxyConfig{"local": {
			Protocol: "socks5",
			Settings: map[string]interface{}{
				"address": s.locAddr, "check_users": s.dbCfg != nil,
			},
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
		DB:      s.dbCfg,
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
	if s.tmpDir != "" {
		_ = os.RemoveAll(s.tmpDir)
	}
	_ = s.targetSvr.Close()
}

func (s *E2ETestSuite) initDB() {
	var err error
	s.tmpDir, err = ioutil.TempDir("", "thestral2_E2ETestSuite")
	s.Require().NoError(err)
	s.dbCfg = &db.Config{
		Driver: "sqlite3",
		DSN:    path.Join(s.tmpDir, "test.db"),
	}
	s.Require().NoError(db.InitDB(*s.dbCfg))
	dao, err := db.NewUserDAO()
	s.Require().NoError(err)
	pwhash := db.HashUserPass("password")
	s.Require().NoError(dao.Add(&db.User{
		Scope: "proxy.socks5", Name: "user",
		PWHash: &pwhash,
	}))
	s.Require().NoError(dao.Close())
}

func (s *E2ETestSuite) SetupTest() {
	var err error
	s.appCtx, s.appCtxCancel = context.WithCancel(context.Background())
	s.svrApp, err = NewThestralApp(*s.svrConfig)
	s.Require().NoError(err)
	s.locApp, err = NewThestralApp(*s.locConfig)
	s.Require().NoError(err)

	runApp := func(appCtx context.Context, app *Thestral) {
		// no error checking here because referencing to s.Xxxx will lead to
		// false positive in the race detector.
		_ = app.Run(appCtx)
	}
	go runApp(s.appCtx, s.svrApp)
	go runApp(s.appCtx, s.locApp)
	time.Sleep(time.Millisecond * 100) // ensure the servers are started

	s.cli, err = CreateProxyClient(ProxyConfig{
		Protocol: "socks5",
		Settings: map[string]interface{}{
			"address": s.locAddr, "username": "user", "password": "password",
		},
	})
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

func (s *E2ETestSuite) TestNoUserPass() {
	if s.dbCfg == nil {
		s.T().Skip("database driver 'sqlite3' is not enabled")
	}
	cli, err := CreateProxyClient(ProxyConfig{
		Protocol: "socks5",
		Settings: map[string]interface{}{"address": s.locAddr},
	})
	s.Require().NoError(err)

	_, _, pErr := cli.Request(context.Background(), s.targetAddr)
	if s.NotNil(pErr) {
		s.Error(pErr.Error)
		s.Equal(ProxyGeneralErr, pErr.ErrType)
	}
}

func (s *E2ETestSuite) TestWrongUserPass() {
	if s.dbCfg == nil {
		s.T().Skip("database driver 'sqlite3' is not enabled")
	}
	cli, err := CreateProxyClient(ProxyConfig{
		Protocol: "socks5",
		Settings: map[string]interface{}{
			"address": s.locAddr, "username": "user", "password": "wrong pass",
		},
	})
	s.Require().NoError(err)

	_, _, pErr := cli.Request(context.Background(), s.targetAddr)
	if s.NotNil(pErr) {
		s.Error(pErr.Error)
		s.Equal(ProxyGeneralErr, pErr.ErrType)
	}
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
