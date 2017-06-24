package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
)

var httpUserAgent string

func init() {
	httpUserAgent = fmt.Sprintf("Thestral/2 (%s %s; %s; %s)",
		runtime.GOOS, runtime.GOARCH, runtime.Version(), ThestralVersion)
}

type HTTPTunnelClient struct {
	Addr string
}

func (c HTTPTunnelClient) Request(ctx context.Context, addr Address) (
	io.ReadWriteCloser, Address, *ProxyError) {
	conn, err := TCPTransport{}.Dial(ctx, c.Addr)
	if err != nil {
		return nil, nil, wrapAsProxyError(err, ProxyGeneralErr)
	}
	if ddl, hasDDL := ctx.Deadline(); hasDDL {
		_ = conn.SetDeadline(ddl.Add(-time.Millisecond))
	}

	brc := &bufReadRWC{bufio.NewReader(conn), conn}
	errCh := make(chan *ProxyError, 1)
	go func() {
		if err := c.sendRequest(brc, addr); err != nil {
			errCh <- err
		} else if err := c.readResponse(brc); err != nil {
			errCh <- err
		} else {
			errCh <- nil
		}
	}()

	select {
	case err := <-errCh:
		if err != nil {
			_ = brc.Close()
			return nil, nil, err
		}
		_ = conn.SetDeadline(time.Time{})
		return brc, &TCP4Addr{net.IPv4zero, 0}, nil
	case <-ctx.Done():
		_ = brc.Close()
		return nil, nil, wrapAsProxyError(
			errors.WithStack(ctx.Err()), ProxyGeneralErr)
	}
}

func (c HTTPTunnelClient) sendRequest(w io.Writer, addr Address) *ProxyError {
	addrStr := addr.String()
	var buf bytes.Buffer
	_, _ = buf.WriteString("CONNECT ")
	_, _ = buf.WriteString(addrStr)
	_, _ = buf.WriteString(" HTTP/1.1\r\nHost: ")
	_, _ = buf.WriteString(addrStr)
	_, _ = buf.WriteString("\r\nProxy-Connection: keep-alive\r\nUser-Agent: ")
	_, _ = buf.WriteString(httpUserAgent)
	_, _ = buf.WriteString("\r\n\r\n")
	_, err := buf.WriteTo(w)
	return wrapAsProxyError(
		errors.WithMessage(err, "failed to send HTTP CONNECT request"),
		ProxyGeneralErr)
}

func (c HTTPTunnelClient) readResponse(brc *bufReadRWC) *ProxyError {
	var err error
	var errType byte = ProxyGeneralErr // default error type
	line, _, err := brc.ReadLine()
	if err != nil {
		err = errors.WithMessage(err, "failed to read from proxy server")
		return wrapAsProxyError(err, errType)
	}

	heading := string(line)
	hFields := strings.Fields(string(line))
	if len(hFields) < 2 {
		err = errors.WithMessage(err, "invalid heading from proxy server")
		return wrapAsProxyError(err, errType)
	}

	code, err := strconv.Atoi(hFields[1])
	if err != nil {
		err = errors.WithMessage(err, "invalid response code: "+hFields[1])
		return wrapAsProxyError(err, errType)
	}

	if code != 200 {
		if code/100 == 4 {
			errType = ProxyCmdUnsupported // maybe...
		} else if code/100 == 5 {
			errType = ProxyConnectFailed
		}
		err = errors.New("proxy server responses: " + heading)
		return wrapAsProxyError(err, errType)
	}

	for { // drop headers
		line, _, err = brc.ReadLine()
		if err != nil {
			err = errors.WithMessage(err, "failed to read from proxy server")
			return wrapAsProxyError(err, errType)
		}
		if len(line) == 0 {
			return nil // done
		}
	}
}

type bufReadRWC struct {
	*bufio.Reader
	c net.Conn
}

func (b *bufReadRWC) Write(p []byte) (n int, err error) {
	return b.c.Write(p)
}

func (b *bufReadRWC) Close() error {
	return b.c.Close()
}
