package main

import (
	"bytes"
	"context"
	"io"
	"math/rand"
	"net"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
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
		buf.Reset()
		err := c.packet.WritePacket(buf)
		if c.bytes == nil {
			if err == nil {
				t.Errorf("case %d, marshaling error not reported", i)
			}
			continue
		} else {
			if !bytes.Equal(c.bytes, buf.Bytes()) {
				t.Errorf("case %d, marshaling result mismatch", i)
			}
		}

		reader := bytes.NewReader(c.bytes)
		if err = c.newPkt.ReadPacket(reader); err != nil {
			t.Errorf("case %d, unmarshal failed: %s", i, err)
		}
		if !reflect.DeepEqual(c.packet, c.newPkt) {
			t.Errorf("case %d, unmarshal result mismatch, expected %+v, actual %+v", i, c.packet, c.newPkt)
		}
		for n := range c.bytes {
			_, _ = reader.Seek(0, io.SeekStart)
			if err = c.newPkt.ReadPacket(io.LimitReader(reader, int64(n))); err == nil {
				t.Errorf("case %d, incomplete error not reported at %d", i, n)
			}
		}
	}
}

func doTestSOCKS5Request(
	t *testing.T, addr Address, simplified bool,
	checkUserFunc CheckUserFunc, provideUser, shouldFail bool) {
	ctx, _ := context.WithTimeout(context.Background(), 1*time.Second) // nolint: vet
	address := "localhost:" + strconv.Itoa(52048+(rand.Intn(2048)))
	trans := &TCPTransport{}

	logger := zap.NewNop().Sugar()
	svr, err := newSOCKS5Server(
		logger, trans, address, simplified, checkUserFunc)
	if err != nil {
		t.Fatal(err)
	}

	reqCh, err := svr.Start()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		select {
		case req := <-reqCh:
			actual := req.TargetAddr()
			if addr.String() != actual.String() {
				t.Errorf("addr mismatch, expected %+v, actual %+v", addr, actual)
				req.Fail(wrapAsProxyError(errors.New("mismatch"), ProxyGeneralErr))
			} else {
				conn := req.Success(&TCP4Addr{net.ParseIP("123.45.67.89").To4(), 23333})
				_, _ = conn.Write([]byte("hello"))
				_ = conn.Close()
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
		if pErr == nil {
			t.Error("this test should fail when requesting, but did not")
		}
		return
	}
	if pErr != nil {
		t.Fatalf("failed to send request: %+v", pErr.Error)
	}
	assert.Equal(t, &TCP4Addr{net.ParseIP("123.45.67.89").To4(), 23333}, boundAddr)
	buf := make([]byte, 5)
	_, err = io.ReadFull(conn, buf)
	if err != nil {
		t.Errorf("%+v", err)
	}
	if !bytes.Equal([]byte("hello"), buf) {
		t.Errorf("data mismatch")
	}

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
