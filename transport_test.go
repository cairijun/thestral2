package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var gKCPServerConfig = &KCPConfig{
	Mode:     "fast2",
	Optimize: "send",
	FEC:      true,
}
var gKCPClientConfig = &KCPConfig{
	Mode:     "fast2",
	Optimize: "receive",
	FEC:      true,
}
var gTLSClientConfig = &TLSConfig{
	Cert: "test_files/test.pem",
	Key:  "test_files/test.key.pem",
	CAs:  []string{"test_files/ca.pem"},
}
var gTLSServerConfig = &TLSConfig{
	Cert:         "test_files/test.server.pem",
	Key:          "test_files/test.server.key.pem",
	VerifyClient: true,
	ClientCAs:    []string{"test_files/ca.pem"},
}

func TestTransportDefault(t *testing.T) {
	doTestWithTransConf(t, nil, nil)
}

func TestTransport(t *testing.T) {
	for _, compMethod := range []string{"", "lz4", "snappy", "deflate"} {
		for _, tls := range []bool{false, true} {
			for _, kcp := range []bool{false, true} {
				name := fmt.Sprintf(
					"compMethod-%s/tls-%v/kcp-%v", compMethod, tls, kcp)
				t.Run(name, func(t *testing.T) {
					svrConfig := &TransportConfig{Compression: compMethod}
					cliConfig := &TransportConfig{Compression: compMethod}

					if tls {
						svrConfig.TLS = gTLSServerConfig
						cliConfig.TLS = gTLSClientConfig
					}

					if kcp {
						svrConfig.KCP = gKCPServerConfig
						cliConfig.KCP = gKCPClientConfig
					}

					doTestWithTransConf(t, svrConfig, cliConfig)
				})
			}
		}
	}
}

func doTestWithTransConf(t *testing.T, svrConfig, cliConfig *TransportConfig) {
	svrTrans, err := CreateTransport(svrConfig)
	require.NoError(t, err)
	cliTrans, err := CreateTransport(cliConfig)
	require.NoError(t, err)

	address := "127.0.0.1:" + strconv.Itoa(50000+(rand.Intn(2048)))
	listener, err := svrTrans.Listen(address)
	require.NoError(t, err)

	exit := make(chan struct{})
	go runEchoServer(t, listener, exit)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)

		go func() {
			defer wg.Done()
			client, err := cliTrans.Dial(context.Background(), address)
			require.NoError(t, err)
			defer client.Close() // nolint: errcheck
			_ = client.SetDeadline(time.Now().Add(10 * time.Second))

			for _, data := range getRandomData() {
				_, err := client.Write(data)
				require.NoError(t, err)

				buf := make([]byte, len(data))
				_, err = io.ReadFull(client, buf)
				require.NoError(t, err)

				assert.True(t, bytes.Equal(buf, data))
			}
		}()
	}

	wg.Wait()
	close(exit)
	_ = listener.Close()
}

func getRandomData() [][]byte {
	data := make([][]byte, 16)
	for i := range data {
		n := rand.Intn(1024 * 64)
		data[i] = make([]byte, n)
		_, _ = rand.Read(data[i])
	}
	return data
}

func runEchoServer(t *testing.T, listener net.Listener, exit <-chan struct{}) {
	clientProc := func(client net.Conn) {
		defer client.Close() // nolint: errcheck
		var buf [1024 * 32]byte
		for {
			n, err := client.Read(buf[:])
			if err != nil {
				assert.Equal(t, io.EOF, err)
				break
			}

			_, err = client.Write(buf[:n])
			if !assert.NoError(t, err) {
				break
			}
		}
	}

	for {
		conn, err := listener.Accept()
		select {
		case <-exit:
			return
		default:
		}
		if assert.NoError(t, err) {
			go clientProc(conn)
		}
	}
}
