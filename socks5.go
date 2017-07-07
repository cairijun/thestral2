package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
	. "github.com/richardtsai/thestral2/lib"
	"go.uber.org/zap"
)

// CheckUserFunc is the type of user checking callback function.
type CheckUserFunc func(user, password string) bool

// SOCKS5Server is a proxy server on SOCKS5 protocol.
type SOCKS5Server struct {
	transport  Transport
	addr       string
	checkUser  CheckUserFunc
	simplified bool
	isRunning  uint32 // should be used with atomic operations
	listener   net.Listener
	reqCh      chan ProxyRequest
	log        *zap.SugaredLogger
}

func parseSOCKS5Config(
	config ProxyConfig) (address string, simplified bool, err error) {
	if config.Protocol != "socks5" {
		panic("protocol should be 'socks5' rather than: " + config.Protocol)
	}

	var ok bool
	for k, v := range config.Settings {
		switch k {
		case "address":
			if address, ok = v.(string); !ok {
				err = errors.Errorf("invalid value for 'address': %v", v)
			}
		case "simplified":
			if simplified, ok = v.(bool); !ok {
				err = errors.Errorf("invalid value for 'simplified': %v", v)
			}
		default:
			err = errors.New("invalid setting for SOCKS5 protocol: " + k)
		}
	}

	if address == "" {
		err = errors.New(
			"a valid 'address' must be specified for socks5 protocol")
	}
	return
}

// NewSOCKS5Server creates a SOCKS5Server from the given configuration.
func NewSOCKS5Server(
	logger *zap.SugaredLogger,
	config ProxyConfig) (*SOCKS5Server, error) {
	address, simplified, err := parseSOCKS5Config(config)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to create SOCKS5 server")
	}

	transport, err := CreateTransport(config.Transport)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to create SOCKS5 server")
	}

	return newSOCKS5Server(logger, transport, address, simplified, nil)
}

// newSOCKS5Server creates a SOCKS5Server. It is used internally.
func newSOCKS5Server(
	logger *zap.SugaredLogger,
	transport Transport, addr string, simplified bool,
	checkUser CheckUserFunc) (*SOCKS5Server, error) {
	if simplified && checkUser != nil {
		return nil, errors.New(
			"simplified SOCKS5 does not support authentication")
	}
	return &SOCKS5Server{
		transport:  transport,
		addr:       addr,
		simplified: simplified,
		checkUser:  checkUser,
		log:        logger,
	}, nil
}

// Start fires up the SOCKS5Server and returns a channel of client requests.
func (s *SOCKS5Server) Start() (<-chan ProxyRequest, error) {
	s.reqCh = make(chan ProxyRequest, 1)

	var err error
	if s.listener, err = s.transport.Listen(s.addr); err != nil {
		s.log.Errorw(
			"failed to start SOCKS5 server", "addr", s.addr, "error", err)
		return nil, errors.WithMessage(err, "failed to start SOCKS5 server")
	}
	s.log.Infow(
		"SOCKS5 server started", "addr", s.addr, "simplified", s.simplified)

	atomic.StoreUint32(&s.isRunning, 1)
	go func() {
		for {
			conn, err := s.listener.Accept()
			if err != nil {
				if atomic.LoadUint32(&s.isRunning) > 0 { // still running
					s.log.Warnw("accept error", "error", err)
				}
				break
			}

			reqID := GetNextRequestID()
			cliLogger := s.log.With("reqID", reqID).Named("client")
			cliLogger.Debugw(
				"client connection accepted", "addr", conn.RemoteAddr())
			req := &socks5Request{
				id: GetNextRequestID(), conn: conn, log: cliLogger}

			go s.handshake(req)
		}
		s.log.Infow("SOCKS5 server exited")
	}()

	return s.reqCh, nil
}

// Stop kill the server.
func (s *SOCKS5Server) Stop() {
	s.log.Infow("stopping SOCKS5 server")
	atomic.StoreUint32(&s.isRunning, 0)
	err := s.listener.Close()
	if err != nil {
		s.log.Warnw("error occurred when closing listener", "error", err)
	}
}

