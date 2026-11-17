package destawssqs_test

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawssqs"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWSSQS_CustomEndpoint(t *testing.T) {
	t.Parallel()

	baseConfig := map[string]string{"queue_url": "https://sqs.us-east-1.amazonaws.com/123456789012/q"}
	build := func(endpoint string) *models.Destination {
		config := map[string]string{}
		for k, v := range baseConfig {
			config[k] = v
		}
		if endpoint != "" {
			config["endpoint"] = endpoint
		}
		d := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("aws_sqs"),
			testutil.DestinationFactory.WithConfig(config),
			testutil.DestinationFactory.WithCredentials(map[string]string{"key": "k", "secret": "s"}),
		)
		return &d
	}

	t.Run("not allowed rejects endpoint", func(t *testing.T) {
		t.Parallel()
		provider, err := destawssqs.New(testutil.Registry.MetadataLoader(), nil)
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
		provider, err := destawssqs.New(testutil.Registry.MetadataLoader(), nil)
		require.NoError(t, err)
		assert.NoError(t, provider.Validate(context.Background(), build("")))
	})

	t.Run("allowed accepts endpoint", func(t *testing.T) {
		t.Parallel()
		provider, err := destawssqs.New(testutil.Registry.MetadataLoader(), nil, destawssqs.WithAllowCustomEndpoints(true))
		require.NoError(t, err)
		assert.NoError(t, provider.Validate(context.Background(), build("http://localhost:4566")))
		assert.NoError(t, provider.Validate(context.Background(), build("")))
	})
}

func TestAWSSQS_QueueURLRestrictedWhenCustomEndpointsOff(t *testing.T) {
	t.Parallel()

	build := func(queueURL string) *models.Destination {
		d := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("aws_sqs"),
			testutil.DestinationFactory.WithConfig(map[string]string{"queue_url": queueURL}),
			testutil.DestinationFactory.WithCredentials(map[string]string{"key": "k", "secret": "s"}),
		)
		return &d
	}
	off, err := destawssqs.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)
	on, err := destawssqs.New(testutil.Registry.MetadataLoader(), nil, destawssqs.WithAllowCustomEndpoints(true))
	require.NoError(t, err)

	rejected := []string{
		"http://localhost:4566/000000000000/q",
		"http://sqs.us-east-1.amazonaws.com/123456789012/q",
		"https://sqs.us-east-1.amazonaws.com.evil.example/123456789012/q",
		"https://evil.example/sqs.us-east-1.amazonaws.com/q",
		"https://sqs.us-east-1.amazonaws.com@evil.example/123456789012/q",
		"https://sqs.us-east-1.amazonaws.com:8443/123456789012/q",
		"https://notsqs.us-east-1.amazonaws.com/123456789012/q",
		"https://sqs.evil.amazonaws.com/123456789012/q",
		"https://queue.example.com/123456789012/q",
	}
	for _, queueURL := range rejected {
		t.Run("off rejects "+queueURL, func(t *testing.T) {
			err := off.Validate(context.Background(), build(queueURL))
			var validationErr *destregistry.ErrDestinationValidation
			require.ErrorAs(t, err, &validationErr)
			assert.Equal(t, []destregistry.ValidationErrorDetail{{Field: "config.queue_url", Type: "forbidden"}}, validationErr.Errors)

			_, err = off.CreatePublisher(context.Background(), build(queueURL))
			assert.ErrorAs(t, err, &validationErr)

			assert.NoError(t, on.Validate(context.Background(), build(queueURL)))
		})
	}

	accepted := map[string]string{
		"https://sqs.us-east-1.amazonaws.com/123456789012/q":      "us-east-1",
		"https://sqs-fips.us-east-2.amazonaws.com/123456789012/q": "us-east-2",
		"https://sqs.us-west-2.api.aws/123456789012/q":            "us-west-2",
		"https://sqs-fips.us-east-1.api.aws/123456789012/q":       "us-east-1",
		"https://sqs.us-gov-west-1.amazonaws.com/123456789012/q":  "us-gov-west-1",
		"https://sqs.cn-north-1.amazonaws.com.cn/123456789012/q":  "cn-north-1",
		"https://eu-west-1.queue.amazonaws.com/123456789012/q":    "eu-west-1",
		"https://queue.amazonaws.com/123456789012/q":              "us-east-1",
		"https://SQS.us-east-1.amazonaws.com:443/123456789012/q":  "us-east-1",
	}
	for queueURL, region := range accepted {
		t.Run("off accepts "+queueURL, func(t *testing.T) {
			assert.NoError(t, off.Validate(context.Background(), build(queueURL)))
			_, gotRegion, err := destawssqs.ParseQueueURL(queueURL)
			require.NoError(t, err)
			assert.Equal(t, region, gotRegion)
		})
	}
}

func TestAWSSQS_ParseQueueURL_LookAlikeHostHasNoRegion(t *testing.T) {
	t.Parallel()
	for _, queueURL := range []string{
		"https://sqs.us-east-1.amazonaws.com.evil.example/1/q",
		"http://localhost:4566/000000000000/q",
	} {
		_, region, err := destawssqs.ParseQueueURL(queueURL)
		require.NoError(t, err)
		assert.Empty(t, region, queueURL)
	}
}
