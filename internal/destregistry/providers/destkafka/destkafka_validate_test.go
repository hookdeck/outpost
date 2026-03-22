package destkafka_test

import (
	"context"
	"maps"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destkafka"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKafkaDestination_Validate(t *testing.T) {
	t.Parallel()

	validDestination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("kafka"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"brokers": "localhost:9092",
			"topic":   "test-topic",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{}),
	)

	kafkaDestination, err := destkafka.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)

	t.Run("should validate valid destination", func(t *testing.T) {
		t.Parallel()
		assert.NoError(t, kafkaDestination.Validate(context.Background(), &validDestination))
	})

	t.Run("should validate invalid type", func(t *testing.T) {
		t.Parallel()
		dest := validDestination
		dest.Config = maps.Clone(validDestination.Config)
		dest.Credentials = maps.Clone(validDestination.Credentials)
		dest.Type = "invalid"
		err := kafkaDestination.Validate(context.Background(), &dest)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "type", validationErr.Errors[0].Field)
		assert.Equal(t, "invalid_type", validationErr.Errors[0].Type)
	})

	t.Run("should validate missing brokers", func(t *testing.T) {
		t.Parallel()
		dest := validDestination
		dest.Config = map[string]string{
			"topic": "test-topic",
		}
		dest.Credentials = maps.Clone(validDestination.Credentials)
		err := kafkaDestination.Validate(context.Background(), &dest)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.brokers", validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should validate missing topic", func(t *testing.T) {
		t.Parallel()
		dest := validDestination
		dest.Config = map[string]string{
			"brokers": "localhost:9092",
		}
		dest.Credentials = maps.Clone(validDestination.Credentials)
		err := kafkaDestination.Validate(context.Background(), &dest)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.topic", validationErr.Errors[0].Field)
		assert.Equal(t, "required", validationErr.Errors[0].Type)
	})

	t.Run("should validate sasl_mechanism values", func(t *testing.T) {
		t.Parallel()
		testCases := []struct {
			name        string
			mechanism   string
			shouldError bool
		}{
			{name: "valid plain", mechanism: "plain", shouldError: false},
			{name: "valid scram-sha-256", mechanism: "scram-sha-256", shouldError: false},
			{name: "valid scram-sha-512", mechanism: "scram-sha-512", shouldError: false},
			{name: "invalid mechanism", mechanism: "oauth", shouldError: true},
			{name: "empty is valid (no auth)", mechanism: "", shouldError: false},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				dest := validDestination
				dest.Config = maps.Clone(validDestination.Config)
				// Provide credentials when mechanism is set so we only test mechanism validation
				if tc.mechanism != "" {
					dest.Credentials = map[string]string{"username": "user", "password": "pass"}
				} else {
					dest.Credentials = maps.Clone(validDestination.Credentials)
				}
				if tc.mechanism != "" {
					dest.Config["sasl_mechanism"] = tc.mechanism
				}
				err := kafkaDestination.Validate(context.Background(), &dest)
				if tc.shouldError {
					var validationErr *destregistry.ErrDestinationValidation
					if !assert.ErrorAs(t, err, &validationErr) {
						return
					}
					assert.Equal(t, "config.sasl_mechanism", validationErr.Errors[0].Field)
					assert.Equal(t, "invalid", validationErr.Errors[0].Type)
				} else {
					assert.NoError(t, err)
				}
			})
		}
	})

	t.Run("should validate tls config values", func(t *testing.T) {
		t.Parallel()
		testCases := []struct {
			name        string
			tlsValue    string
			shouldError bool
		}{
			{name: "valid true", tlsValue: "true", shouldError: false},
			{name: "valid on", tlsValue: "on", shouldError: false},
			{name: "valid false", tlsValue: "false", shouldError: false},
			{name: "invalid value", tlsValue: "yes", shouldError: true},
			{name: "empty value is valid (not configured)", tlsValue: "", shouldError: false},
			{name: "case sensitive True", tlsValue: "True", shouldError: true},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				dest := validDestination
				dest.Config = maps.Clone(validDestination.Config)
				dest.Credentials = maps.Clone(validDestination.Credentials)
				dest.Config["tls"] = tc.tlsValue
				err := kafkaDestination.Validate(context.Background(), &dest)
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

	t.Run("should require credentials when sasl_mechanism is set", func(t *testing.T) {
		t.Parallel()
		testCases := []struct {
			name        string
			mechanism   string
			username    string
			password    string
			shouldError bool
		}{
			{name: "plain with credentials", mechanism: "plain", username: "user", password: "pass", shouldError: false},
			{name: "plain without username", mechanism: "plain", username: "", password: "pass", shouldError: true},
			{name: "plain without password", mechanism: "plain", username: "user", password: "", shouldError: true},
			{name: "scram-sha-256 with credentials", mechanism: "scram-sha-256", username: "user", password: "pass", shouldError: false},
			{name: "scram-sha-256 without credentials", mechanism: "scram-sha-256", username: "", password: "", shouldError: true},
			{name: "no mechanism no credentials", mechanism: "", username: "", password: "", shouldError: false},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				dest := validDestination
				dest.Config = maps.Clone(validDestination.Config)
				dest.Credentials = map[string]string{
					"username": tc.username,
					"password": tc.password,
				}
				if tc.mechanism != "" {
					dest.Config["sasl_mechanism"] = tc.mechanism
				}
				err := kafkaDestination.Validate(context.Background(), &dest)
				if tc.shouldError {
					var validationErr *destregistry.ErrDestinationValidation
					if !assert.ErrorAs(t, err, &validationErr) {
						return
					}
					assert.Equal(t, "credentials", validationErr.Errors[0].Field)
					assert.Equal(t, "required", validationErr.Errors[0].Type)
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
		assert.NoError(t, kafkaDestination.Validate(context.Background(), &dest))
	})
}

func TestKafkaDestination_ComputeTarget(t *testing.T) {
	t.Parallel()

	kafkaDestination, err := destkafka.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)

	t.Run("should return 'broker / topic' for single broker", func(t *testing.T) {
		t.Parallel()
		dest := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("kafka"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"brokers": "broker1:9092",
				"topic":   "my-topic",
			}),
		)
		target := kafkaDestination.ComputeTarget(&dest)
		assert.Equal(t, "broker1:9092 / my-topic", target.Target)
		assert.Empty(t, target.TargetURL)
	})

	t.Run("should return first broker for multiple brokers", func(t *testing.T) {
		t.Parallel()
		dest := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("kafka"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"brokers": "broker1:9092,broker2:9092,broker3:9092",
				"topic":   "my-topic",
			}),
		)
		target := kafkaDestination.ComputeTarget(&dest)
		assert.Equal(t, "broker1:9092 / my-topic", target.Target)
	})
}

