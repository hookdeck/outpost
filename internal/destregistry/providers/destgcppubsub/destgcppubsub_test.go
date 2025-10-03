package destgcppubsub_test

import (
	"context"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destgcppubsub"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeTarget(t *testing.T) {
	provider, err := destgcppubsub.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	tests := []struct {
		name           string
		config         map[string]string
		expectedTarget string
		expectedURL    string
	}{
		{
			name: "with valid project and topic",
			config: map[string]string{
				"project_id": "my-project",
				"topic":      "my-topic",
			},
			expectedTarget: "my-project/my-topic",
			expectedURL:    "https://console.cloud.google.com/cloudpubsub/topic/detail/my-topic?project=my-project",
		},
		{
			name: "with different project and topic",
			config: map[string]string{
				"project_id": "test-project-123",
				"topic":      "events-topic",
			},
			expectedTarget: "test-project-123/events-topic",
			expectedURL:    "https://console.cloud.google.com/cloudpubsub/topic/detail/events-topic?project=test-project-123",
		},
		{
			name: "with missing project_id",
			config: map[string]string{
				"topic": "my-topic",
			},
			expectedTarget: "/my-topic",
			expectedURL:    "https://console.cloud.google.com/cloudpubsub/topic/detail/my-topic?project=",
		},
		{
			name: "with missing topic",
			config: map[string]string{
				"project_id": "my-project",
			},
			expectedTarget: "my-project/",
			expectedURL:    "https://console.cloud.google.com/cloudpubsub/topic/detail/?project=my-project",
		},
		{
			name:           "with missing both",
			config:         map[string]string{},
			expectedTarget: "/",
			expectedURL:    "https://console.cloud.google.com/cloudpubsub/topic/detail/?project=",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			destination := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithType("gcp_pubsub"),
				testutil.DestinationFactory.WithConfig(tt.config),
			)
			result := provider.ComputeTarget(&destination)
			assert.Equal(t, tt.expectedTarget, result.Target)
			assert.Equal(t, tt.expectedURL, result.TargetURL)
		})
	}
}

func TestValidate(t *testing.T) {
	provider, err := destgcppubsub.New(testutil.Registry.MetadataLoader())
	require.NoError(t, err)

	tests := []struct {
		name        string
		config      map[string]string
		credentials map[string]string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid destination",
			config: map[string]string{
				"project_id": "my-project",
				"topic":      "my-topic",
			},
			credentials: map[string]string{
				"service_account_json": `{"type":"service_account","project_id":"my-project"}`,
			},
			wantErr: false,
		},
		{
			name: "valid destination with endpoint (emulator)",
			config: map[string]string{
				"project_id": "my-project",
				"topic":      "my-topic",
				"endpoint":   "http://localhost:8085",
			},
			credentials: map[string]string{
				"service_account_json": `{"type":"service_account"}`,
			},
			wantErr: false,
		},
		{
			name: "missing project_id",
			config: map[string]string{
				"topic": "my-topic",
			},
			credentials: map[string]string{
				"service_account_json": `{"type":"service_account"}`,
			},
			wantErr:     true,
			errContains: "config.project_id",
		},
		{
			name: "missing topic",
			config: map[string]string{
				"project_id": "my-project",
			},
			credentials: map[string]string{
				"service_account_json": `{"type":"service_account"}`,
			},
			wantErr:     true,
			errContains: "config.topic",
		},
		{
			name: "missing service_account_json",
			config: map[string]string{
				"project_id": "my-project",
				"topic":      "my-topic",
			},
			credentials: map[string]string{},
			wantErr:     true,
			errContains: "credentials.service_account_json",
		},
		{
			name: "empty service_account_json",
			config: map[string]string{
				"project_id": "my-project",
				"topic":      "my-topic",
			},
			credentials: map[string]string{
				"service_account_json": "",
			},
			wantErr:     true,
			errContains: "credentials.service_account_json",
		},
		{
			name: "invalid JSON in service_account_json",
			config: map[string]string{
				"project_id": "my-project",
				"topic":      "my-topic",
			},
			credentials: map[string]string{
				"service_account_json": "not-valid-json",
			},
			wantErr: false, // We don't validate JSON structure anymore, Google SDK will handle it
		},
		{
			name: "valid with all optional fields",
			config: map[string]string{
				"project_id": "my-project",
				"topic":      "my-topic",
				"endpoint":   "https://pubsub.googleapis.com",
			},
			credentials: map[string]string{
				"service_account_json": `{"type":"service_account","project_id":"my-project"}`,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			destination := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithType("gcp_pubsub"),
				testutil.DestinationFactory.WithConfig(tt.config),
				testutil.DestinationFactory.WithCredentials(tt.credentials),
			)
			ctx := context.Background()
			err := provider.Validate(ctx, &destination)
			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					var validationErr *destregistry.ErrDestinationValidation
					require.ErrorAs(t, err, &validationErr)
					require.NotEmpty(t, validationErr.Errors)
					// Check that at least one error contains the expected field
					found := false
					for _, e := range validationErr.Errors {
						if strings.Contains(e.Field, tt.errContains) {
							found = true
							break
						}
					}
					assert.True(t, found, "Expected error field containing %q, but got %+v", tt.errContains, validationErr.Errors)
				}
			} else {
				require.NoError(t, err)
			}
		})
	}
}
