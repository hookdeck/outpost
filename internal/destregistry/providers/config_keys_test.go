package destregistrydefault_test

import (
	"sort"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// keyFixtures holds a valid value for every key each type keeps. A key added
// to a provider's metadata or allowlist fails the test until it is added here.
var keyFixtures = map[string]struct{ config, credentials map[string]string }{
	"webhook": {
		config:      map[string]string{"url": "https://example.com/hook", "custom_headers": `{"x-key":"value"}`},
		credentials: map[string]string{"secret": "whsec_abcdef0123456789", "previous_secret": "whsec_0123456789abcdef", "previous_secret_invalid_at": "2099-01-01T00:00:00Z"},
	},
	"hookdeck": {
		config:      map[string]string{"source_id": "src_abc", "source_name": "source"},
		credentials: map[string]string{"token": "src_abc:token"},
	},
	"aws_sqs": {
		config:      map[string]string{"queue_url": "https://sqs.us-east-1.amazonaws.com/123456789012/queue", "endpoint": "http://localhost:4566"},
		credentials: map[string]string{"key": "k", "secret": "s", "session": "t"},
	},
	"aws_s3": {
		config:      map[string]string{"bucket": "bucket", "region": "us-east-1", "key_template": `metadata."event-id"`, "storage_class": "STANDARD", "endpoint": "http://localhost:4566"},
		credentials: map[string]string{"key": "k", "secret": "s", "session": "t"},
	},
	"aws_eventbridge": {
		config:      map[string]string{"event_bus_name": "bus", "region": "us-east-1", "endpoint": "http://localhost:4566"},
		credentials: map[string]string{"key": "k", "secret": "s", "session": "t"},
	},
	"aws_kinesis": {
		config:      map[string]string{"stream_name": "stream", "region": "us-east-1", "endpoint": "http://localhost:4566", "partition_key_template": `metadata."event-id"`},
		credentials: map[string]string{"key": "k", "secret": "s", "session": "t"},
	},
	"gcp_pubsub": {
		config:      map[string]string{"project_id": "project", "topic": "topic", "endpoint": "localhost:8085"},
		credentials: map[string]string{"service_account_json": `{"type":"service_account"}`},
	},
	"azure_servicebus": {
		config:      map[string]string{"name": "queue"},
		credentials: map[string]string{"connection_string": "Endpoint=sb://example.servicebus.windows.net/;SharedAccessKeyName=k;SharedAccessKey=v"},
	},
	"cloudflare_queues": {
		config:      map[string]string{"account_id": "0123456789abcdef0123456789abcdef", "queue_id": "fedcba9876543210fedcba9876543210"},
		credentials: map[string]string{"api_token": "token"},
	},
	"kafka": {
		config:      map[string]string{"brokers": "localhost:9092", "topic": "topic", "tls": "true", "partition_key_template": `metadata."event-id"`, "sasl_mechanism": "plain"},
		credentials: map[string]string{"username": "user", "password": "pass"},
	},
	"rabbitmq": {
		config:      map[string]string{"server_url": "localhost:5672", "exchange": "exchange", "tls": "true"},
		credentials: map[string]string{"username": "user", "password": "pass"},
	},
	"mcp": {
		config:      map[string]string{"url": "https://example.com/mcp", "arguments": `{"a":"b"}`},
		credentials: map[string]string{"secret": "s", "previous_secret": "p", "previous_secret_invalid_at": "2099-01-01T00:00:00Z"},
	},
}

// directDrop lists the types whose Preprocess or Validate cannot run on a
// fixture alone: hookdeck verifies its token over the network, webhook rejects
// secrets set without the admin role, and mcp checks topics against a catalog. They are tested through
// the drop step the registry runs after Preprocess.
var directDrop = map[string]bool{"hookdeck": true, "webhook": true, "mcp": true}

func TestProviders_KeysSurviveWriteAndExtrasDoNot(t *testing.T) {
	registry := newKeysTestRegistry(t, true)

	for _, destType := range providerDirs {
		t.Run(destType, func(t *testing.T) {
			fixture, ok := keyFixtures[destType]
			require.True(t, ok, "no fixture for %s", destType)

			provider := providerKeys(t, registry, destType)
			assert.Equal(t, sortedMapKeys(fixture.config), provider.ConfigKeys(), "fixture config keys differ from the keys the provider keeps")
			assert.Equal(t, sortedMapKeys(fixture.credentials), provider.CredentialKeys(), "fixture credential keys differ from the keys the provider keeps")

			config := cloneWith(fixture.config, "undeclared_key", "x")
			credentials := cloneWith(fixture.credentials, "undeclared_key", "x")
			destination := &models.Destination{
				ID:          "des_1",
				Type:        destType,
				Topics:      []string{"*"},
				Config:      config,
				Credentials: credentials,
			}

			if directDrop[destType] {
				p, err := registry.ResolveProvider(destination)
				require.NoError(t, err)
				p.DropUndeclaredKeys(destination)
			} else {
				require.NoError(t, registry.ValidateDestination(t.Context(), destination))
				require.NoError(t, registry.PreprocessDestination(destination, nil, &destregistry.PreprocessDestinationOpts{}))
			}

			display, err := registry.DisplayDestination(destination)
			require.NoError(t, err)
			for _, got := range []struct {
				name    string
				want    map[string]string
				have    models.MapStringString
				mutated models.MapStringString
			}{
				{"config", fixture.config, display.Destination.Config, config},
				{"credentials", fixture.credentials, display.Destination.Credentials, credentials},
			} {
				assert.Equal(t, sortedMapKeys(got.want), sortedMapKeys(got.have), "%s keys in the response", got.name)
				assert.Contains(t, got.mutated, "undeclared_key", "the caller's map is not modified")
			}
			assert.Equal(t, sortedMapKeys(fixture.config), sortedMapKeys(destination.Config))
			assert.Equal(t, sortedMapKeys(fixture.credentials), sortedMapKeys(destination.Credentials))
		})
	}
}

func cloneWith(m map[string]string, key, value string) models.MapStringString {
	out := make(models.MapStringString, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	out[key] = value
	return out
}

func sortedMapKeys[M ~map[string]string](m M) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
