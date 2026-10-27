package e2e_test

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/stretchr/testify/require"
)

const (
	mcpDeliveryTimeoutSeconds = 2
	// mcpAutoDisableCount is below the 6 events the non-retryable tests
	// send, and above the attempts of any fully retried message.
	mcpAutoDisableCount = "5"
)

// mcpRetrySchedule is MCP_RETRY_SCHEDULE: two retries, unlike the global
// retry config, which the webhook comparison keeps at its default.
var mcpRetrySchedule = []int{1, 1}

// withMCPDelivery enables MCP with the settings the delivery suite needs.
func withMCPDelivery(cfg *config.Config) {
	cfg.MCP.Enabled = true
	cfg.Destinations.PrivateAddressAllowlist = []string{"127.0.0.2/32"}
	cfg.MCP.RetrySchedule = mcpRetrySchedule
	cfg.DeliveryTimeoutSeconds = mcpDeliveryTimeoutSeconds
	cfg.RetryPollBackoffMs = 50
	cfg.Alert.ConsecutiveFailureCount = config.NewOptionalString(mcpAutoDisableCount)
}

// withMCPDisabled turns MCP off explicitly.
func withMCPDisabled(cfg *config.Config) {
	cfg.MCP.Enabled = false
}

// writeDestinationToStore saves a destination straight into the tenant
// store the running Outpost reads, bypassing API validation.
func writeDestinationToStore(t *testing.T, cfg config.Config, d models.Destination) {
	t.Helper()
	client, err := redis.New(context.Background(), cfg.Redis.ToConfig())
	require.NoError(t, err)
	store := tenantstore.New(tenantstore.Config{
		RedisClient:              client,
		Secret:                   cfg.AESEncryptionSecret,
		AvailableTopics:          cfg.Topics,
		MaxDestinationsPerTenant: cfg.MaxDestinationsPerTenant,
		DeploymentID:             cfg.DeploymentID,
	})
	now := time.Now()
	d.CreatedAt, d.UpdatedAt = now, now
	require.NoError(t, store.CreateDestination(context.Background(), d))
}
