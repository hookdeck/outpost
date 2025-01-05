package destrabbitmq_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destrabbitmq"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRabbitMQDestination_Validate(t *testing.T) {
	t.Parallel()

	validDestination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("rabbitmq"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"server_url":  "localhost:5672",
			"exchange":    "test-exchange",
			"routing_key": "test.key",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"username": "guest",
			"password": "guest",
		}),
	)

	rabbitmqDestination, err := destrabbitmq.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	t.Run("should validate valid destination", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, rabbitmqDestination.Validate(nil, &validDestination))
	})

	t.Run("should validate invalid type", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Type = "invalid"
		err := rabbitmqDestination.Validate(nil, &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "type", validationErr.Errors[0].Field)
		assert.Equal(t, "invalid_type", validationErr.Errors[0].Type)
	})

	t.Run("should validate missing credentials", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Credentials = map[string]string{}
		err := rabbitmqDestination.Validate(nil, &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		// Could be either username or password that's reported first
		assert.Contains(t, []string{"credentials.username", "credentials.password"}, validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should validate missing server_url", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"exchange": "test-exchange",
		}
		err := rabbitmqDestination.Validate(nil, &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.server_url", validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should validate malformed server_url", func(t *testing.T) {
		t.Parallel()
		invalidDestination := validDestination
		invalidDestination.Config = map[string]string{
			"server_url": "not-a-valid-url",
			"exchange":   "test-exchange",
		}
		err := rabbitmqDestination.Validate(nil, &invalidDestination)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.server_url", validationErr.Errors[0].Field)
		assert.Equal(t, "pattern", validationErr.Errors[0].Type)
	})

	t.Run("should validate valid destination without exchange", func(t *testing.T) {
		t.Parallel()
		validDestWithoutExchange := validDestination
		validDestWithoutExchange.Config = map[string]string{
			"server_url":  "localhost:5672",
			"routing_key": "test.key",
		}
		assert.NoError(t, rabbitmqDestination.Validate(nil, &validDestWithoutExchange))
	})

	t.Run("should validate empty routing_key as valid", func(t *testing.T) {
		t.Parallel()
		validDestWithEmptyRoutingKey := validDestination
		validDestWithEmptyRoutingKey.Config = map[string]string{
			"server_url":  "localhost:5672",
			"exchange":    "test-exchange",
			"routing_key": "",
		}
		assert.NoError(t, rabbitmqDestination.Validate(nil, &validDestWithEmptyRoutingKey))
	})

	t.Run("should validate empty exchange as valid", func(t *testing.T) {
		t.Parallel()
		validDestWithEmptyExchange := validDestination
		validDestWithEmptyExchange.Config = map[string]string{
			"server_url":  "localhost:5672",
			"exchange":    "",
			"routing_key": "test.key",
		}
		assert.NoError(t, rabbitmqDestination.Validate(nil, &validDestWithEmptyExchange))
	})

	t.Run("should validate empty exchange and routing_key as invalid", func(t *testing.T) {
		t.Parallel()
		invalidDest := validDestination
		invalidDest.Config = map[string]string{
			"server_url":  "localhost:5672",
			"exchange":    "",
			"routing_key": "",
		}
		err := rabbitmqDestination.Validate(nil, &invalidDest)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Len(t, validationErr.Errors, 2)
		assert.Equal(t, "config.exchange", validationErr.Errors[0].Field)
		assert.Equal(t, "either_required", validationErr.Errors[0].Type)
		assert.Equal(t, "config.routing_key", validationErr.Errors[1].Field)
		assert.Equal(t, "either_required", validationErr.Errors[1].Type)
	})
}

func TestRabbitMQDestination_ComputeTarget(t *testing.T) {
	t.Parallel()

	rabbitmqDestination, err := destrabbitmq.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	t.Run("should return exchange -> routing_key as target when both are present", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("rabbitmq"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"server_url":  "localhost:5672",
				"exchange":    "my-exchange",
				"routing_key": "my-key",
			}),
		)
		target := rabbitmqDestination.ComputeTarget(&destination)
		assert.Equal(t, "my-exchange -> my-key", target)
	})

	t.Run("should return only exchange when routing_key is empty", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("rabbitmq"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"server_url": "localhost:5672",
				"exchange":   "my-exchange",
			}),
		)
		target := rabbitmqDestination.ComputeTarget(&destination)
		assert.Equal(t, "my-exchange", target)
	})

	t.Run("should return only routing_key when exchange is empty", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("rabbitmq"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"server_url":  "localhost:5672",
				"routing_key": "my-key",
			}),
		)
		target := rabbitmqDestination.ComputeTarget(&destination)
		assert.Equal(t, "my-key", target)
	})
}