func (s *SOCKS5Server) handshake(cli *socks5Request) {
	var err error
	if !s.simplified {
		// authenticate
		helloPkt := &socksHello{}
		err = helloPkt.ReadPacket(cli.conn)
		if err == nil {
			if s.checkUser != nil {
				if bytes.IndexByte(helloPkt.Methods, socksUserPass) >= 0 {
					cli.user, err = s.authUser(cli)
				} else {
					err = errors.New("client doesn't support socksUserPass")
					_ = (&socksSelect{0xff}).WritePacket(cli.conn)
				}
			} else {
				if bytes.IndexByte(helloPkt.Methods, socksNoAuth) >= 0 {
					err = (&socksSelect{socksNoAuth}).WritePacket(cli.conn)
				} else {
					err = errors.New("client doesn't support socksNoAuth")
					_ = (&socksSelect{0xff}).WritePacket(cli.conn)
				}
			}
		}
	}

	// get request
	reqPkt := &socksReqResp{}
	if err == nil {
		err = reqPkt.ReadPacket(cli.conn)
	}

	if err == nil {
		if reqPkt.Type == socksConnect {
			// the response packet will be sent by cli.Success()
			cli.targetAddr = reqPkt.Addr
		} else {
			err = errors.Errorf("client sent unsupported cmd: %d", reqPkt.Type)
			reqPkt.Type = byte(ProxyCmdUnsupported)
			_ = reqPkt.WritePacket(cli.conn)
		}
	} else if addrErr, isAddrError := err.(addrError); isAddrError {
		err = addrErr.error
		reqPkt.Type = byte(ProxyAddrUnsupported)
		_ = reqPkt.WritePacket(cli.conn)
	}

	var peerIDs []*PeerIdentifier
	if err == nil {
		peerIDs, err = cli.GetPeerIdentifiers()
	}
	if err == nil {
		cli.log.Debugw(
			"handshake with SOCKS5 client succeeded",
			"target", cli.targetAddr, "user_ids", peerIDs)
		s.reqCh <- cli
	} else {
		cli.log.Warnw(
			"handshake with SOCKS5 client failed",
			"error", err, "user_ids", peerIDs)
		_ = cli.conn.Close()
	}
}

func (s *SOCKS5Server) authUser(cli *socks5Request) (user string, err error) {
	cli.log.Debugw("start user/pass authentication")
	err = (&socksSelect{socksUserPass}).WritePacket(cli.conn)

	authPkt := &socksUserPassReq{}
	if err == nil {
		err = authPkt.ReadPacket(cli.conn)
	}

	if err == nil {
		if s.checkUser(authPkt.Username, authPkt.Password) {
			err = (&socksUserPassResp{true}).WritePacket(cli.conn)
		} else {
			cli.log.Warnw("user authentication failed", "user", authPkt.Username)
			err = errors.New("checkUser returned false")
			_ = (&socksUserPassResp{false}).WritePacket(cli.conn)
		}
	}

	return authPkt.Username, errors.WithMessage(err, "user auth failed")
}

type socks5Request struct {
	id         string
	log        *zap.SugaredLogger
	conn       net.Conn
	user       string
	targetAddr Address
}

// GetPeerIdentifiers returns a list of peer identifiers of this client.
func (r *socks5Request) GetPeerIdentifiers() ([]*PeerIdentifier, error) {
	var ids []*PeerIdentifier
	if r.user != "" {
		ids = append(ids, &PeerIdentifier{
			Scope:    "proxy.socks5",
			UniqueID: r.user,
		})
	}
	if withID, ok := r.conn.(WithPeerIdentifiers); ok {
		connIDs, err := withID.GetPeerIdentifiers()
		if err != nil {
			return nil, errors.WithMessage(err, "failed to get peerIDs")
		}
		ids = append(ids, connIDs...)
	}
	return ids, nil
}

// PeerAddr returns the address of the client.
func (r *socks5Request) PeerAddr() string {
	return r.conn.RemoteAddr().String()
}

// TargetAddr returns the address the client wants to connect to.
func (r *socks5Request) TargetAddr() Address {
	return r.targetAddr
}

// Success notifies the client that the connection is established.
func (r *socks5Request) Success(addr Address) io.ReadWriteCloser {
	respPkt := &socksReqResp{Type: socksSuccess, Addr: addr}
	if err := respPkt.WritePacket(r.conn); err != nil {
		// if it is actually a fatal error, the upper level code
		// would notice it when operating on the returned conn
		r.log.Warnw("failed to write response packet", "error", err)
	}
	return r.conn
}

