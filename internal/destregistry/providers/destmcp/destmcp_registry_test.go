package destmcp_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destmcp"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/netguard"
	"github.com/hookdeck/outpost/internal/util/testutil"
	standardwebhooks "github.com/standard-webhooks/standard-webhooks/libraries/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newRegistryWith(t *testing.T, regCfg destregistry.Config, cfg destmcp.Config) destregistry.Registry {
	t.Helper()
	registry := destregistry.NewRegistry(&regCfg, testutil.CreateTestLogger(t))
	p, err := destmcp.New(registry.MetadataLoader(), cfg)
	require.NoError(t, err)
	require.NoError(t, registry.RegisterProvider(destmcp.Type, p))
	return registry
}

func newRegistry(t *testing.T, cfg destmcp.Config) destregistry.Registry {
	t.Helper()
	return newRegistryWith(t, destregistry.Config{}, cfg)
}

func mcpProvider(t *testing.T, registry destregistry.Registry) *destmcp.Provider {
	t.Helper()
	provider, err := registry.ResolveProvider(&models.Destination{Type: destmcp.Type})
	require.NoError(t, err)
	p, ok := provider.(*destmcp.Provider)
	require.True(t, ok)
	return p
}

func localConfig(t *testing.T) destmcp.Config {
	t.Helper()
	guard := loopbackGuard(t)
	return destmcp.Config{
		Catalog:   newCatalog(t),
		Guard:     guard,
		Client:    newClient(t, netguard.ClientConfig{Guard: guard}),
		Verifier:  &fakeVerifier{},
		UserAgent: testUserAgent,
	}
}

// Subscribe-time validation and preprocessing, then delivery, all through the
// registry as the API and the delivery service use it.
func TestRegistry_SubscribeAndPublish(t *testing.T) {
	t.Parallel()
	rec := newRecorder(t, nil)
	registry := newRegistry(t, localConfig(t))
	p := mcpProvider(t, registry)

	d := localSubscription(t, p, rec.URL+"/hook")
	require.NoError(t, registry.ValidateDestination(context.Background(), d))
	require.NoError(t, registry.PreprocessDestination(d, nil, &destregistry.PreprocessDestinationOpts{Role: "admin"}))

	event := testEvent()
	attempt, err := registry.PublishEvent(context.Background(), d, event)
	require.NoError(t, err)
	require.NotNil(t, attempt)
	assert.Equal(t, models.AttemptStatusSuccess, attempt.Status)
	assert.Equal(t, "200", attempt.Code)
	assert.Equal(t, destmcp.Type, attempt.DestinationType)
	assert.Equal(t, d.ID, attempt.DestinationID)
	assert.Equal(t, event.ID, attempt.EventID)

	reqs := rec.Requests()
	require.Len(t, reqs, 1)
	wh, err := standardwebhooks.NewWebhook(testSecret(1))
	require.NoError(t, err)
	assert.NoError(t, wh.Verify(reqs[0].Body, reqs[0].Header))
	assert.Equal(t, d.ID, reqs[0].Header.Get("X-MCP-Subscription-Id"))

	// A refresh with a new secret is dual-signed through the grace period.
	refresh := localSubscription(t, p, rec.URL+"/hook", func(d *models.Destination) {
		d.Credentials[destmcp.CredentialSecret] = testSecret(7)
	})
	require.NoError(t, registry.ValidateDestination(context.Background(), refresh))
	require.NoError(t, registry.PreprocessDestination(refresh, d, &destregistry.PreprocessDestinationOpts{Role: "admin"}))
	_, err = registry.PublishEvent(context.Background(), refresh, event)
	require.NoError(t, err)
	reqs = rec.Requests()
	require.Len(t, reqs, 2)
	assert.Len(t, strings.Fields(reqs[1].Header.Get("webhook-signature")), 2)
	for _, secret := range []string{testSecret(7), testSecret(1)} {
		wh, err := standardwebhooks.NewWebhook(secret)
		require.NoError(t, err)
		assert.NoError(t, wh.Verify(reqs[1].Body, reqs[1].Header))
	}
}

