package main

import (
	"context"
	"io"
	"math/rand"
	"runtime"
	"sync"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"
)

const (
	defaultConnectTimeout = time.Minute * 1
	relayBufferSize       = 32 * 1024
	enableReadFrom        = runtime.GOOS != "darwin" &&
		runtime.GOOS != "nacl" &&
		runtime.GOOS != "netbsd" &&
		runtime.GOOS != "openbsd"
)

// Thestral is the main thestral app.
type Thestral struct {
	log            *zap.SugaredLogger
	downstreams    map[string]ProxyServer
	upstreams      map[string]ProxyClient
	upstreamNames  []string
	ruleMatcher    *RuleMatcher
	relayBufPool   *sync.Pool
	connectTimeout time.Duration
}

// NewThestralApp creates a Thestral app object from the given configuration.
func NewThestralApp(config Config) (app *Thestral, err error) {
	if len(config.Downstreams) == 0 {
		err = errors.New("no downstream server defined")
	}
	if err == nil && len(config.Upstreams) == 0 {
		err = errors.New("no upstream server defined")
	}

	app = &Thestral{
		downstreams: make(map[string]ProxyServer),
		upstreams:   make(map[string]ProxyClient),
		relayBufPool: &sync.Pool{
			New: func() interface{} {
				return make([]byte, relayBufferSize)
			},
		},
	}

	// create logger
	if err == nil {
		app.log, err = CreateLogger(config.Logging)
		if err != nil {
			err = errors.WithMessage(err, "failed to create logger")
		}
	}

	// create downstream servers
	if err == nil {
		dsLogger := app.log.Named("downstreams")
		for k, v := range config.Downstreams {
			app.downstreams[k], err = CreateProxyServer(dsLogger.Named(k), v)
			if err != nil {
				err = errors.WithMessage(
					err, "failed to create downstream server: "+k)
				break
			}
		}
	}

	// create upstream clients
	if err == nil {
		for k, v := range config.Upstreams {
			app.upstreams[k], err = CreateProxyClient(v)
			if err != nil {
				err = errors.WithMessage(
					err, "failed to create upstream client: "+k)
				break
			}
			app.upstreamNames = append(app.upstreamNames, k)
		}
	}

	// create rule matcher
	if err == nil {
		app.ruleMatcher, err = NewRuleMatcher(config.Rules)
		if err != nil {
			err = errors.WithMessage(err, "failed to create rule matcher")
		}
	}
	if err == nil {
		for _, ruleUpstream := range app.ruleMatcher.AllUpstreams {
			if _, ok := app.upstreams[ruleUpstream]; !ok {
				err = errors.Errorf(
					"undefined upstream '%s' used in the rule set",
					ruleUpstream)
			}
		}
	}

	// parse other settings
	if err == nil {
		if config.Misc.ConnectTimeout != "" {
			app.connectTimeout, err = time.ParseDuration(
				config.Misc.ConnectTimeout)
			if err != nil {
				err = errors.WithStack(err)
			}
			if err == nil && app.connectTimeout <= 0 {
				err = errors.New("'connect_timeout' should be greater than 0")
			}
		} else {
			app.connectTimeout = defaultConnectTimeout
		}
	}

	return
}

// Run starts the thestral app and blocks until the context is canceled.
func (t *Thestral) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	for dsName, server := range t.downstreams {
		reqCh, err := server.Start()
		if err != nil {
			t.log.Errorw(
				"failed to start downstream server: "+dsName, "error", err)
			return err
		}

		wg.Add(1)
		go func(reqCh <-chan ProxyRequest, dsName string, server ProxyServer) {
			log := t.log.Named("downstreams").Named(dsName)
			log.Infof("downstream server started: %s", dsName)

			t.processRequests(ctx, reqCh) // blocks

			server.Stop()
			log.Infof("downstream server stopped: %s", dsName)
			wg.Done()
		}(reqCh, dsName, server)
	}

	t.log.Info("thestral app started")
	wg.Wait()
	return nil
}

func (t *Thestral) processRequests(
	ctx context.Context, reqCh <-chan ProxyRequest) {
	for {
		select {
		case req := <-reqCh:
			req.Logger().Infow("request accepted", "target", req.TargetAddr())
			go t.processOneRequest(ctx, req)
		case <-ctx.Done():
			return
		}
	}
}

