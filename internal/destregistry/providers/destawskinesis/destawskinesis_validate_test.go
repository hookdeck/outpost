package destawskinesis_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destawskinesis"
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
			"metadata_in_payload":    "true",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"key":     "test-key",
			"secret":  "test-secret",
			"session": "test-session",
		}),
	)

	awsKinesisDestination, err := destawskinesis.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	t.Run("should validate valid destination", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, awsKinesisDestination.Validate(nil, &validDestination))
	})

	t.Run("should validate invalid type", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Type = "invalid"
		err := awsKinesisDestination.Validate(nil, &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "type", validationErr.Errors[0].Field)
		assert.Equal(t, "invalid_type", validationErr.Errors[0].Type)
	})

	t.Run("should validate missing stream_name", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"region":              "us-east-1",
			"endpoint":            "https://kinesis.us-east-1.amazonaws.com",
			"metadata_in_payload": "true",
		}
		err := awsKinesisDestination.Validate(nil, &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.stream_name", validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should validate missing region", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"stream_name":         "my-stream",
			"endpoint":            "https://kinesis.us-east-1.amazonaws.com",
			"metadata_in_payload": "true",
		}
		err := awsKinesisDestination.Validate(nil, &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.region", validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should validate malformed endpoint", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"stream_name":         "my-stream",
			"region":              "us-east-1",
			"endpoint":            "not-a-valid-url",
			"metadata_in_payload": "true",
		}
		err := awsKinesisDestination.Validate(nil, &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.endpoint", validationErr.Errors[0].Field)
		assert.Equal(t, "pattern", validationErr.Errors[0].Type)
	})

	t.Run("should validate invalid region format", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"stream_name":         "my-stream",
			"region":              "invalid-region",
			"endpoint":            "https://kinesis.us-east-1.amazonaws.com",
			"metadata_in_payload": "true",
		}
		err := awsKinesisDestination.Validate(nil, &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.region", validationErr.Errors[0].Field)
		assert.Equal(t, "pattern", validationErr.Errors[0].Type)
	})

	t.Run("should validate missing credentials", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Credentials = map[string]string{}
		err := awsKinesisDestination.Validate(nil, &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		// Could be either key or secret that's reported first
		assert.Contains(t, []string{"credentials.key", "credentials.secret"}, validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should validate metadata_in_payload values", func(t *testing.T) {
		t.Parallel()

		// Test valid values
		validValues := []string{"true", "false", "on", "off", ""}
		for _, val := range validValues {
			validValueDest := validDestination
			validValueDest.Config["metadata_in_payload"] = val
			assert.NoError(t, awsKinesisDestination.Validate(nil, &validValueDest),
				"Value '%s' should be valid for metadata_in_payload", val)
		}

		// Test invalid value
		invalidValueDestination := validDestination
		invalidValueDestination.Config["metadata_in_payload"] = "invalid"
		err := awsKinesisDestination.Validate(nil, &invalidValueDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.metadata_in_payload", validationErr.Errors[0].Field)
		assert.Equal(t, "allowed_values", validationErr.Errors[0].Type)
	})
}

func TestAWSKinesisDestination_Preprocess(t *testing.T) {
	t.Parallel()

	awsKinesisDestination, err := destawskinesis.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	// Create a fully valid destination as a base for our tests
	baseDestination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("aws_kinesis"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"stream_name": "my-stream",
			"region":      "us-east-1",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"key":    "test-key",
			"secret": "test-secret",
		}),
	)

	t.Run("should standardize metadata_in_payload values", func(t *testing.T) {
		t.Parallel()

		// Test with "on" - should be converted to "true"
		onDest := baseDestination
		onDest.Config["metadata_in_payload"] = "on"
		err := awsKinesisDestination.Preprocess(&onDest, nil, nil)
		assert.NoError(t, err)
		assert.Equal(t, "true", onDest.Config["metadata_in_payload"])

		// Test with "off" - should be converted to "false"
		offDest := baseDestination
		offDest.Config["metadata_in_payload"] = "off"
		err = awsKinesisDestination.Preprocess(&offDest, nil, nil)
		assert.NoError(t, err)
		assert.Equal(t, "false", offDest.Config["metadata_in_payload"])

		// Test with empty string - should be converted to "false"
		emptyDest := baseDestination
		emptyDest.Config["metadata_in_payload"] = ""
		err = awsKinesisDestination.Preprocess(&emptyDest, nil, nil)
		assert.NoError(t, err)
		assert.Equal(t, "false", emptyDest.Config["metadata_in_payload"])

		// Test with no metadata_in_payload - should default to "true"
		noMetadataDest := baseDestination
		delete(noMetadataDest.Config, "metadata_in_payload")
		err = awsKinesisDestination.Preprocess(&noMetadataDest, nil, nil)
		assert.NoError(t, err)
		assert.Equal(t, "true", noMetadataDest.Config["metadata_in_payload"])
	})
}

func TestAWSKinesisDestination_ComputeTarget(t *testing.T) {
	t.Parallel()

	awsKinesisDestination, err := destawskinesis.New(testutil.Registry.MetadataLoader())
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
		assert.Equal(t, "my-stream in us-east-1", target)
	})
}
