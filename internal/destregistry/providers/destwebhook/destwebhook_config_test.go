package destwebhook_test

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
)

func TestGetEncoder(t *testing.T) {
	tests := []struct {
		name     string
		encoding string
		want     destwebhook.SignatureEncoder
	}{
		{
			name:     "hex encoder (explicit)",
			encoding: "hex",
			want:     destwebhook.HexEncoder{},
		},
		{
			name:     "base64 encoder",
			encoding: "base64",
			want:     destwebhook.Base64Encoder{},
		},
		{
			name:     "default to hex for unknown encoding",
			encoding: "unknown",
			want:     destwebhook.HexEncoder{},
		},
		{
			name:     "default to hex for empty encoding",
			encoding: "",
			want:     destwebhook.HexEncoder{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := destwebhook.GetEncoder(tt.encoding)
			assert.IsType(t, tt.want, got)
		})
	}
}

func TestGetAlgorithm(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
		want      destwebhook.SigningAlgorithm
	}{
		{
			name:      "hmac-sha256 (explicit)",
			algorithm: "hmac-sha256",
			want:      destwebhook.NewHmacSHA256(),
		},
		{
			name:      "hmac-sha1",
			algorithm: "hmac-sha1",
			want:      destwebhook.NewHmacSHA1(),
		},
		{
			name:      "default to hmac-sha256 for unknown algorithm",
			algorithm: "unknown",
			want:      destwebhook.NewHmacSHA256(),
		},
		{
			name:      "default to hmac-sha256 for empty algorithm",
			algorithm: "",
			want:      destwebhook.NewHmacSHA256(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := destwebhook.GetAlgorithm(tt.algorithm)
			assert.Equal(t, tt.want.Name(), got.Name())
		})
	}
}

func TestWebhookDestination_CustomHeadersConfig(t *testing.T) {
	t.Parallel()

	webhookDestination, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil)
	assert.NoError(t, err)

	t.Run("should parse config with valid custom_headers", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url":            "https://example.com/webhook",
				"custom_headers": `{"x-api-key":"secret123","x-tenant-id":"tenant-abc"}`,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "test-secret",
			}),
		)

		err := webhookDestination.Validate(context.Background(), &destination)
		assert.NoError(t, err)
	})

	t.Run("should parse config with empty custom_headers", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url":            "https://example.com/webhook",
				"custom_headers": `{}`,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "test-secret",
			}),
		)

		err := webhookDestination.Validate(context.Background(), &destination)
		assert.NoError(t, err)
	})

	t.Run("should parse config without custom_headers field (backward compatibility)", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com/webhook",
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "test-secret",
			}),
		)

		err := webhookDestination.Validate(context.Background(), &destination)
		assert.NoError(t, err)
	})

	t.Run("should fail on invalid custom_headers JSON", func(t *testing.T) {
		t.Parallel()
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url":            "https://example.com/webhook",
				"custom_headers": `{invalid json}`,
			}),
			testutil.DestinationFactory.WithCredentials(map[string]string{
				"secret": "test-secret",
			}),
		)

		err := webhookDestination.Validate(context.Background(), &destination)
		assert.Error(t, err)
		var validationErr *destregistry.ErrDestinationValidation
		assert.ErrorAs(t, err, &validationErr)
		assert.Equal(t, "config.custom_headers", validationErr.Errors[0].Field)
		assert.Equal(t, "invalid", validationErr.Errors[0].Type)
	})
}

func TestWebhookDestination_SignatureOptions(t *testing.T) {
	tests := []struct {
		name          string
		opts          []destwebhook.Option
		wantEncoding  string
		wantAlgorithm string
	}{
		{
			name:          "default values",
			opts:          []destwebhook.Option{},
			wantEncoding:  destwebhook.DefaultEncoding,
			wantAlgorithm: destwebhook.DefaultAlgorithm,
		},
		{
			name: "custom encoding",
			opts: []destwebhook.Option{
				destwebhook.WithSignatureEncoding("base64"),
			},
			wantEncoding:  "base64",
			wantAlgorithm: destwebhook.DefaultAlgorithm,
		},
		{
			name: "custom algorithm",
			opts: []destwebhook.Option{
				destwebhook.WithSignatureAlgorithm("hmac-sha1"),
			},
			wantEncoding:  destwebhook.DefaultEncoding,
			wantAlgorithm: "hmac-sha1",
		},
		{
			name: "custom encoding and algorithm",
			opts: []destwebhook.Option{
				destwebhook.WithSignatureEncoding("base64"),
				destwebhook.WithSignatureAlgorithm("hmac-sha1"),
			},
			wantEncoding:  "base64",
			wantAlgorithm: "hmac-sha1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil, tt.opts...)
			assert.NoError(t, err)
			assert.Equal(t, tt.wantEncoding, dest.GetSignatureEncoding())
			assert.Equal(t, tt.wantAlgorithm, dest.GetSignatureAlgorithm())
		})
	}
}