func TestKafkaDestination_Preprocess(t *testing.T) {
	t.Parallel()

	kafkaDestination, err := destkafka.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)

	t.Run("should normalize tls 'on' to 'true'", func(t *testing.T) {
		t.Parallel()
		dest := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("kafka"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"brokers": "broker1:9092",
				"topic":   "my-topic",
				"tls":     "on",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{}),
		)
		err := kafkaDestination.Preprocess(&dest, nil, nil)
		require.NoError(t, err)
		assert.Equal(t, "true", dest.Config["tls"])
	})

	t.Run("should normalize empty tls to 'false'", func(t *testing.T) {
		t.Parallel()
		dest := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("kafka"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"brokers": "broker1:9092",
				"topic":   "my-topic",
				"tls":     "",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{}),
		)
		err := kafkaDestination.Preprocess(&dest, nil, nil)
		require.NoError(t, err)
		assert.Equal(t, "false", dest.Config["tls"])
	})

	t.Run("should trim whitespace from brokers", func(t *testing.T) {
		t.Parallel()
		dest := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("kafka"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"brokers": " broker1:9092 , broker2:9092 ",
				"topic":   "my-topic",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{}),
		)
		err := kafkaDestination.Preprocess(&dest, nil, nil)
		require.NoError(t, err)
		assert.Equal(t, "broker1:9092,broker2:9092", dest.Config["brokers"])
	})
}