// Fail notifies the client that the connection is not able to be established.
func (r *socks5Request) Fail(proxyErr *ProxyError) {
	respPkt := &socksReqResp{
		Type: byte(proxyErr.ErrType), Addr: &TCP4Addr{net.IPv4zero, 0}}
	if err := respPkt.WritePacket(r.conn); err != nil {
		r.log.Warnw("failed to write error response packet", "error", err)
	}
	if err := r.conn.Close(); err != nil {
		r.log.Warnw("failed to close client connection", "error", err)
	}
}

// Logger returns a logger of this client.
func (r *socks5Request) Logger() *zap.SugaredLogger {
	return r.log
}

// ID returns the identifier of this client.
func (r *socks5Request) ID() string {
	return r.id
}

// SOCKS5Client is a ProxyClient using SOCKS5 protocol.
type SOCKS5Client struct {
	Transport  Transport
	Addr       string
	Simplified bool
	Username   string
	Password   string
}

// NewSOCKS5Client creates a SOCKS5 client from the given configuration.
func NewSOCKS5Client(config ProxyConfig) (*SOCKS5Client, error) {
	address, simplified, err := parseSOCKS5Config(config)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to create SOCKS5 client")
	}

	transport, err := CreateTransport(config.Transport)
	if err != nil {
		return nil, errors.WithMessage(err, "failed to create SOCKS5 client")
	}

	return &SOCKS5Client{
		Transport: transport, Addr: address, Simplified: simplified,
	}, nil
}

// Request send a connection request to the proxy server.
func (c *SOCKS5Client) Request(ctx context.Context, addr Address) (
	io.ReadWriteCloser, Address, *ProxyError) {
	conn, err := c.Transport.Dial(ctx, c.Addr)
	if err != nil {
		return nil, nil, wrapAsProxyError(
			errors.WithMessage(err, "failed to dial to proxy server"),
			ProxyGeneralErr)
	}
	if ddl, hasDDL := ctx.Deadline(); hasDDL {
		// so that the underlying IO will propagate the timeout error upwards
		_ = conn.SetDeadline(ddl.Add(-time.Millisecond))
	}

	var boundAddr Address
	errCh := make(chan *ProxyError, 1)
	go func() {
		bAddr, pErr := c.doRequest(conn, addr)
		boundAddr = bAddr
		errCh <- pErr
	}()

	select {
	case err := <-errCh:
		if err != nil {
			_ = conn.Close()
			return nil, nil, err
		}
		_ = conn.SetDeadline(time.Time{})
		return conn, boundAddr, nil
	case <-ctx.Done():
		_ = conn.Close()
		return nil, nil, wrapAsProxyError(
			errors.WithStack(ctx.Err()), ProxyGeneralErr)
	}
}

func (c *SOCKS5Client) doRequest(
	conn io.ReadWriter, addr Address) (Address, *ProxyError) {
	var err error
	errType := ProxyGeneralErr
	if !c.Simplified {
		err = c.authenticate(conn)
	}

	// send connect request
	reqPkt := &socksReqResp{Type: socksConnect, Addr: addr}
	respPkt := &socksReqResp{}
	if err == nil {
		err = reqPkt.WritePacket(conn)
		if addrErr, isAddrErr := err.(addrError); isAddrErr {
			err = addrErr.error
			errType = ProxyAddrUnsupported
		}
	}
	if err == nil {
		if err = respPkt.ReadPacket(conn); err == nil {
			if respPkt.Type != socksSuccess {
				// socks error codes are identical to those of ProxyError
				errType = ProxyErrorType(respPkt.Type)
				err = errors.Errorf("SOCKS server replies %s", errType)
			}
		}
	}

	return respPkt.Addr, wrapAsProxyError(
		errors.WithMessage(err, "failed to establish SOCKS connection"),
		errType)
}

