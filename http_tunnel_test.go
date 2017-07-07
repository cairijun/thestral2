package main

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"net"
	"testing"

	. "github.com/richardtsai/thestral2/lib"
	"github.com/stretchr/testify/suite"
)

type HTTPTunnelTestSuite struct {
	suite.Suite

	testData   [][]byte
	expReq     string
	targetAddr Address
}

func (s *HTTPTunnelTestSuite) SetupTest() {
	s.testData = make([][]byte, 16)
	for i := range s.testData {
		s.testData[i] = make([]byte, rand.Intn(1024*16-1)+1)
		_, _ = rand.Read(s.testData[i])
	}

	s.targetAddr = &DomainNameAddr{"target.server", 12345}
	s.expReq = "CONNECT target.server:12345 HTTP/1.1\r\n" +
		"Host: target.server:12345\r\n" +
		"Proxy-Connection: keep-alive\r\n" +
		"User-Agent: " + httpUserAgent + "\r\n\r\n"
}

func (s *HTTPTunnelTestSuite) mockServer(
	l net.Listener, code int, svrSend bool) {
	reqBuf := make([]byte, len(s.expReq))

	conn, err := l.Accept()
	s.Require().NoError(err)

	_, err = io.ReadFull(conn, reqBuf)
	s.Require().NoError(err)
	s.Require().EqualValues(s.expReq, reqBuf)

	_, err = fmt.Fprintf(
		conn, "HTTP/1.1 %d Response\r\nExtra: header\r\n\r\n", code)
	s.Require().NoError(err)

	if code == 200 {
		for _, data := range s.testData {
			if svrSend {
				_, err = conn.Write(data)
				s.Require().NoError(err)
			}

			buf := GlobalBufPool.Get(uint(len(data)))
			_, err = io.ReadFull(conn, buf)
			s.NoError(err)
			s.Equal(data, buf)
			GlobalBufPool.Free(buf)

			if !svrSend {
				_, err = conn.Write(data)
				s.Require().NoError(err)
			}
		}
	}
}

func (s *HTTPTunnelTestSuite) doTest(code int, svrSend bool) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	s.Require().NoError(err)
	defer l.Close() // nolint: errcheck

	go s.mockServer(l, code, svrSend)

	cfg := ProxyConfig{
		Protocol: "http",
		Settings: map[string]interface{}{"address": l.Addr().String()},
	}
	cli, err := CreateProxyClient(cfg)
	s.Require().NoError(err)
	rwc, _, pErr := cli.Request(context.Background(), s.targetAddr)
	if code != 200 {
		s.Error(pErr.Error)
		if code/100 == 4 {
			s.EqualValues(ProxyCmdUnsupported, pErr.ErrType)
		} else if code/100 == 5 {
			s.EqualValues(ProxyConnectFailed, pErr.ErrType)
		} else {
			s.EqualValues(ProxyGeneralErr, pErr.ErrType)
		}
		return
	}
	s.Require().Nil(pErr)

	for _, data := range s.testData {
		if !svrSend {
			_, err = rwc.Write(data)
			s.Require().NoError(err)
		}

		buf := GlobalBufPool.Get(uint(len(data)))
		_, err = io.ReadFull(rwc, buf)
		s.NoError(err)
		s.Equal(data, buf)
		GlobalBufPool.Free(buf)

		if svrSend {
			_, err = rwc.Write(data)
			s.Require().NoError(err)
		}
	}
}

func (s *HTTPTunnelTestSuite) TestSuccess() {
	s.doTest(200, false)
}

func (s *HTTPTunnelTestSuite) TestSuccessSvrSend() {
	s.doTest(200, true)
}

func (s *HTTPTunnelTestSuite) Test4XX() {
	s.doTest(400, false)
}

func (s *HTTPTunnelTestSuite) Test5XX() {
	s.doTest(503, false)
}

func (s *HTTPTunnelTestSuite) TestOtherError() {
	s.doTest(304, false)
}

func TestHTTPTunnelSuite(t *testing.T) {
	suite.Run(t, new(HTTPTunnelTestSuite))
}
