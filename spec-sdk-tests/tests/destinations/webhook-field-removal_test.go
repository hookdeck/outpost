package destinations_test

import (
	"context"
	"os"
	"testing"

	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

const testTenantID = "go-sdk-test-tenant"

func setupTenant(t *testing.T, client *outpostgo.Outpost) {
	t.Helper()
	ctx := context.Background()
	_, err := client.Tenants.Upsert(ctx, testTenantID, nil)
	require.NoError(t, err, "failed to upsert tenant")
}

func cleanupTenant(t *testing.T, client *outpostgo.Outpost) {
	t.Helper()
	ctx := context.Background()
	_, _ = client.Tenants.Delete(ctx, testTenantID)
}

func createWebhookDest(t *testing.T, client *outpostgo.Outpost, filter map[string]any, metadata, deliveryMetadata map[string]string) string {
	t.Helper()
	ctx := context.Background()
	topics := components.CreateTopicsArrayOfStr([]string{"user.created", "user.updated"})
	resp, err := client.Destinations.Create(ctx, testTenantID, components.CreateDestinationCreateWebhook(components.DestinationCreateWebhook{
		Topics: topics,
		Config: components.WebhookConfig{
			URL: "https://example.com/webhook",
		},
		Filter:           filter,
		Metadata:         metadata,
		DeliveryMetadata: deliveryMetadata,
	}))
	require.NoError(t, err, "failed to create destination")
	wh := resp.GetDestinationWebhook()
	require.NotNil(t, wh, "expected webhook destination in response")
	return wh.ID
}

func deleteDest(t *testing.T, client *outpostgo.Outpost, destID string) {
	t.Helper()
	ctx := context.Background()
	_, _ = client.Destinations.Delete(ctx, testTenantID, destID)
}

func getDest(t *testing.T, client *outpostgo.Outpost, destID string) *components.DestinationWebhook {
	t.Helper()
	ctx := context.Background()
	resp, err := client.Destinations.Get(ctx, testTenantID, destID)
	require.NoError(t, err, "failed to get destination")
	wh := resp.Destination.DestinationWebhook
	require.NotNil(t, wh, "expected webhook destination")
	return wh
}

func updateDest(t *testing.T, client *outpostgo.Outpost, destID string, update components.DestinationUpdateWebhook) *components.DestinationWebhook {
	t.Helper()
	ctx := context.Background()
	resp, err := client.Destinations.Update(ctx, testTenantID, destID,
		components.CreateDestinationUpdateDestinationUpdateWebhook(update))
	require.NoError(t, err, "failed to update destination")
	wh := resp.OneOf.Destination.DestinationWebhook
	require.NotNil(t, wh, "expected webhook destination in update response")
	return wh
}

func TestFilterRemoval(t *testing.T) {
	client := getClient(t)
	setupTenant(t, client)
	defer cleanupTenant(t, client)

	filter := map[string]any{"body": map[string]any{"user_id": "usr_123"}}
	destID := createWebhookDest(t, client, filter, nil, nil)
	defer deleteDest(t, client, destID)

	t.Run("should have filter set after creation", func(t *testing.T) {
		dest := getDest(t, client, destID)
		assert.NotNil(t, dest.Filter)
	})

	t.Run("should clear filter when set to empty map", func(t *testing.T) {
		dest := updateDest(t, client, destID, components.DestinationUpdateWebhook{
			Filter: map[string]any{},
		})
		assert.True(t, dest.Filter == nil || len(dest.Filter) == 0,
			"filter should be nil or empty, got: %v", dest.Filter)
	})
}

func TestMetadataRemoval(t *testing.T) {
	client := getClient(t)
	setupTenant(t, client)
	defer cleanupTenant(t, client)

	metadata := map[string]string{
		"env":    "production",
		"team":   "platform",
		"region": "us-east-1",
	}
	destID := createWebhookDest(t, client, nil, metadata, nil)
	defer deleteDest(t, client, destID)

	t.Run("should have metadata set after creation", func(t *testing.T) {
		dest := getDest(t, client, destID)
		assert.Equal(t, metadata, dest.Metadata)
	})

	t.Run("should remove a single metadata key when sending subset", func(t *testing.T) {
		dest := updateDest(t, client, destID, components.DestinationUpdateWebhook{
			Metadata: map[string]string{
				"env":  "production",
				"team": "platform",
			},
		})
		assert.Equal(t, map[string]string{
			"env":  "production",
			"team": "platform",
		}, dest.Metadata)
		_, hasRegion := dest.Metadata["region"]
		assert.False(t, hasRegion, "region key should have been removed")
	})

	t.Run("should clear all metadata when set to empty map", func(t *testing.T) {
		dest := updateDest(t, client, destID, components.DestinationUpdateWebhook{
			Metadata: map[string]string{},
		})
		assert.True(t, dest.Metadata == nil || len(dest.Metadata) == 0,
			"metadata should be nil or empty, got: %v", dest.Metadata)
	})
}

func TestDeliveryMetadataRemoval(t *testing.T) {
	client := getClient(t)
	setupTenant(t, client)
	defer cleanupTenant(t, client)

	dm := map[string]string{
		"source":  "outpost",
		"version": "1.0",
	}
	destID := createWebhookDest(t, client, nil, nil, dm)
	defer deleteDest(t, client, destID)

	t.Run("should have delivery_metadata set after creation", func(t *testing.T) {
		dest := getDest(t, client, destID)
		assert.Equal(t, dm, dest.DeliveryMetadata)
	})

	t.Run("should remove a single delivery_metadata key when sending subset", func(t *testing.T) {
		dest := updateDest(t, client, destID, components.DestinationUpdateWebhook{
			DeliveryMetadata: map[string]string{
				"source": "outpost",
			},
		})
		assert.Equal(t, map[string]string{
			"source": "outpost",
		}, dest.DeliveryMetadata)
		_, hasVersion := dest.DeliveryMetadata["version"]
		assert.False(t, hasVersion, "version key should have been removed")
	})

	t.Run("should clear all delivery_metadata when set to empty map", func(t *testing.T) {
		dest := updateDest(t, client, destID, components.DestinationUpdateWebhook{
			DeliveryMetadata: map[string]string{},
		})
		assert.True(t, dest.DeliveryMetadata == nil || len(dest.DeliveryMetadata) == 0,
			"delivery_metadata should be nil or empty, got: %v", dest.DeliveryMetadata)
	})
}