func (c *SOCKS5Client) authenticate(conn io.ReadWriter) (err error) {
	// send HELLO and authenticate if required
	helloPkt := &socksHello{[]byte{socksNoAuth}}
	selectPkt := &socksSelect{}
	if len(c.Username) > 0 && len(c.Password) > 0 {
		helloPkt.Methods = append(helloPkt.Methods, socksUserPass)
	}
	if err = helloPkt.WritePacket(conn); err != nil {
		return
	}
	if err = selectPkt.ReadPacket(conn); err != nil {
		return
	}

	switch selectPkt.Method {
	case socksUserPass:
		authReqPkt := &socksUserPassReq{c.Username, c.Password}
		authRespPkt := &socksUserPassResp{}
		if err = authReqPkt.WritePacket(conn); err == nil {
			err = authRespPkt.ReadPacket(conn)
		}
		if err == nil && !authRespPkt.Status {
			err = errors.New("authentication to SOCKS server failed")
		}
	case socksNoAuth: // no-op
	default:
		err = errors.New("SOCKS server require unknown authentication")
	}
	return
}

const (
	socksVersion    = 0x05
	socksNoAuth     = 0x00
	socksUserPass   = 0x02
	socksConnect    = 0x01
	socksIPv4       = 0x01
	socksDomainName = 0x03
	socksIPv6       = 0x04
	socksSuccess    = 0x00
)

type socksPacket interface { // nolint: deadcode
	WritePacket(writer io.Writer) error
	ReadPacket(reader io.Reader) error
}

type socksHello struct {
	Methods []byte
}

func (p *socksHello) WritePacket(writer io.Writer) error {
	n := len(p.Methods)
	if n <= 0 || n > 255 {
		return errors.Errorf("invalid number of methods: %d", n)
	}
	_, err := writer.Write([]byte{socksVersion, byte(n)})
	if err == nil {
		_, err = writer.Write(p.Methods)
	}
	return errors.Wrap(err, "failed to write socksHello")
}

func (p *socksHello) ReadPacket(reader io.Reader) error {
	buf := make([]byte, 2)
	_, err := io.ReadFull(reader, buf[:2])
	if err == nil {
		if buf[0] != 0x05 && buf[0] != 0x04 {
			return errors.Errorf("unknown SOCKS version: %d", buf[0])
		}
		n := int(buf[1])
		p.Methods = make([]byte, n)
		_, err = io.ReadFull(reader, p.Methods)
	}
	return errors.Wrap(err, "failed to read socksHello")
}

type socksSelect struct {
	Method byte
}

func (p *socksSelect) WritePacket(writer io.Writer) error {
	_, err := writer.Write([]byte{socksVersion, p.Method})
	return errors.Wrap(err, "failed to write socksSelect")
}

func (p *socksSelect) ReadPacket(reader io.Reader) error {
	buf := make([]byte, 2)
	_, err := io.ReadFull(reader, buf)
	if err == nil {
		if buf[0] != 0x05 && buf[0] != 0x04 {
			return errors.Errorf("unknown SOCKS version: %d", buf[0])
		}
		p.Method = buf[1]
	}
	return errors.Wrap(err, "failed to read socksSelect")
}

type socksUserPassReq struct {
	Username string
	Password string
}

func (p *socksUserPassReq) WritePacket(writer io.Writer) error {
	lenUser := len(p.Username)
	lenPass := len(p.Password)
	if lenUser <= 0 || lenUser > 255 {
		return errors.Errorf("invalid username length: %d", lenUser)
	}
	if lenPass <= 0 || lenPass > 255 {
		return errors.Errorf("invalid password length: %d", lenPass)
	}

	buf := make([]byte, lenUser+lenPass+3)
	buf[0] = 0x01 // negotiation version
	buf[1] = byte(lenUser)
	copy(buf[2:], p.Username)
	buf[2+lenUser] = byte(lenPass)
	copy(buf[3+lenUser:], p.Password)

	_, err := writer.Write(buf)
	return errors.Wrap(err, "failed to write socksUserPassReq")
}

func (p *socksUserPassReq) ReadPacket(reader io.Reader) error {
	buf := make([]byte, 256)
	n := 0
	_, err := io.ReadFull(reader, buf[:2])
	if err == nil {
		if buf[0] != 0x01 {
			return errors.Errorf("unknown negotiation version: %d", buf[0])
		}
		n = int(buf[1])
		_, err = io.ReadFull(reader, buf[:n])
	}
	if err == nil {
		p.Username = string(buf[:n])
		_, err = io.ReadFull(reader, buf[:1])
	}
	if err == nil {
		n = int(buf[0])
		_, err = io.ReadFull(reader, buf[:n])
	}
	if err == nil {
		p.Password = string(buf[:n])
	}
	return errors.Wrap(err, "failed to read socksUserPassReq")
}

