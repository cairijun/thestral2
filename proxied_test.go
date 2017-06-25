package main

import (
	"context"
	"io"
	"math/rand"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func startEchoServer() (net.Listener, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	go func() {
		defer l.Close() // nolint: errcheck
		for {
			if cli, err := l.Accept(); err == nil {
				_, _ = io.Copy(cli, cli)
			} else {
				break
			}
		}
	}()

	return l, nil
}

func TestProxiedTransport(t *testing.T) {
	targetSvr, err := startEchoServer()
	require.NoError(t, err)
	defer targetSvr.Close() // nolint: errcheck
	addr := targetSvr.Addr().String()

	trans, err := CreateTransport(
		&TransportConfig{Proxied: &ProxyConfig{Protocol: "direct"}})
	require.NoError(t, err)

	cli, err := trans.Dial(context.Background(), addr)
	require.NoError(t, err)
	defer cli.Close() // nolint: errcheck
	for i := 0; i < 10; i++ {
		data := GlobalBufPool.Get(uint(rand.Intn(16 * 1024)))
		_, _ = rand.Read(data)

		_, err = cli.Write(data)
		require.NoError(t, err)

		readBuf := GlobalBufPool.Get(uint(len(data)))
		_, err = io.ReadFull(cli, readBuf)
		require.NoError(t, err)
		require.Equal(t, data, readBuf)

		GlobalBufPool.Free(readBuf)
		GlobalBufPool.Free(data)
	}
}
