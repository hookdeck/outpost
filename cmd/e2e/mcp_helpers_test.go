package e2e_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

const (
	mcpDeliveryTimeoutSeconds = 2
	// mcpAutoDisableCount is below the 6 events the non-retryable tests
	// send, and above the attempts of any fully retried message.
	mcpAutoDisableCount = "5"
)

// mcpRetrySchedule replaces the fixed one-to-five-minute MCP schedule with two
// quick retries.
var mcpRetrySchedule = []time.Duration{time.Second, time.Second}

// mcpDeliveryEvents defines the one event the delivery suite subscribes to.
const mcpDeliveryEvents = `
events:
  - name: user.created
    inputSchema: {type: object, properties: {}}
    payloadSchema: {type: object}
`

// withMCPDelivery enables MCP with the settings the delivery suite needs.
func withMCPDelivery(cfg *config.Config) {
	cfg.Experimental.MCP.Enabled = true
	cfg.Destinations.PrivateAddressAllowlist = []string{"127.0.0.2/32"}
	cfg.Experimental.MCP.AllowInsecureCallbacks = true
	if err := yaml.Unmarshal([]byte(mcpDeliveryEvents), &cfg.Experimental.MCP.EventsSchema); err != nil {
		panic(err)
	}
	cfg.Experimental.MCP.RetryDelays = mcpRetrySchedule
	cfg.Experimental.MCP.SubscriptionTTLMinSeconds = 1
	cfg.DeliveryTimeoutSeconds = mcpDeliveryTimeoutSeconds
	cfg.RetryPollBackoffMs = 50
	cfg.Alert.ConsecutiveFailureCount = config.NewOptionalString(mcpAutoDisableCount)
}

// withMCPDisabled turns MCP off explicitly.
func withMCPDisabled(cfg *config.Config) {
	cfg.Experimental.MCP.Enabled = false
}

// newTestTenantStore opens the tenant store the running Outpost reads.
func newTestTenantStore(t *testing.T, cfg config.Config) tenantstore.TenantStore {
	t.Helper()
	client, err := redis.New(context.Background(), cfg.Redis.ToConfig())
	require.NoError(t, err)
	return tenantstore.New(tenantstore.Config{
		RedisClient:              client,
		Secret:                   cfg.AESEncryptionSecret,
		AvailableTopics:          cfg.Topics,
		MaxDestinationsPerTenant: cfg.MaxDestinationsPerTenant,
		DeploymentID:             cfg.DeploymentID,
	})
}

// writeDestinationToStore saves a destination straight into the tenant
// store the running Outpost reads, bypassing API validation.
func writeDestinationToStore(t *testing.T, cfg config.Config, d models.Destination) {
	t.Helper()
	now := time.Now()
	d.CreatedAt, d.UpdatedAt = now, now
	require.NoError(t, newTestTenantStore(t, cfg).CreateDestination(context.Background(), d))
}

// readDestinationFromStore reads a destination from the tenant store, which
// the destinations API does not return for a subscription. It returns nil
// when the destination does not exist or was deleted.
func readDestinationFromStore(t *testing.T, cfg config.Config, tenantID, destID string) *models.Destination {
	t.Helper()
	d, err := newTestTenantStore(t, cfg).RetrieveDestination(context.Background(), tenantID, destID)
	if !errors.Is(err, tenantstore.ErrDestinationDeleted) {
		require.NoError(t, err)
	}
	return d
}

// disableDestinationInStore suspends a destination straight in the tenant
// store, as auto-disable does.
func disableDestinationInStore(t *testing.T, cfg config.Config, tenantID, destID string) {
	t.Helper()
	store := newTestTenantStore(t, cfg)
	d, err := store.RetrieveDestination(context.Background(), tenantID, destID)
	require.NoError(t, err)
	require.NotNil(t, d)
	now := time.Now()
	d.DisabledAt = &now
	require.NoError(t, store.UpdateDestination(context.Background(), *d))
}
