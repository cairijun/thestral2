package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/rand"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

var packetTestCases = []struct {
	packet socksPacket
	newPkt socksPacket
	bytes  []byte
}{
	{
		&socksHello{[]uint8{0x00, 0x02}},
		&socksHello{},
		[]byte{0x05, 0x02, 0x00, 0x02},
	},
	{
		&socksHello{make([]uint8, 256)},
		&socksHello{},
		nil,
	},
	{
		&socksSelect{0x00},
		&socksSelect{},
		[]byte{0x05, 0x00},
	},
	{
		&socksUserPassReq{"user", "pass"},
		&socksUserPassReq{},
		[]byte{0x01, 0x04, 0x75, 0x73, 0x65, 0x72, 0x04, 0x70, 0x61, 0x73, 0x73},
	},
	{
		&socksUserPassReq{"", "pass"},
		&socksUserPassReq{},
		nil,
	},
	{
		&socksUserPassReq{"user", string(make([]rune, 256))},
		&socksUserPassReq{},
		nil,
	},
	{
		&socksUserPassResp{true},
		&socksUserPassResp{},
		[]byte{0x01, 0x00},
	},
	{
		&socksUserPassResp{false},
		&socksUserPassResp{},
		[]byte{0x01, 0x01},
	},
	{
		&socksReqResp{socksConnect,
			&TCP4Addr{IP: net.ParseIP("123.45.67.89").To4(), Port: 12345}},
		&socksReqResp{},
		[]byte{0x05, 0x01, 0x00, 0x01, 0x7b, 0x2d, 0x43, 0x59, 0x30, 0x39},
	},
	{
		&socksReqResp{socksSuccess,
			&TCP6Addr{IP: net.ParseIP("fe80::c41:9110:fc11"), Port: 12345}},
		&socksReqResp{},
		[]byte{0x05, 0x00, 0x00, 0x04, 0xfe, 0x80,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0x0c, 0x41, 0x91, 0x10, 0xfc, 0x11, 0x30, 0x39},
	},
	{
		&socksReqResp{socksConnect, &DomainNameAddr{"www.gov.cn", 12345}},
		&socksReqResp{},
		[]byte{0x05, 0x01, 0x00, 0x03, 0x0a, 0x77, 0x77, 0x77,
			0x2e, 0x67, 0x6f, 0x76, 0x2e, 0x63, 0x6e, 0x30, 0x39},
	},
	{
		&socksReqResp{socksConnect, &DomainNameAddr{string(make([]rune, 256)), 12345}},
		&socksReqResp{},
		nil,
	},
	{
		&socksReqResp{socksConnect, nil},
		&socksReqResp{},
		nil,
	},
}

func TestSOCKS5Packets(t *testing.T) {
	buf := new(bytes.Buffer)
	for i, c := range packetTestCases {
		t.Run(fmt.Sprintf("%d", i), func(t *testing.T) {
			buf.Reset()
			err := c.packet.WritePacket(buf)
			if c.bytes == nil {
				assert.Error(t, err, "marshaling error not reported")
				return
			}
			assert.Equal(t, c.bytes, buf.Bytes())

			reader := bytes.NewReader(c.bytes)
			err = c.newPkt.ReadPacket(reader)
			if assert.NoError(t, err) {
				assert.Equal(t, c.packet, c.newPkt)
			}
			for n := range c.bytes {
				_, _ = reader.Seek(0, io.SeekStart)
				err = c.newPkt.ReadPacket(io.LimitReader(reader, int64(n)))
				require.Error(t, err, // nolint: vet
					"incomplete error not reported at %d", n)
			}
		})
	}
}

func doTestSOCKS5Request(
	t *testing.T, addr Address, simplified bool,
	checkUserFunc CheckUserFunc, provideUser, shouldFail bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	address := "127.0.0.1:" + strconv.Itoa(52048+(rand.Intn(2048)))
	trans := &TCPTransport{}

	logger := zap.NewNop().Sugar()
	svr, err := newSOCKS5Server(
		logger, trans, address, simplified, checkUserFunc)
	require.NoError(t, err)

	reqCh, err := svr.Start()
	require.NoError(t, err)
	go func() {
		select {
		case req := <-reqCh:
			actual := req.TargetAddr()
			if assert.Equal(t, addr.String(), actual.String()) {
				conn := req.Success(
					&TCP4Addr{net.ParseIP("123.45.67.89").To4(), 23333})
				_, _ = conn.Write([]byte("hello"))
				_ = conn.Close()
			} else {
				req.Fail(
					wrapAsProxyError(errors.New("mismatch"), ProxyGeneralErr))
			}
		case <-ctx.Done():
		}
	}()

	cli := &SOCKS5Client{
		Transport: trans, Addr: address, Simplified: simplified}
	if provideUser {
		cli.Username = "USERNAME"
		cli.Password = "PASSWORD"
	}
	conn, boundAddr, pErr := cli.Request(ctx, addr)
	if shouldFail {
		require.NotNil(
			t, pErr, "this test should fail when requesting, but did not")
		return
	}
	require.Nil(t, pErr)
	assert.Equal(t, &TCP4Addr{net.ParseIP("123.45.67.89").To4(), 23333}, boundAddr)
	buf := make([]byte, 5)
	_, err = io.ReadFull(conn, buf)
	assert.NoError(t, err)
	assert.EqualValues(t, "hello", buf)

	svr.Stop()
}

func TestSOCKS5RequestIPv4(t *testing.T) {
	addr := &TCP4Addr{IP: net.ParseIP("123.45.67.89"), Port: 23333}
	doTestSOCKS5Request(t, addr, false, nil, false, false)
}

func TestSOCKS5RequestIPv6(t *testing.T) {
	addr := &TCP6Addr{IP: net.ParseIP("fe80::fc73:4566:1057"), Port: 6666}
	doTestSOCKS5Request(t, addr, false, nil, false, false)
}

func TestSOCKS5RequestDomainName(t *testing.T) {
	addr := &DomainNameAddr{DomainName: "www.gov.cn", Port: 12345}
	doTestSOCKS5Request(t, addr, false, nil, false, false)
}

func TestSOCKS5RequestUserPassAuth(t *testing.T) {
	addr := &DomainNameAddr{DomainName: "www.gov.cn", Port: 12345}
	doTestSOCKS5Request(t, addr, false, func(user, pass string) bool {
		return user == "USERNAME" && pass == "PASSWORD"
	}, true, false)
}

func TestSOCKS5RequestRequireNoAuth(t *testing.T) {
	addr := &DomainNameAddr{DomainName: "www.gov.cn", Port: 12345}
	doTestSOCKS5Request(t, addr, false, nil, true, false)
}

func TestSOCKS5RequestNoUserPass(t *testing.T) {
	addr := &DomainNameAddr{DomainName: "www.gov.cn", Port: 12345}
	doTestSOCKS5Request(t, addr, false, func(user, pass string) bool {
		return user == "USERNAME" && pass == "PASSWORD"
	}, false, true)
}

func TestSOCKS5RequestWrongUserPass(t *testing.T) {
	addr := &DomainNameAddr{DomainName: "www.gov.cn", Port: 12345}
	doTestSOCKS5Request(t, addr, false, func(user, pass string) bool {
		return user == "USERNAME" && pass == "DIFFERENT_PASSWORD"
	}, true, true)
}

func TestSOCKS5RequestSimplifiedProtocol(t *testing.T) {
	addr := &DomainNameAddr{DomainName: "www.gov.cn", Port: 12345}
	doTestSOCKS5Request(t, addr, true, nil, false, false)
}
