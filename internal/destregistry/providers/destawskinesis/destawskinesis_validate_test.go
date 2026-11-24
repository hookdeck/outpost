package destawskinesis_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawskinesis"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWSKinesisDestination_Validate(t *testing.T) {
	t.Parallel()

	validDestination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("aws_kinesis"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"stream_name":            "my-stream",
			"region":                 "us-east-1",
			"endpoint":               "https://kinesis.us-east-1.amazonaws.com",
			"partition_key_template": "metadata.\"event-id\"",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"key":     "test-key",
			"secret":  "test-secret",
			"session": "test-session",
		}),
	)

	awsKinesisDestination, err := destawskinesis.New(testutil.Registry.MetadataLoader(), nil, destawskinesis.WithAllowCustomEndpoints(true))
	require.NoError(t, err)

	t.Run("should validate valid destination", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, awsKinesisDestination.Validate(context.Background(), &validDestination))
	})

	withTemplate := func(template string) *models.Destination {
		dest := validDestination
		dest.Config = map[string]string{
			"stream_name":            "my-stream",
			"region":                 "us-east-1",
			"partition_key_template": template,
		}
		return &dest
	}

	t.Run("should reject a partition key template that does not compile", func(t *testing.T) {
		t.Parallel()
		err := awsKinesisDestination.Validate(context.Background(), withTemplate("invalid[template"))
		var validationErr *destregistry.ErrDestinationValidation
		require.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.partition_key_template", validationErr.Errors[0].Field)
		assert.Equal(t, "pattern", validationErr.Errors[0].Type)
	})

	t.Run("should reject a partition key template over the length limit", func(t *testing.T) {
		t.Parallel()
		err := awsKinesisDestination.Validate(context.Background(), withTemplate("join('', ['"+strings.Repeat("a", 1024)+"'])"))
		var validationErr *destregistry.ErrDestinationValidation
		require.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.partition_key_template", validationErr.Errors[0].Field)
		assert.Equal(t, "maxlength", validationErr.Errors[0].Type)
	})

	t.Run("should accept an omitted partition key template", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, awsKinesisDestination.Validate(context.Background(), withTemplate("")))
	})

	t.Run("should validate invalid type", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Type = "invalid"
		err := awsKinesisDestination.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "type", validationErr.Errors[0].Field)
		assert.Equal(t, "invalid_type", validationErr.Errors[0].Type)
	})

	t.Run("should validate missing stream_name", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"region":   "us-east-1",
			"endpoint": "https://kinesis.us-east-1.amazonaws.com",
		}
		err := awsKinesisDestination.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.stream_name", validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should validate missing region", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"stream_name": "my-stream",
			"endpoint":    "https://kinesis.us-east-1.amazonaws.com",
		}
		err := awsKinesisDestination.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.region", validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should validate malformed endpoint", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"stream_name": "my-stream",
			"region":      "us-east-1",
			"endpoint":    "not-a-valid-url",
		}
		err := awsKinesisDestination.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.endpoint", validationErr.Errors[0].Field)
		assert.Equal(t, "pattern", validationErr.Errors[0].Type)
	})

	t.Run("should validate invalid region format", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"stream_name": "my-stream",
			"region":      "invalid-region",
			"endpoint":    "https://kinesis.us-east-1.amazonaws.com",
		}
		err := awsKinesisDestination.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.region", validationErr.Errors[0].Field)
		assert.Equal(t, "pattern", validationErr.Errors[0].Type)
	})

	t.Run("should validate missing credentials", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Credentials = map[string]string{}
		err := awsKinesisDestination.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		// Could be either key or secret that's reported first
		assert.Contains(t, []string{"credentials.key", "credentials.secret"}, validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})
}

func TestAWSKinesisDestination_ComputeTarget(t *testing.T) {
	t.Parallel()

	awsKinesisDestination, err := destawskinesis.New(testutil.Registry.MetadataLoader(), nil, destawskinesis.WithAllowCustomEndpoints(true))
	require.NoError(t, err)

	t.Run("should return stream and region as target", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("aws_kinesis"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"stream_name": "my-stream",
				"region":      "us-east-1",
			}),
		)
		target := awsKinesisDestination.ComputeTarget(&destination)
		assert.Equal(t, "my-stream in us-east-1", target.Target)
	})
}

func TestAWSKinesisDestination_CreatePublisher_StoredTemplateThatDoesNotCompile(t *testing.T) {
	t.Parallel()

	provider, err := destawskinesis.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)

	dest := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("aws_kinesis"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"stream_name":            "my-stream",
			"region":                 "us-east-1",
			"partition_key_template": "data.[",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"key":    "test-key",
			"secret": "test-secret",
		}),
	)

	require.Error(t, provider.Validate(context.Background(), &dest), "rejected on create and update")

	publisher, err := provider.CreatePublisher(context.Background(), &dest)
	require.NoError(t, err, "a stored destination still gets a publisher and falls back to the event ID")
	publisher.Close()
}
