package destregistry_test

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
)

func TestDestinationValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	registry := destregistry.NewRegistry()
	destregistrydefault.RegisterDefault(registry)

	t.Run("validates valid webhook", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com",
			}),
		)

		provider, err := registry.GetProvider(destination.Type)
		assert.NoError(t, err)
		assert.NoError(t, provider.Validate(ctx, &destination))
	})

	t.Run("validates invalid type", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("invalid"),
			testutil.DestinationFactory.WithConfig(map[string]string{}),
		)

		_, err := registry.GetProvider(destination.Type)
		assert.ErrorContains(t, err, "unsupported destination type: invalid")
	})

	t.Run("validates invalid rabbitmq config", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("rabbitmq"),
			testutil.DestinationFactory.WithConfig(map[string]string{}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"username": "guest",
				"password": "guest",
			}),
		)

		provider, err := registry.GetProvider(destination.Type)
		assert.NoError(t, err)
		assert.ErrorContains(t, provider.Validate(ctx, &destination),
			"server_url is required for rabbitmq destination config")
	})

	t.Run("validates invalid rabbitmq credentials", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("rabbitmq"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"server_url": "localhost:5672",
				"exchange":   "events",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"username":    "guest",
				"notpassword": "guest",
			}),
		)

		provider, err := registry.GetProvider(destination.Type)
		assert.NoError(t, err)
		assert.ErrorContains(t, provider.Validate(ctx, &destination),
			"password is required for rabbitmq destination credentials")
	})
}
