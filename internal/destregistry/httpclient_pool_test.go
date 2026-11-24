package destregistry_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingServer is an httptest server that counts how many TCP connections
// were opened against it. That count is the thing under test: with a correctly
// sized idle pool it should track the concurrency level, not the request count.
type countingServer struct {
	*httptest.Server
	opened atomic.Int64
}

func newCountingServer(t *testing.T, latency time.Duration) *countingServer {
	t.Helper()
	cs := &countingServer{}
	cs.Server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if latency > 0 {
			time.Sleep(latency)
		}
		w.WriteHeader(http.StatusOK)
	}))
	cs.Server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			cs.opened.Add(1)
		}
	}
	cs.Server.Start()
	t.Cleanup(cs.Server.Close)
	return cs
}

// drive sends requestsPerWorker requests from each of `concurrency` workers,
// reading and closing every body so the connection is returned to the idle
// pool before the next request.
func drive(t *testing.T, client *http.Client, url string, concurrency, requestsPerWorker int) {
	t.Helper()
	var wg sync.WaitGroup
	errs := make(chan error, concurrency*requestsPerWorker)
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range requestsPerWorker {
				req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
				if err != nil {
					errs <- err
					return
				}
				resp, err := client.Do(req)
				if err != nil {
					errs <- err
					return
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}

// TestHTTPClientPool_ConnectionsTrackConcurrency is the core verification for
// the shared-client change: connections opened must stay near the concurrency
// level rather than near the request count.
//
// Both destination speeds are covered because the two regimes fail differently
// — against a fast destination the cost of a fresh connection is latency (the
// handshake dwarfs the request), against a slow one it is ephemeral ports
// piling up in TIME_WAIT on the sender.
func TestHTTPClientPool_ConnectionsTrackConcurrency(t *testing.T) {
	t.Parallel()

	const requestsPerWorker = 10

	for _, dest := range []struct {
		name    string
		latency time.Duration
	}{
		{"fast_destination", 0},
		{"slow_destination", 20 * time.Millisecond},
	} {
		for _, concurrency := range []int{1, 4, 16} {
			t.Run(fmt.Sprintf("%s/concurrency_%d", dest.name, concurrency), func(t *testing.T) {
				t.Parallel()

				server := newCountingServer(t, dest.latency)
				client, err := destregistry.NewHTTPClient(destregistry.HTTPClientConfig{
					Pool: destregistry.SizeFanOutPool(concurrency),
				})
				require.NoError(t, err)

				drive(t, client, server.URL, concurrency, requestsPerWorker)

				// Go's transport can start dialing a new connection while an
				// idle one is being returned, so the count sits at or slightly
				// above the concurrency level rather than exactly on it. The
				// claim under test is the order of magnitude: it tracks
				// concurrency, not request count.
				total := concurrency * requestsPerWorker
				opened := int(server.opened.Load())
				assert.LessOrEqual(t, opened, 2*concurrency,
					"connections opened should track concurrency (%d), not the %d requests sent",
					concurrency, total)
				assert.Less(t, opened, total/2,
					"connections opened should be far below the request count")
			})
		}
	}
}

// TestHTTPClientPool_BeatsStockDefaults pins the behavior the change exists to
// fix, so the test above can't pass trivially: with Go's stock per-host limit
// of two, reuse collapses as soon as more than two deliveries to a destination
// overlap. Same workload, two identical servers, only the pool differs.
func TestHTTPClientPool_BeatsStockDefaults(t *testing.T) {
	t.Parallel()

	const concurrency = 32
	const requestsPerWorker = 10

	sizedServer := newCountingServer(t, 0)
	sized, err := destregistry.NewHTTPClient(destregistry.HTTPClientConfig{
		Pool: destregistry.SizeFanOutPool(concurrency),
	})
	require.NoError(t, err)
	drive(t, sized, sizedServer.URL, concurrency, requestsPerWorker)

	stockServer := newCountingServer(t, 0)
	stock, err := destregistry.NewHTTPClient(destregistry.HTTPClientConfig{})
	require.NoError(t, err)
	drive(t, stock, stockServer.URL, concurrency, requestsPerWorker)

	// Only a strict inequality is asserted — the exact ratio varies with
	// scheduling, and pinning a multiplier makes the test flaky.
	assert.Greater(t, stockServer.opened.Load(), sizedServer.opened.Load(),
		"stock defaults should open more connections than a pool sized for the concurrency level")
}

func TestHTTPClientPool_OnConnectionReportsReuse(t *testing.T) {
	t.Parallel()

	const concurrency = 4
	const requestsPerWorker = 10

	server := newCountingServer(t, 0)

	var fresh, reused atomic.Int64
	client, err := destregistry.NewHTTPClient(destregistry.HTTPClientConfig{
		Pool: destregistry.SizeFanOutPool(concurrency),
		OnConnection: func(wasReused bool) {
			if wasReused {
				reused.Add(1)
			} else {
				fresh.Add(1)
			}
		},
	})
	require.NoError(t, err)

	drive(t, client, server.URL, concurrency, requestsPerWorker)

	assert.Equal(t, int64(concurrency*requestsPerWorker), fresh.Load()+reused.Load(),
		"every request should report exactly one connection acquisition")
	// Every non-reused acquisition needed a connection the server saw opened.
	// The reverse doesn't hold: a speculative dial that loses the race to a
	// returning idle connection is opened and then discarded without ever
	// serving a request.
	assert.LessOrEqual(t, fresh.Load(), server.opened.Load())
	assert.Positive(t, reused.Load())
}

// TestHTTPClientPool_MultipleHostsKeepWarmConnections covers breadth: fan-out
// across many destinations, each with almost no concurrency, is the normal
// shape for this product. Go's 100-connection total would have destinations
// evicting each other.
func TestHTTPClientPool_MultipleHostsKeepWarmConnections(t *testing.T) {
	t.Parallel()

	const hosts = 20
	const rounds = 5

	servers := make([]*countingServer, hosts)
	for i := range servers {
		servers[i] = newCountingServer(t, 0)
	}

	client, err := destregistry.NewHTTPClient(destregistry.HTTPClientConfig{
		Pool: destregistry.SizeFanOutPool(1),
	})
	require.NoError(t, err)

	for range rounds {
		for _, server := range servers {
			drive(t, client, server.URL, 1, 1)
		}
	}

	for i, server := range servers {
		assert.Equal(t, int64(1), server.opened.Load(),
			"host %d should have kept its connection warm across all %d rounds", i, rounds)
	}
}
