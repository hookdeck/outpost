package destregistry_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newKeysProvider(t *testing.T) *destregistry.BaseProvider {
	t.Helper()
	base, err := destregistry.NewBaseProvider(&mockMetadataLoader{}, "mock")
	require.NoError(t, err)
	return base
}

func TestBaseProvider_DropUndeclaredKeys(t *testing.T) {
	t.Parallel()

	t.Run("keeps declared keys and drops the rest", func(t *testing.T) {
		t.Parallel()
		base := newKeysProvider(t)
		config := models.MapStringString{"public_key": "pk", "secret_key": "sk", "extra": "x"}
		credentials := models.MapStringString{"api_key": "ak", "token": "t", "extra": "x"}
		destination := &models.Destination{Config: config, Credentials: credentials}

		base.DropUndeclaredKeys(destination)

		assert.Equal(t, models.MapStringString{"public_key": "pk", "secret_key": "sk"}, destination.Config)
		assert.Equal(t, models.MapStringString{"api_key": "ak", "token": "t"}, destination.Credentials)
	})

	t.Run("does not modify the maps it was given", func(t *testing.T) {
		t.Parallel()
		base := newKeysProvider(t)
		config := models.MapStringString{"public_key": "pk", "extra": "x"}
		credentials := models.MapStringString{"api_key": "ak", "extra": "x"}
		destination := &models.Destination{Config: config, Credentials: credentials}

		base.DropUndeclaredKeys(destination)

		assert.Equal(t, models.MapStringString{"public_key": "pk", "extra": "x"}, config)
		assert.Equal(t, models.MapStringString{"api_key": "ak", "extra": "x"}, credentials)
	})

	t.Run("allowed keys are kept", func(t *testing.T) {
		t.Parallel()
		base := newKeysProvider(t)
		base.AllowConfigKeys("a", "b")
		base.AllowCredentialKeys("c")
		destination := &models.Destination{
			Config:      models.MapStringString{"a": "1", "b": "2", "c": "3"},
			Credentials: models.MapStringString{"a": "1", "c": "3"},
		}

		base.DropUndeclaredKeys(destination)

		assert.Equal(t, models.MapStringString{"a": "1", "b": "2"}, destination.Config)
		assert.Equal(t, models.MapStringString{"c": "3"}, destination.Credentials)
		assert.Contains(t, base.ConfigKeys(), "a")
		assert.Contains(t, base.CredentialKeys(), "c")
	})

	t.Run("a key omitted from metadata stays known", func(t *testing.T) {
		t.Parallel()
		base := newKeysProvider(t)
		base.OmitConfigField("public_key")
		destination := &models.Destination{Config: models.MapStringString{"public_key": "pk"}}

		base.DropUndeclaredKeys(destination)

		assert.Equal(t, models.MapStringString{"public_key": "pk"}, destination.Config)
	})

	t.Run("nil maps stay nil and other maps are untouched", func(t *testing.T) {
		t.Parallel()
		base := newKeysProvider(t)
		destination := &models.Destination{
			DeliveryMetadata: models.MapStringString{"anything": "1"},
			Metadata:         models.MapStringString{"anything": "2"},
		}

		base.DropUndeclaredKeys(destination)

		assert.Nil(t, destination.Config)
		assert.Nil(t, destination.Credentials)
		assert.Equal(t, models.MapStringString{"anything": "1"}, destination.DeliveryMetadata)
		assert.Equal(t, models.MapStringString{"anything": "2"}, destination.Metadata)
	})
}
