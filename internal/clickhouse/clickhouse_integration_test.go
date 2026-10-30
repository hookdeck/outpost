package clickhouse_test

import (
	"context"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	stPass int32 = iota
	stHalfOpen
	stEOFOnWrite
)

type session struct {
	client  *net.TCPConn
	backend net.Conn
	state   atomic.Int32
}

// faultProxy forwards to a backend and can turn every open session into a
// connection the backend side has silently dropped.
type faultProxy struct {
	ln       net.Listener
	backend  string
	mu       sync.Mutex
	sessions []*session
}

func newFaultProxy(t *testing.T, backend string) *faultProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	p := &faultProxy{ln: ln, backend: backend}
	t.Cleanup(func() {
		_ = ln.Close()
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, s := range p.sessions {
			_ = s.client.Close()
			_ = s.backend.Close()
		}
	})
	go p.serve()
	return p
}

func (p *faultProxy) addr() string { return p.ln.Addr().String() }

func (p *faultProxy) serve() {
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		b, err := net.Dial("tcp", p.backend)
		if err != nil {
			_ = c.Close()
			continue
		}
		s := &session{client: c.(*net.TCPConn), backend: b}
		p.mu.Lock()
		p.sessions = append(p.sessions, s)
		p.mu.Unlock()
		go func() {
			_, _ = io.Copy(s.client, s.backend)
			if s.state.Load() == stPass {
				_ = s.client.Close()
			}
		}()
		go func() {
			buf := make([]byte, 32<<10)
			for {
				n, err := s.client.Read(buf)
				if n > 0 {
					switch s.state.Load() {
					case stPass:
						if _, werr := s.backend.Write(buf[:n]); werr != nil {
							return
						}
					case stEOFOnWrite:
						_ = s.client.Close()
						return
					}
				}
				if err != nil {
					_ = s.backend.Close()
					return
				}
			}
		}()
	}
}

// fault applies to the sessions open now; later ones pass through.
func (p *faultProxy) fault(state int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.sessions {
		s.state.Store(state)
		_ = s.backend.Close()
	}
	p.sessions = nil
}

func setup(t *testing.T, mutate func(*clickhouse.ClickHouseConfig)) (clickhouse.DB, *faultProxy) {
	t.Helper()
	t.Cleanup(testinfra.Start(t))
	cfg := testinfra.NewClickHouseConfig(t)
	proxy := newFaultProxy(t, cfg.Addr)
	cfg.Addr = proxy.addr()
	mutate(&cfg)
	db, err := clickhouse.New(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db, proxy
}

// warm leaves several idle pooled connections.
func warm(t *testing.T, db clickhouse.DB, n int) {
	t.Helper()
	var wg sync.WaitGroup
	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var v uint8
			assert.NoError(t, db.QueryRow(context.Background(), "SELECT sleep(0.3)").Scan(&v))
		}()
	}
	wg.Wait()
}

func selectOne(ctx context.Context, db clickhouse.DB) error {
	var v uint8
	return db.QueryRow(ctx, "SELECT 1").Scan(&v)
}

func TestIntegration_ConnectionFaults(t *testing.T) {
	testutil.CheckIntegrationTest(t)
	const conns = 5

	t.Run("closed on next write is invisible to reads", func(t *testing.T) {
		db, proxy := setup(t, func(c *clickhouse.ClickHouseConfig) { c.IdleClose = time.Hour })
		warm(t, db, conns)
		proxy.fault(stEOFOnWrite)

		for range 3 * conns {
			require.NoError(t, selectOne(context.Background(), db))
		}
	})

	t.Run("half-open conn fails one read, then the pool is replaced", func(t *testing.T) {
		db, proxy := setup(t, func(c *clickhouse.ClickHouseConfig) {
			c.IdleClose = time.Hour
			c.ReadTimeout = time.Second
		})
		warm(t, db, conns)
		proxy.fault(stHalfOpen)

		start := time.Now()
		require.Error(t, selectOne(context.Background(), db))
		for range 3 * conns {
			require.NoError(t, selectOne(context.Background(), db))
		}
		assert.Less(t, time.Since(start), 5*time.Second)
	})

	t.Run("failed write resets the pool for the next call", func(t *testing.T) {
		db, proxy := setup(t, func(c *clickhouse.ClickHouseConfig) { c.IdleClose = time.Hour })
		warm(t, db, conns)
		proxy.fault(stEOFOnWrite)

		ctx := context.Background()
		require.Error(t, db.Exec(ctx, "SELECT 1"))
		for range 3 * conns {
			require.NoError(t, db.Exec(ctx, "SELECT 1"))
		}
	})

	t.Run("batch on a dropped conn is retried on a fresh pool", func(t *testing.T) {
		db, proxy := setup(t, func(c *clickhouse.ClickHouseConfig) { c.IdleClose = time.Hour })
		ctx := context.Background()
		require.NoError(t, db.Exec(ctx, "CREATE TABLE batch_t (id UInt32) ENGINE = Memory"))
		warm(t, db, conns)
		proxy.fault(stEOFOnWrite)

		insert := func(id uint32) error {
			b, err := db.PrepareBatch(ctx, "INSERT INTO batch_t (id)")
			if err != nil {
				return err
			}
			defer b.Close()
			if err := b.Append(id); err != nil {
				return err
			}
			return b.Send()
		}
		for i := uint32(1); i <= 3*conns; i++ {
			require.NoError(t, insert(i))
		}
		var n uint64
		require.NoError(t, db.QueryRow(ctx, "SELECT count() FROM batch_t").Scan(&n))
		assert.EqualValues(t, 3*conns, n)
	})

	t.Run("idle close drops half-open conns before the next read", func(t *testing.T) {
		db, proxy := setup(t, func(c *clickhouse.ClickHouseConfig) {
			c.IdleClose = 300 * time.Millisecond
			c.ReadTimeout = 30 * time.Second
		})
		warm(t, db, conns)
		proxy.fault(stHalfOpen)
		time.Sleep(time.Second)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		start := time.Now()
		for range 3 * conns {
			require.NoError(t, selectOne(ctx, db))
		}
		assert.Less(t, time.Since(start), 3*time.Second)
	})
}