func (t *Thestral) processOneRequest(ctx context.Context, req ProxyRequest) {
	// match against rule set
	ruleName := ""
	var upstreams []string
	switch addr := req.TargetAddr().(type) {
	case *TCP4Addr:
		ruleName, upstreams = t.ruleMatcher.MatchIP(addr.IP)
	case *TCP6Addr:
		ruleName, upstreams = t.ruleMatcher.MatchIP(addr.IP)
	case *DomainNameAddr:
		ruleName, upstreams = t.ruleMatcher.MatchDomain(addr.DomainName)
	default:
		req.Logger().Errorw("unknown target address", "addr", addr)
		req.Fail(&ProxyError{nil, ProxyAddrUnsupported})
		return
	}

	// select an upstream
	if ruleName == "" { // unmatch and no default rule, allow all
		upstreams = t.upstreamNames
	} else if len(upstreams) == 0 { // no upstream, reject
		req.Logger().Errorw(
			"request rejected by rule",
			"rule", ruleName, "addr", req.TargetAddr())
		req.Fail(&ProxyError{nil, ProxyNotAllowed})
		return
	}
	//TODO: the selection is not actually uniform, fix it
	selected := upstreams[rand.Intn(len(upstreams))]
	req.Logger().Debugw(
		"upstream selected",
		"rule", ruleName, "upstream", selected, "addr", req.TargetAddr())
	upstream := t.upstreams[selected]

	// make request
	reqCtx, cancelFunc := context.WithTimeout(ctx, t.connectTimeout)
	defer cancelFunc()
	upConn, boundAddr, pErr := upstream.Request(reqCtx, req.TargetAddr())
	if pErr != nil {
		req.Logger().Errorw(
			"connection failed", "addr", req.TargetAddr(),
			"error", pErr.Error, "errType", pErr.ErrType)
		req.Fail(pErr)
		return
	}
	t.doRelay(ctx, req, boundAddr, upConn) // block
}

func (t *Thestral) doRelay(
	ctx context.Context, req ProxyRequest,
	boundAddr Address, upRWC io.ReadWriteCloser) {
	// notify the downstream
	req.Logger().Infow(
		"connection established",
		"addr", req.TargetAddr(), "boundAddr", boundAddr)
	downRWC := req.Success(boundAddr)

	relayCtx, cancelFunc := context.WithCancel(ctx)
	relay := func(dst, src io.ReadWriteCloser, dstName, srcName string) {
		defer cancelFunc()
		n, err := t.relayHalf(dst, src)
		if err == nil { // src closed
			req.Logger().Infow(
				"connection closed", "src", srcName, "bytes_transferred", n)
		} else if relayCtx.Err() == context.Canceled { // other direction closed
			req.Logger().Infow(
				"relay ended", "src", srcName, "bytes_transferred", n)
		} else { // error
			req.Logger().Warnw(
				"error occurred",
				"error", err, "src", srcName, "bytes_transferred", n)
		}
	}

	go relay(upRWC, downRWC, "upstream", "downstream")
	go relay(downRWC, upRWC, "downstream", "upstream")

	<-relayCtx.Done() // block until done/canceled
	if err := upRWC.Close(); err != nil {
		req.Logger().Warnw(
			"error occurred when closing upstream", "error", err)
	}
	if err := downRWC.Close(); err != nil {
		req.Logger().Warnw(
			"error occurred when closing downstream", "error", err)
	}
}

func (t *Thestral) relayHalf(
	dst io.Writer, src io.Reader) (n int64, err error) {
	if wt, ok := src.(io.WriterTo); ok {
		n, err = wt.WriteTo(dst)

	} else if rt, ok := dst.(io.ReaderFrom); enableReadFrom && ok {
		n, err = rt.ReadFrom(src)

	} else {
		buf := t.relayBufPool.Get().([]byte)
		defer t.relayBufPool.Put(buf)
		for {
			var nr, nw int
			if nr, err = src.Read(buf); err == nil { // data read from src
				nw, err = dst.Write(buf[:nr])
				n += int64(nw)
				if err != nil { // write failed
					break
				}
				if nw < nr {
					err = io.ErrShortWrite
					break
				}
			} else { // EOF or error occurred
				if err == io.EOF { // ended
					err = nil
				}
				break
			}
		}
	}

	err = errors.WithStack(err)
	return
}
