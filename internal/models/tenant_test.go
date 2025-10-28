package models_test

import (
	"encoding/json"
	"testing"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
)

func TestTenant_JSONMarshalWithMetadata(t *testing.T) {
	t.Parallel()

	tenant := testutil.TenantFactory.Any(
		testutil.TenantFactory.WithID("tenant_123"),
		testutil.TenantFactory.WithMetadata(map[string]string{
			"environment": "production",
			"team":        "platform",
			"region":      "us-east-1",
		}),
	)

	// Marshal to JSON
	jsonBytes, err := json.Marshal(tenant)
	assert.NoError(t, err)

	// Unmarshal back
	var unmarshaled models.Tenant
	err = json.Unmarshal(jsonBytes, &unmarshaled)
	assert.NoError(t, err)

	// Verify metadata is preserved
	assert.Equal(t, tenant.Metadata, unmarshaled.Metadata)
	assert.Equal(t, "production", unmarshaled.Metadata["environment"])
	assert.Equal(t, "platform", unmarshaled.Metadata["team"])
	assert.Equal(t, "us-east-1", unmarshaled.Metadata["region"])

	// Verify other fields still work
	assert.Equal(t, tenant.ID, unmarshaled.ID)
}

func TestTenant_JSONMarshalWithoutMetadata(t *testing.T) {
	t.Parallel()

	tenant := testutil.TenantFactory.Any(
		testutil.TenantFactory.WithID("tenant_123"),
	)

	// Marshal to JSON
	jsonBytes, err := json.Marshal(tenant)
	assert.NoError(t, err)

	// Unmarshal back
	var unmarshaled models.Tenant
	err = json.Unmarshal(jsonBytes, &unmarshaled)
	assert.NoError(t, err)

	// Verify metadata is nil when not provided
	assert.Nil(t, unmarshaled.Metadata)
}

func TestTenant_JSONUnmarshalEmptyMetadata(t *testing.T) {
	t.Parallel()

	jsonData := `{
		"id": "tenant_123",
		"destinations_count": 0,
		"topics": [],
		"metadata": {},
		"created_at": "2024-01-01T00:00:00Z"
	}`

	var tenant models.Tenant
	err := json.Unmarshal([]byte(jsonData), &tenant)
	assert.NoError(t, err)

	// Empty maps should be preserved as empty, not nil
	assert.NotNil(t, tenant.Metadata)
	assert.Empty(t, tenant.Metadata)
}

func TestTenant_JSONMarshalWithUpdatedAt(t *testing.T) {
	t.Parallel()

	tenant := testutil.TenantFactory.Any(
		testutil.TenantFactory.WithID("tenant_123"),
	)

	// Marshal to JSON
	jsonBytes, err := json.Marshal(tenant)
	assert.NoError(t, err)

	// Unmarshal back
	var unmarshaled models.Tenant
	err = json.Unmarshal(jsonBytes, &unmarshaled)
	assert.NoError(t, err)

	// Verify updated_at is preserved
	assert.Equal(t, tenant.UpdatedAt.Unix(), unmarshaled.UpdatedAt.Unix())
	assert.Equal(t, tenant.CreatedAt.Unix(), unmarshaled.CreatedAt.Unix())
}
