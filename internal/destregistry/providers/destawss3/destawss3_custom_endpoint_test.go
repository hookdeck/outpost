package destawss3_test

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawss3"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWSS3_CustomEndpoint(t *testing.T) {
	t.Parallel()

	baseConfig := map[string]string{"bucket": "b", "region": "us-east-1"}
	build := func(endpoint string) *models.Destination {
		config := map[string]string{}
		for k, v := range baseConfig {
			config[k] = v
		}
		if endpoint != "" {
			config["endpoint"] = endpoint
		}
		d := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("aws_s3"),
			testutil.DestinationFactory.WithConfig(config),
			testutil.DestinationFactory.WithCredentials(map[string]string{"key": "k", "secret": "s"}),
		)
		return &d
	}

	t.Run("not allowed rejects endpoint", func(t *testing.T) {
		t.Parallel()
		provider, err := destawss3.New(testutil.Registry.MetadataLoader(), nil)
		require.NoError(t, err)

		err = provider.Validate(context.Background(), build("http://localhost:4566"))
		var validationErr *destregistry.ErrDestinationValidation
		require.ErrorAs(t, err, &validationErr)
		assert.Equal(t, []destregistry.ValidationErrorDetail{{Field: "config.endpoint", Type: "forbidden"}}, validationErr.Errors)

		_, err = provider.CreatePublisher(context.Background(), build("http://localhost:4566"))
		require.Error(t, err)
		assert.ErrorAs(t, err, &validationErr)
	})

	t.Run("not allowed accepts destination without endpoint", func(t *testing.T) {
		t.Parallel()
		provider, err := destawss3.New(testutil.Registry.MetadataLoader(), nil)
		require.NoError(t, err)
		assert.NoError(t, provider.Validate(context.Background(), build("")))
	})

	t.Run("allowed accepts endpoint", func(t *testing.T) {
		t.Parallel()
		provider, err := destawss3.New(testutil.Registry.MetadataLoader(), nil, destawss3.WithAllowCustomEndpoints(true))
		require.NoError(t, err)
		assert.NoError(t, provider.Validate(context.Background(), build("http://localhost:4566")))
		assert.NoError(t, provider.Validate(context.Background(), build("")))
	})
}
