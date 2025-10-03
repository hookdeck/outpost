package destawss3_test

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawss3"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAWSS3Destination_Validate(t *testing.T) {
	t.Parallel()

	validDestination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("aws_s3"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"bucket":        "my-bucket",
			"region":        "us-east-1",
			"key_template":  `join('', ['events/', metadata."event-id", '.json'])`,
			"storage_class": "STANDARD",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"key":     "test-key",
			"secret":  "test-secret",
			"session": "test-session",
		}),
	)

	awsS3Destination, err := destawss3.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	t.Run("should validate valid destination", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, awsS3Destination.Validate(context.Background(), &validDestination))
	})

	t.Run("should validate invalid type", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Type = "invalid"
		err := awsS3Destination.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "type", validationErr.Errors[0].Field)
		assert.Equal(t, "invalid_type", validationErr.Errors[0].Type)
	})

	t.Run("should validate missing bucket", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"region": "us-east-1",
		}
		err := awsS3Destination.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.bucket", validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should validate missing region", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"bucket": "my-bucket",
		}
		err := awsS3Destination.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.region", validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should validate malformed region", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"bucket": "my-bucket",
			"region": "invalid-region",
		}
		err := awsS3Destination.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.region", validationErr.Errors[0].Field)
		assert.Equal(t, "pattern", validationErr.Errors[0].Type)
	})

	t.Run("should validate invalid storage class", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config["storage_class"] = "INVALID"
		err := awsS3Destination.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.storage_class", validationErr.Errors[0].Field)
		assert.Equal(t, "enum", validationErr.Errors[0].Type)
	})

	t.Run("should validate invalid key template", func(t *testing.T) {
		t.Parallel()
		invalidDestination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("aws_s3"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"bucket":        "my-bucket",
				"region":        "us-east-1",
				"key_template":  "invalid[template",
				"storage_class": "STANDARD",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"key":     "test-key",
				"secret":  "test-secret",
				"session": "test-session",
			}),
		)
		err := awsS3Destination.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.key_template", validationErr.Errors[0].Field)
		assert.Equal(t, "pattern", validationErr.Errors[0].Type)
	})

	t.Run("should validate missing credentials", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Credentials = map[string]string{}
		err := awsS3Destination.Validate(context.Background(), &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Contains(t, []string{"credentials.key", "credentials.secret"}, validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})
}

func TestAWSS3Destination_ComputeTarget(t *testing.T) {
	t.Parallel()

	awsS3Destination, err := destawss3.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("aws_s3"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"bucket": "my-bucket",
			"region": "us-east-1",
		}),
	)

	target := awsS3Destination.ComputeTarget(&destination)
	assert.Equal(t, "my-bucket in us-east-1", target.Target)
	assert.Equal(t, "https://s3.console.aws.amazon.com/s3/buckets/my-bucket?region=us-east-1", target.TargetURL)
}
