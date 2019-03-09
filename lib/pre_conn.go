package lib

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/pkg/errors"
)

const (
	preConnTimeout              = 30000 * time.Millisecond
	idlePreConnPoolSize         = 2
	maxPreConnEpochInterval     = 30 * time.Second
	preConnEpochsDuringLifetime = 20
	defaultMaxPreConnPoolSize   = 5
	defaultPreConnLifetime      = 10 * time.Minute
)

// PreConnTransWrapper wraps a transport to establish connections to the target
// hosts in advance in a bid to reduce overall latency.
type PreConnTransWrapper struct {
	transport       Transport
	preConnMgrs     sync.Map
	maxPoolSize     int
	preConnLifetime time.Duration
}

// WrapAsPreConnTransport wraps a transport into a PreConnTransWrapper.
func WrapAsPreConnTransport(
	transport Transport, config PreConnConfig) (*PreConnTransWrapper, error) {
	w := &PreConnTransWrapper{
		transport: transport,
	}

	if config.MaxPoolSize == 0 {
		w.maxPoolSize = defaultMaxPreConnPoolSize
	} else if config.MaxPoolSize > 0 {
		w.maxPoolSize = config.MaxPoolSize
	} else {
		return nil, errors.New("max_pool_size must be greater than 0")
	}

	if config.Lifetime == "" {
		w.preConnLifetime = defaultPreConnLifetime
	} else if d, err := time.ParseDuration(config.Lifetime); err != nil {
		return nil, errors.Wrap(err, "failed to parse pre_conn lifetime")
	} else if d <= 0 {
		return nil, errors.New("pre_conn lifetime must be > 0")
	} else {
		w.preConnLifetime = d
	}

	epochInterval := w.preConnLifetime / preConnEpochsDuringLifetime
	if epochInterval > maxPreConnEpochInterval {
		epochInterval = maxPreConnEpochInterval
	}
	go func() {
		ticker := time.Tick(epochInterval)
		for _ = range ticker {
			w.preConnMgrs.Range(func(_ interface{}, value interface{}) bool {
				value.(*preConnMgr).Epoch(w.preConnLifetime)
				return true
			})
		}
	}()

	return w, nil
}

// Dial retrieves a connection to the target host if there are any in the
// pre-connect pool, otherwise delegates the call to the wrapped transport.
func (t *PreConnTransWrapper) Dial(
	ctx context.Context, address string) (net.Conn, error) {
	m, found := t.preConnMgrs.Load(address)
	if !found {
		m, _ = t.preConnMgrs.LoadOrStore(
			address, newPreConnMgr(t, address, t.maxPoolSize))
	}
	return m.(*preConnMgr).Dial(ctx)
}

// Listen is not implemented for this transport.
func (t *PreConnTransWrapper) Listen(address string) (net.Listener, error) {
	panic("PreConnTransWrapper is a client-only transport")
}

type preConn struct {
	conn            net.Conn
	establishedTime time.Time
}

type preConnMgr struct {
	wrapper *PreConnTransWrapper
	target  string
	// a ring buffer
	pool      []*preConn
	poolCap   int
	poolBegin int
	poolNext  int
	poolMtx   SpinMutex
	// guarding mutex of runPreConn()
	preConnMtx sync.Mutex
}

func newPreConnMgr(
	wrapper *PreConnTransWrapper, target string, capacity int) *preConnMgr {
	return &preConnMgr{
		wrapper: wrapper,
		target:  target,
		pool:    make([]*preConn, capacity+1),
		poolCap: capacity,
	}
}

func (m *preConnMgr) poolSizeUnsafe() int {
	size := m.poolNext - m.poolBegin
	if size < 0 { // we require poolCap < cap(pool), so 0 always means empty
		size += cap(m.pool)
	}
	return size
}

// runPreConn established preliminary connections to the target host
// in an attempt to increase the pool size to expectedPoolSize.
func (m *preConnMgr) runPreConn(expectedPoolSize int) {
	if expectedPoolSize > m.poolCap {
		panic("expectedPoolSize must be less than or equal to m.poolCap")
	}
	m.preConnMtx.Lock()
	defer m.preConnMtx.Unlock()

	m.poolMtx.Lock()
	poolSize := m.poolSizeUnsafe()
	m.poolMtx.Unlock()
	// guarded by preConnMtx, this is the only goroutine pushing elements
	// into the ring buffer, so we won't accidentally overflow
	for i := poolSize; i < expectedPoolSize; i++ {
		ctx, cancel := context.WithTimeout(
			context.Background(), preConnTimeout)
		conn, err := m.wrapper.transport.Dial(ctx, m.target)
		cancel()
		if err != nil {
			break
		}
		m.poolMtx.Lock()
		m.pool[m.poolNext] = &preConn{
			conn:            conn,
			establishedTime: time.Now(),
		}
		m.poolNext = (m.poolNext + 1) % cap(m.pool)
		m.poolMtx.Unlock()
	}
}

// Epoch cleanups the preConnMgr asynchronously.
// Preliminary connections that last longer than preConnLifetime are dropped,
// and the pool size is increased to at least idlePreConnPoolSize.
func (m *preConnMgr) Epoch(preConnLifetime time.Duration) {
	// pop expired connections
	shouldAfter := time.Now().Add(-preConnLifetime)
	var connsToDrop []net.Conn
	m.poolMtx.Lock()
	for m.poolBegin != m.poolNext {
		if m.pool[m.poolBegin].establishedTime.After(shouldAfter) {
			break // conns in the pool are ordered by establishedTime
		}
		connsToDrop = append(connsToDrop, m.pool[m.poolBegin].conn)
		m.pool[m.poolBegin] = nil
		m.poolBegin = (m.poolBegin + 1) % cap(m.pool)
	}
	poolSize := m.poolSizeUnsafe()
	m.poolMtx.Unlock()
	// close expired connections asynchronously
	if len(connsToDrop) > 0 {
		go func() {
			for _, conn := range connsToDrop {
				_ = conn.Close()
			}
		}()
	}
	// increase pool size if needed
	// note that poolSize might be less than idlePreConnPoolSize
	if poolSize < idlePreConnPoolSize && poolSize < m.poolCap {
		go m.runPreConn(idlePreConnPoolSize)
	}
}

func (m *preConnMgr) Dial(ctx context.Context) (conn net.Conn, err error) {
	m.poolMtx.Lock()
	if m.poolBegin != m.poolNext {
		conn = m.pool[m.poolBegin].conn
		m.pool[m.poolBegin] = nil
		m.poolBegin = (m.poolBegin + 1) % cap(m.pool)
	}
	m.poolMtx.Unlock()
	// starved, trigger a runPreConn and delegate to the underlying transport
	if conn == nil {
		go m.runPreConn(m.poolCap)
		conn, err = m.wrapper.transport.Dial(ctx, m.target)
	}
	return
}
