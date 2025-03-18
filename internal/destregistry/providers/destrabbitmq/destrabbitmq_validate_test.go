package destrabbitmq_test

import (
	"context"
	"maps"
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
		assert.NoError(t, rabbitmqDestination.Validate(context.Background(), &validDestination))
	})

	t.Run("should validate invalid type", func(t *testing.T) {
		t.Parallel()
		dest := validDestination
		dest.Config = maps.Clone(validDestination.Config)
		dest.Credentials = maps.Clone(validDestination.Credentials)
		dest.Type = "invalid"
		err := rabbitmqDestination.Validate(context.Background(), &dest)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "type", validationErr.Errors[0].Field)
		assert.Equal(t, "invalid_type", validationErr.Errors[0].Type)
	})

	t.Run("should validate missing credentials", func(t *testing.T) {
		t.Parallel()
		dest := validDestination
		dest.Config = maps.Clone(validDestination.Config)
		dest.Credentials = map[string]string{}
		err := rabbitmqDestination.Validate(context.Background(), &dest)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Contains(t, []string{"credentials.username", "credentials.password"}, validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should validate missing server_url", func(t *testing.T) {
		t.Parallel()
		dest := validDestination
		dest.Config = map[string]string{
			"exchange": "test-exchange",
		}
		dest.Credentials = maps.Clone(validDestination.Credentials)
		err := rabbitmqDestination.Validate(context.Background(), &dest)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.server_url", validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should validate malformed server_url", func(t *testing.T) {
		t.Parallel()
		dest := validDestination
		dest.Config = map[string]string{
			"server_url": "not-a-valid-url",
			"exchange":   "test-exchange",
		}
		dest.Credentials = maps.Clone(validDestination.Credentials)
		err := rabbitmqDestination.Validate(context.Background(), &dest)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.server_url", validationErr.Errors[0].Field)
		assert.Equal(t, "pattern", validationErr.Errors[0].Type)
	})

	t.Run("should validate valid destination without exchange", func(t *testing.T) {
		t.Parallel()
		dest := validDestination
		dest.Config = map[string]string{
			"server_url":  "localhost:5672",
			"routing_key": "test.key",
		}
		dest.Credentials = maps.Clone(validDestination.Credentials)
		assert.NoError(t, rabbitmqDestination.Validate(context.Background(), &dest))
	})

	t.Run("should validate empty routing_key as valid", func(t *testing.T) {
		t.Parallel()
		dest := validDestination
		dest.Config = map[string]string{
			"server_url":  "localhost:5672",
			"exchange":    "test-exchange",
			"routing_key": "",
		}
		dest.Credentials = maps.Clone(validDestination.Credentials)
		assert.NoError(t, rabbitmqDestination.Validate(context.Background(), &dest))
	})

	t.Run("should validate empty exchange as valid", func(t *testing.T) {
		t.Parallel()
		dest := validDestination
		dest.Config = map[string]string{
			"server_url":  "localhost:5672",
			"exchange":    "",
			"routing_key": "test.key",
		}
		dest.Credentials = maps.Clone(validDestination.Credentials)
		assert.NoError(t, rabbitmqDestination.Validate(context.Background(), &dest))
	})

	t.Run("should validate empty exchange and routing_key as invalid", func(t *testing.T) {
		t.Parallel()
		dest := validDestination
		dest.Config = map[string]string{
			"server_url":  "localhost:5672",
			"exchange":    "",
			"routing_key": "",
		}
		dest.Credentials = maps.Clone(validDestination.Credentials)
		err := rabbitmqDestination.Validate(context.Background(), &dest)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Len(t, validationErr.Errors, 2)
		assert.Equal(t, "config.exchange", validationErr.Errors[0].Field)
		assert.Equal(t, "either_required", validationErr.Errors[0].Type)
		assert.Equal(t, "config.routing_key", validationErr.Errors[1].Field)
		assert.Equal(t, "either_required", validationErr.Errors[1].Type)
	})

	t.Run("should validate tls config values", func(t *testing.T) {
		t.Parallel()
		testCases := []struct {
			name        string
			tlsValue    string
			shouldError bool
		}{
			{
				name:        "valid true value ('true')",
				tlsValue:    "true",
				shouldError: false,
			},
			{
				name:        "valid true value ('on')",
				tlsValue:    "on",
				shouldError: false,
			},
			{
				name:        "valid false value",
				tlsValue:    "false",
				shouldError: false,
			},
			{
				name:        "invalid value",
				tlsValue:    "yes",
				shouldError: true,
			},
			{
				name:        "empty value",
				tlsValue:    "",
				shouldError: true,
			},
			{
				name:        "case sensitive True",
				tlsValue:    "True",
				shouldError: true,
			},
		}

		for _, tc := range testCases {
			tc := tc
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				dest := validDestination
				dest.Config = maps.Clone(validDestination.Config)
				dest.Credentials = maps.Clone(validDestination.Credentials)
				dest.Config["tls"] = tc.tlsValue
				err := rabbitmqDestination.Validate(context.Background(), &dest)
				if tc.shouldError {
					var validationErr *destregistry.ErrDestinationValidation
					if !assert.ErrorAs(t, err, &validationErr) {
						return
					}
					assert.Equal(t, "config.tls", validationErr.Errors[0].Field)
					assert.Equal(t, "invalid", validationErr.Errors[0].Type)
				} else {
					assert.NoError(t, err)
				}
			})
		}
	})

	t.Run("should allow tls to be omitted", func(t *testing.T) {
		t.Parallel()
		dest := validDestination
		dest.Config = maps.Clone(validDestination.Config)
		dest.Credentials = maps.Clone(validDestination.Credentials)
		delete(dest.Config, "tls")
		assert.NoError(t, rabbitmqDestination.Validate(context.Background(), &dest))
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