type socksUserPassResp struct {
	Status bool
}

func (p *socksUserPassResp) WritePacket(writer io.Writer) error {
	buf := []byte{0x01, 0x01} // failure
	if p.Status {
		buf[1] = 0x00 // success
	}
	_, err := writer.Write(buf)
	return errors.Wrap(err, "failed to write socksUserPassResp")
}

func (p *socksUserPassResp) ReadPacket(reader io.Reader) error {
	buf := make([]byte, 2)
	_, err := io.ReadFull(reader, buf)
	if err == nil {
		if buf[0] != 0x01 {
			return errors.Errorf("unknown negotiation version: %d", buf[0])
		}
		p.Status = buf[1] == 0x00
	}
	return errors.Wrap(err, "failed to read socksUserPassResp")
}

type socksReqResp struct {
	Type byte
	Addr Address
}

func (p *socksReqResp) WritePacket(writer io.Writer) error {
	buf := make([]byte, 0, 32)
	buf = append(buf, socksVersion, p.Type, 0x00)

	var port uint16
	switch addr := p.Addr.(type) {
	case *TCP4Addr:
		buf = append(buf, socksIPv4)
		if ip := addr.IP.To4(); ip != nil {
			buf = append(buf, ip...)
		} else {
			return errors.New("invalid TCP4Addr")
		}
		port = addr.Port
	case *TCP6Addr:
		buf = append(buf, socksIPv6)
		if ip := addr.IP.To16(); ip != nil {
			buf = append(buf, ip...)
		} else {
			return errors.New("invalid TCP6Addr")
		}
		port = addr.Port
	case *DomainNameAddr:
		n := len(addr.DomainName)
		if n > 255 {
			return addrError{errors.Errorf("domain name too long: %d", n)}
		}
		buf = append(buf, socksDomainName, byte(n))
		buf = append(buf, addr.DomainName...)
		port = addr.Port
	default:
		return addrError{errors.New("unsupported address type")}
	}

	buf = append(buf, byte(port>>8), byte(port))

	_, err := writer.Write(buf)
	return errors.Wrap(err, "failed to write socksReqResp")
}

func (p *socksReqResp) ReadPacket(reader io.Reader) error {
	buf := make([]byte, 32)
	_, err := io.ReadFull(reader, buf[:4])
	if err == nil {
		if buf[0] != 0x05 && buf[0] != 0x04 {
			return errors.Errorf("unknown SOCKS version: %d", buf[0])
		}
		p.Type = buf[1]
	}

	if err == nil {
		switch buf[3] {
		case socksIPv4:
			_, err = io.ReadFull(reader, buf[:6])
			if err == nil {
				p.Addr = &TCP4Addr{
					IP: buf[:4], Port: getPortFromBytes(buf[4:6])}
			}
		case socksIPv6:
			_, err = io.ReadFull(reader, buf[:18])
			if err == nil {
				p.Addr = &TCP6Addr{
					IP: buf[:16], Port: getPortFromBytes(buf[16:18])}
			}
		case socksDomainName:
			_, err = io.ReadFull(reader, buf[:1])
			nDN := 0
			if err == nil {
				nDN = int(buf[0])
				if len(buf) < nDN+2 {
					buf = make([]byte, nDN+2)
				}
				_, err = io.ReadFull(reader, buf[:nDN+2])
			}
			if err == nil {
				p.Addr = &DomainNameAddr{
					DomainName: string(buf[:nDN]),
					Port:       getPortFromBytes(buf[nDN : nDN+2])}
			}
		default:
			return addrError{
				errors.Errorf("unsupported address type: %d", buf[3])}
		}
	}

	return errors.Wrap(err, "failed to read socksReqResp")
}

func getPortFromBytes(raw []byte) uint16 {
	return uint16(raw[0])<<8 | uint16(raw[1])
}

type addrError struct {
	error
}