func TestRegistry_FailedAttempts(t *testing.T) {
	t.Parallel()

	t.Run("non-retryable status", func(t *testing.T) {
		t.Parallel()
		rec := newRecorder(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusGone) })
		registry := newRegistry(t, localConfig(t))
		d := localSubscription(t, mcpProvider(t, registry), rec.URL+"/hook")
		attempt, err := registry.PublishEvent(context.Background(), d, testEvent())
		require.NotNil(t, attempt)
		assert.Equal(t, models.AttemptStatusFailed, attempt.Status)
		assert.Equal(t, "410", attempt.Code)
		assert.True(t, destregistry.IsNonRetryable(err))
	})

	t.Run("payload too large", func(t *testing.T) {
		t.Parallel()
		rec := newRecorder(t, nil)
		registry := newRegistry(t, localConfig(t))
		d := localSubscription(t, mcpProvider(t, registry), rec.URL+"/hook")
		event := testEvent()
		event.Data = []byte(`"` + strings.Repeat("x", 300<<10) + `"`)
		attempt, err := registry.PublishEvent(context.Background(), d, event)
		require.NotNil(t, attempt, "the failure is visible as an attempt")
		assert.Equal(t, destmcp.CodePayloadTooLarge, attempt.Code)
		assert.True(t, destregistry.IsNonRetryable(err))
		assert.Empty(t, rec.Requests())
	})

	t.Run("undeliverable destination", func(t *testing.T) {
		t.Parallel()
		registry := newRegistry(t, localConfig(t))
		d := localSubscription(t, mcpProvider(t, registry), "http://127.0.0.1:1/hook", func(d *models.Destination) {
			d.Credentials[destmcp.CredentialSecret] = "corrupt"
		})
		attempt, err := registry.PublishEvent(context.Background(), d, testEvent())
		require.NotNil(t, attempt)
		assert.Equal(t, models.AttemptStatusFailed, attempt.Status)
		assert.True(t, destregistry.IsNonRetryable(err))
		assert.NotContains(t, attempt.ResponseData["error"], "corrupt")
	})
}

// The registry bounds MCP attempts with the provider's timeout, not
// DELIVERY_TIMEOUT_SECONDS.
func TestRegistry_DeliveryTimeout(t *testing.T) {
	t.Parallel()

	t.Run("longer than the global timeout", func(t *testing.T) {
		t.Parallel()
		rec := newRecorder(t, func(w http.ResponseWriter, r *http.Request) { time.Sleep(300 * time.Millisecond) })
		registry := newRegistryWith(t, destregistry.Config{DeliveryTimeout: 50 * time.Millisecond}, localConfig(t))
		p := mcpProvider(t, registry)
		assert.Equal(t, 10*time.Second, p.DeliveryTimeout())

		attempt, err := registry.PublishEvent(context.Background(), localSubscription(t, p, rec.URL+"/hook"), testEvent())
		require.NoError(t, err)
		assert.Equal(t, "200", attempt.Code)
	})

	t.Run("enforced", func(t *testing.T) {
		t.Parallel()
		release := make(chan struct{})
		rec := newRecorder(t, func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-release:
			}
		})
		t.Cleanup(func() { close(release) })
		registry := newRegistryWith(t, destregistry.Config{DeliveryTimeout: time.Minute}, localConfig(t))
		p := mcpProvider(t, registry)
		destmcp.SetDeliveryTimeout(p, 100*time.Millisecond)

		start := time.Now()
		attempt, err := registry.PublishEvent(context.Background(), localSubscription(t, p, rec.URL+"/hook"), testEvent())
		assert.Less(t, time.Since(start), 5*time.Second)
		require.NotNil(t, attempt)
		assert.Equal(t, models.AttemptStatusFailed, attempt.Status)
		assert.Equal(t, "timeout", attempt.Code)
		var pubErr *destregistry.ErrDestinationPublishAttempt
		require.ErrorAs(t, err, &pubErr)
		assert.Equal(t, "timeout", pubErr.Data["error"])
		assert.Equal(t, "100ms", pubErr.Data["timeout"])
		assert.False(t, pubErr.NonRetryable)
	})
}

// Every attempt gets its own publisher; nothing lands in the shared LRU.
func TestRegistry_BypassesPublisherCache(t *testing.T) {
	t.Parallel()
	registry := newRegistry(t, localConfig(t))
	p := mcpProvider(t, registry)
	assert.True(t, p.BypassPublisherCache())
	d := localSubscription(t, p, "http://127.0.0.1:1/hook")
	first, err := registry.ResolvePublisher(context.Background(), d)
	require.NoError(t, err)
	second, err := registry.ResolvePublisher(context.Background(), d)
	require.NoError(t, err)
	assert.NotSame(t, first, second)
	assert.NoError(t, first.Close())
	assert.NoError(t, second.Close())
}
