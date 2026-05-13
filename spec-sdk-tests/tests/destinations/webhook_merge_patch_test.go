package destinations_test

import (
	"context"
	"os"
	"testing"

	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	"github.com/hookdeck/outpost/sdks/outpost-go/optionalnullable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ptr[T any](v T) *T { return &v }

func getClient(t *testing.T) *outpostgo.Outpost {
	t.Helper()
	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		apiKey = "apikey"
	}
	baseURL := os.Getenv("API_BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:3333/api/v1"
	}
	return outpostgo.New(
		outpostgo.WithSecurity(apiKey),
		outpostgo.WithServerURL(baseURL),
	)
}

const testTenantID = "go-merge-patch-test"

func setupTenant(t *testing.T, client *outpostgo.Outpost) {
	t.Helper()
	_, err := client.Tenants.Upsert(context.Background(), testTenantID, nil)
	require.NoError(t, err)
}

func cleanupTenant(t *testing.T, client *outpostgo.Outpost) {
	t.Helper()
	_, _ = client.Tenants.Delete(context.Background(), testTenantID)
}

func webhookCreate(metadata map[string]string) components.DestinationCreateWebhook {
	create := components.DestinationCreateWebhook{
		Type:   components.DestinationCreateWebhookTypeWebhook,
		Topics: components.CreateTopicsArrayOfStr([]string{"*"}),
		Config: components.WebhookConfig{URL: "https://example.com/webhook"},
	}
	if metadata != nil {
		create.Metadata = optionalnullable.From(&metadata)
	}
	return create
}

func createDest(t *testing.T, client *outpostgo.Outpost, create components.DestinationCreateWebhook) string {
	t.Helper()
	resp, err := client.Destinations.Create(context.Background(), testTenantID,
		components.CreateDestinationCreateWebhook(create))
	require.NoError(t, err)
	wh := resp.GetDestinationWebhook()
	require.NotNil(t, wh)
	return wh.ID
}

func getDest(t *testing.T, client *outpostgo.Outpost, id string) *components.DestinationWebhook {
	t.Helper()
	resp, err := client.Destinations.Get(context.Background(), testTenantID, id)
	require.NoError(t, err)
	wh := resp.Destination.DestinationWebhook
	require.NotNil(t, wh)
	return wh
}

func updateDest(t *testing.T, client *outpostgo.Outpost, id string, update components.DestinationUpdateWebhook) {
	t.Helper()
	_, err := client.Destinations.Update(context.Background(), testTenantID, id,
		components.CreateDestinationUpdateDestinationUpdateWebhook(update))
	require.NoError(t, err)
}

func deleteDest(t *testing.T, client *outpostgo.Outpost, id string) {
	t.Helper()
	_, _ = client.Destinations.Delete(context.Background(), testTenantID, id)
}

// ── metadata merge-patch ──

func TestMetadataMergePatch(t *testing.T) {
	client := getClient(t)
	setupTenant(t, client)
	defer cleanupTenant(t, client)

	t.Run("add key preserving existing", func(t *testing.T) {
		id := createDest(t, client, webhookCreate(map[string]string{"env": "prod"}))
		defer deleteDest(t, client, id)

		updateDest(t, client, id, components.DestinationUpdateWebhook{
			Metadata: optionalnullable.From(&map[string]*string{
				"team": ptr("platform"),
			}),
		})

		got := getDest(t, client, id)
		m, ok := got.Metadata.GetOrZero()
		require.True(t, ok)
		assert.Equal(t, "prod", m["env"])
		assert.Equal(t, "platform", m["team"])
	})

	t.Run("delete key via null value", func(t *testing.T) {
		id := createDest(t, client, webhookCreate(map[string]string{"env": "prod", "region": "us-east-1"}))
		defer deleteDest(t, client, id)

		updateDest(t, client, id, components.DestinationUpdateWebhook{
			Metadata: optionalnullable.From(&map[string]*string{
				"region": nil, // null = delete key
			}),
		})

		got := getDest(t, client, id)
		m, ok := got.Metadata.GetOrZero()
		require.True(t, ok)
		assert.Equal(t, "prod", m["env"])
		_, hasRegion := m["region"]
		assert.False(t, hasRegion, "region should be deleted")
	})

	t.Run("clear entire field via null", func(t *testing.T) {
		id := createDest(t, client, webhookCreate(map[string]string{"env": "prod"}))
		defer deleteDest(t, client, id)

		updateDest(t, client, id, components.DestinationUpdateWebhook{
			Metadata: optionalnullable.From[map[string]*string](nil), // null = clear all
		})

		got := getDest(t, client, id)
		m, _ := got.Metadata.GetOrZero()
		assert.True(t, got.Metadata.IsNull() || len(m) == 0,
			"metadata should be cleared")
	})

	t.Run("omitted field is no-op", func(t *testing.T) {
		id := createDest(t, client, webhookCreate(map[string]string{"env": "prod"}))
		defer deleteDest(t, client, id)

		// Update with no metadata field set
		updateDest(t, client, id, components.DestinationUpdateWebhook{})

		got := getDest(t, client, id)
		m, ok := got.Metadata.GetOrZero()
		require.True(t, ok)
		assert.Equal(t, "prod", m["env"])
	})

	t.Run("mixed add update delete", func(t *testing.T) {
		id := createDest(t, client, webhookCreate(map[string]string{
			"keep": "v", "remove": "v", "update": "old",
		}))
		defer deleteDest(t, client, id)

		updateDest(t, client, id, components.DestinationUpdateWebhook{
			Metadata: optionalnullable.From(&map[string]*string{
				"remove": nil,
				"update": ptr("new"),
				"add":    ptr("v"),
			}),
		})

		got := getDest(t, client, id)
		m, ok := got.Metadata.GetOrZero()
		require.True(t, ok)
		assert.Equal(t, "v", m["keep"])
		assert.Equal(t, "new", m["update"])
		assert.Equal(t, "v", m["add"])
		_, hasRemove := m["remove"]
		assert.False(t, hasRemove, "remove key should be deleted")
	})
}

// ── filter replacement ──

func TestFilterReplacement(t *testing.T) {
	client := getClient(t)
	setupTenant(t, client)
	defer cleanupTenant(t, client)

	t.Run("clear filter with null", func(t *testing.T) {
		create := webhookCreate(nil)
		create.Filter = optionalnullable.From(&map[string]any{
			"body": map[string]any{"user_id": "usr_123"},
		})
		id := createDest(t, client, create)
		defer deleteDest(t, client, id)

		updateDest(t, client, id, components.DestinationUpdateWebhook{
			Filter: optionalnullable.From[map[string]any](nil),
		})

		got := getDest(t, client, id)
		f, _ := got.Filter.GetOrZero()
		assert.True(t, got.Filter.IsNull() || len(f) == 0,
			"filter should be cleared")
	})

	t.Run("clear filter with empty map", func(t *testing.T) {
		create := webhookCreate(nil)
		create.Filter = optionalnullable.From(&map[string]any{
			"body": map[string]any{"user_id": "usr_123"},
		})
		id := createDest(t, client, create)
		defer deleteDest(t, client, id)

		emptyFilter := map[string]any{}
		updateDest(t, client, id, components.DestinationUpdateWebhook{
			Filter: optionalnullable.From(&emptyFilter),
		})

		got := getDest(t, client, id)
		f, _ := got.Filter.GetOrZero()
		assert.True(t, got.Filter.IsNull() || len(f) == 0,
			"filter should be cleared")
	})
}
