package destwebhook_test

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_InvalidSigningSecretTemplate(t *testing.T) {
	t.Parallel()

	_, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil,
		destwebhook.WithSigningSecretTemplate(`{{.RandomHex`),
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid signing secret template")
}

func TestNew_ValidSigningSecretTemplate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		template string
	}{
		{
			name:     "default (empty)",
			template: "",
		},
		{
			name:     "hex only",
			template: "{{.RandomHex}}",
		},
		{
			name:     "with prefix",
			template: "whsec_{{.RandomHex}}",
		},
		{
			name:     "base64 variable",
			template: "{{.RandomBase64}}",
		},
		{
			name:     "alphanumeric variable",
			template: "{{.RandomAlphanumeric}}",
		},
		{
			name:     "with sprig function",
			template: `{{.RandomHex | upper}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil,
				destwebhook.WithSigningSecretTemplate(tt.template),
			)
			assert.NoError(t, err)
		})
	}
}

func TestSigningSecretTemplate_DefaultGeneratesHex(t *testing.T) {
	t.Parallel()

	webhookDestination, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil)
	require.NoError(t, err)

	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": "https://example.com",
		}),
	)

	err = webhookDestination.Preprocess(&destination, nil, &destregistry.PreprocessDestinationOpts{Role: "tenant"})
	require.NoError(t, err)

	secret := destination.Credentials["secret"]
	assert.Len(t, secret, 64)
	_, err = hex.DecodeString(secret)
	assert.NoError(t, err, "default template should produce a valid hex string")
}

func TestSigningSecretTemplate_WithPrefix(t *testing.T) {
	t.Parallel()

	webhookDestination, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil,
		destwebhook.WithSigningSecretTemplate("whsec_{{.RandomHex}}"),
	)
	require.NoError(t, err)

	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": "https://example.com",
		}),
	)

	err = webhookDestination.Preprocess(&destination, nil, &destregistry.PreprocessDestinationOpts{Role: "tenant"})
	require.NoError(t, err)

	secret := destination.Credentials["secret"]
	assert.True(t, strings.HasPrefix(secret, "whsec_"), "secret should have whsec_ prefix")
	hexPart := strings.TrimPrefix(secret, "whsec_")
	assert.Len(t, hexPart, 64)
	_, err = hex.DecodeString(hexPart)
	assert.NoError(t, err, "hex part should be valid hex")
}

func TestSigningSecretTemplate_Base64(t *testing.T) {
	t.Parallel()

	webhookDestination, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil,
		destwebhook.WithSigningSecretTemplate("{{.RandomBase64}}"),
	)
	require.NoError(t, err)

	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": "https://example.com",
		}),
	)

	err = webhookDestination.Preprocess(&destination, nil, &destregistry.PreprocessDestinationOpts{Role: "tenant"})
	require.NoError(t, err)

	secret := destination.Credentials["secret"]
	_, err = base64.StdEncoding.DecodeString(secret)
	assert.NoError(t, err, "secret should be valid base64")
}

func TestSigningSecretTemplate_Alphanumeric(t *testing.T) {
	t.Parallel()

	webhookDestination, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil,
		destwebhook.WithSigningSecretTemplate("{{.RandomAlphanumeric}}"),
	)
	require.NoError(t, err)

	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": "https://example.com",
		}),
	)

	err = webhookDestination.Preprocess(&destination, nil, &destregistry.PreprocessDestinationOpts{Role: "tenant"})
	require.NoError(t, err)

	secret := destination.Credentials["secret"]
	assert.Len(t, secret, 32)
	assert.Regexp(t, `^[a-zA-Z0-9]+$`, secret, "secret should be alphanumeric")
}

func TestSigningSecretTemplate_AppliesOnRotation(t *testing.T) {
	t.Parallel()

	webhookDestination, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil,
		destwebhook.WithSigningSecretTemplate("whsec_{{.RandomHex}}"),
	)
	require.NoError(t, err)

	originalDestination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": "https://example.com",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"secret": "old-secret",
		}),
	)

	newDestination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": "https://example.com",
		}),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"rotate_secret": "true",
		}),
	)

	err = webhookDestination.Preprocess(&newDestination, &originalDestination, &destregistry.PreprocessDestinationOpts{Role: "tenant"})
	require.NoError(t, err)

	secret := newDestination.Credentials["secret"]
	assert.True(t, strings.HasPrefix(secret, "whsec_"), "rotated secret should have whsec_ prefix")
	assert.Equal(t, "old-secret", newDestination.Credentials["previous_secret"])
}

func TestSigningSecretTemplate_UniqueSecrets(t *testing.T) {
	t.Parallel()

	webhookDestination, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil,
		destwebhook.WithSigningSecretTemplate("prefix_{{.RandomHex}}"),
	)
	require.NoError(t, err)

	secrets := make(map[string]bool)
	for i := 0; i < 10; i++ {
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com",
			}),
		)

		err := webhookDestination.Preprocess(&destination, nil, &destregistry.PreprocessDestinationOpts{Role: "tenant"})
		require.NoError(t, err)

		secret := destination.Credentials["secret"]
		assert.False(t, secrets[secret], "each generated secret should be unique")
		secrets[secret] = true
	}
}

func TestSigningSecretTemplate_E2ESigningWithTemplatedSecret(t *testing.T) {
	t.Parallel()

	provider, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil,
		destwebhook.WithSigningSecretTemplate("whsec_{{.RandomHex}}"),
	)
	require.NoError(t, err)

	// Generate a secret via Preprocess
	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": "http://example.com/webhook",
		}),
	)
	err = provider.Preprocess(&destination, nil, &destregistry.PreprocessDestinationOpts{Role: "tenant"})
	require.NoError(t, err)

	generatedSecret := destination.Credentials["secret"]
	require.True(t, strings.HasPrefix(generatedSecret, "whsec_"))

	// Create a publisher using the generated secret and verify it produces valid signatures
	publisher, err := provider.CreatePublisher(context.Background(), &destination)
	require.NoError(t, err)
	defer publisher.Close()

	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithDataMap(map[string]interface{}{"key": "value"}),
	)

	req, err := publisher.(*destwebhook.WebhookPublisher).Format(context.Background(), &event)
	require.NoError(t, err)

	signatureHeader := req.Header.Get("x-outpost-signature")
	assert.NotEmpty(t, signatureHeader, "signature header should be present")
	assertSignatureFormat(t, signatureHeader, 1)
	assertValidSignature(t, generatedSecret, []byte(`{"key":"value"}`), signatureHeader)
}

func TestSigningSecretTemplate_StaticTemplate(t *testing.T) {
	t.Parallel()

	provider, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil,
		destwebhook.WithSigningSecretTemplate("my-static-secret"),
	)
	require.NoError(t, err)

	// Generate two secrets — both should be identical
	secrets := make([]string, 2)
	for i := range secrets {
		destination := testutil.DestinationFactory.Any(
			testutil.DestinationFactory.WithType("webhook"),
			testutil.DestinationFactory.WithConfig(map[string]string{
				"url": "https://example.com",
			}),
		)
		err := provider.Preprocess(&destination, nil, &destregistry.PreprocessDestinationOpts{Role: "tenant"})
		require.NoError(t, err)
		secrets[i] = destination.Credentials["secret"]
	}

	assert.Equal(t, "my-static-secret", secrets[0])
	assert.Equal(t, secrets[0], secrets[1], "static template should produce the same secret every time")
}

func TestSigningSecretTemplate_UndefinedVariable(t *testing.T) {
	t.Parallel()

	provider, err := destwebhook.New(testutil.Registry.MetadataLoader(), nil,
		destwebhook.WithSigningSecretTemplate("{{.Foo}}"),
	)
	require.NoError(t, err) // Template parses fine — .Foo is syntactically valid

	destination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithType("webhook"),
		testutil.DestinationFactory.WithConfig(map[string]string{
			"url": "https://example.com",
		}),
	)

	// Should fail at execution time during Preprocess
	err = provider.Preprocess(&destination, nil, &destregistry.PreprocessDestinationOpts{Role: "tenant"})
	assert.Error(t, err, "template with undefined variable should fail during secret generation")
}
