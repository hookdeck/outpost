package opevents_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/opevents"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMCPSubscriptionExpiredEvent(t *testing.T) {
	expiresAt := time.Date(2026, 10, 9, 18, 0, 0, 123_000_000, time.FixedZone("x", 3600))
	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithID("sub_1fa69bec3707ee16223ae5400f92f935"),
		testutil.DestinationFactory.WithTenantID("tenant_123"),
		testutil.DestinationFactory.WithType(models.DestinationTypeMCP),
		testutil.DestinationFactory.WithTopics([]string{"order.created"}),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url":             "https://receiver.example.com/mcp-events/abc123",
			"subscription_id": "sub_1fa69bec3707ee16223ae5400f92f935",
			"principal":       "user_8f2c",
			"event":           "order.created",
			"arguments":       `{"currency":"USD"}`,
			"schema_hash":     "8bdecd62",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{"secret": "whsec_c2VjcmV0"}),
		testutil.DestinationFactory.WithExpiresAt(expiresAt),
	)

	ev := opevents.MCPSubscriptionExpiredEvent(opevents.NewMCPSubscriptionExpiredData(&dest))
	assert.Equal(t, opevents.TopicMCPSubscriptionExpired, ev.Topic)
	assert.Equal(t, "mcp.subscription.expired", ev.Topic)
	assert.Equal(t, "tenant_123", ev.TenantID)

	b, err := json.Marshal(ev.Data)
	require.NoError(t, err)
	// The documented payload, exactly: no secret, arguments or schema hash.
	assert.JSONEq(t, `{
		"tenant_id": "tenant_123",
		"subscription_id": "sub_1fa69bec3707ee16223ae5400f92f935",
		"principal": "user_8f2c",
		"topic": "order.created",
		"url": "https://receiver.example.com/mcp-events/abc123",
		"expires_at": "2026-10-09T17:00:00.123Z"
	}`, string(b))

	t.Run("topic falls back to the destination topics", func(t *testing.T) {
		d := dest
		d.Config = map[string]string{}
		data := opevents.NewMCPSubscriptionExpiredData(&d)
		assert.Equal(t, "order.created", data.Topic)
		assert.Empty(t, data.Principal)
	})
}
