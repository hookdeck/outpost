package destwebhook_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWebhookProvider_PublishersShareOneConnectionPool is the provider-level
// half of the pooling change. Before, each destination got its own client with
// its own two-connection idle pool, so concurrent deliveries to one destination
// opened roughly one connection each. Now the client — and therefore the pool —
// is built once per provider.
func TestWebhookProvider_PublishersShareOneConnectionPool(t *testing.T) {
	t.Parallel()

	const destinations = 8
	const requestsPerDestination = 5
	const concurrency = 4

	var opened, received atomic.Int64
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			opened.Add(1)
		}
	}
	server.Start()
	defer server.Close()

	provider := NewTestProvider(t,
		destwebhook.WithConnectionPool(destregistry.SizeFanOutPool(concurrency)),
	)

	ctx := context.Background()

	// Every destination points at the same host — the pool is per-host, so
	// these all draw from the same set of connections once the client is
	// shared.
	publishers := make([]destregistry.Publisher, destinations)
	for i := range publishers {
		dest := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{"url": server.URL}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": fmt.Sprintf("whsec_test_%d", i),
			}),
		)
		publisher, err := provider.CreatePublisher(ctx, &dest)
		require.NoError(t, err)
		publishers[i] = publisher
		t.Cleanup(func() { publisher.Close() })
	}

	// Drive `concurrency` workers over the publishers round-robin.
	var wg sync.WaitGroup
	errs := make(chan error, destinations*requestsPerDestination)
	work := make(chan destregistry.Publisher)
	for range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for publisher := range work {
				event := testutil.EventFactory.Any()
				if _, err := publisher.Publish(ctx, &event); err != nil {
					errs <- err
				}
			}
		}()
	}
	for range requestsPerDestination {
		for _, publisher := range publishers {
			work <- publisher
		}
	}
	close(work)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	require.Equal(t, int64(destinations*requestsPerDestination), received.Load(),
		"every publish should have reached the server")
	// Slack of one concurrency level: Go's transport can start dialing while
	// an idle connection is being returned.
	assert.LessOrEqual(t, int(opened.Load()), 2*concurrency,
		"publishers should share one pool: %d requests across %d destinations should track concurrency (%d), not destination count",
		destinations*requestsPerDestination, destinations, concurrency)
}
