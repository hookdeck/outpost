package destwebhook_test

import (
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/stretchr/testify/assert"
)

func TestHmacSHA256_Sign(t *testing.T) {
	algo := destwebhook.HmacSHA256{}
	key := "test-secret"
	content := `1234567890.{"hello":"world"}`

	signature := algo.Sign(key, content, destwebhook.HexEncoder{})
	// Pre-computed expected signature for the given inputs
	expected := "7054f74dae9f73e82b56ca73e8f81450097c698eeda0b00bb8728e89796baf2d"

	assert.Equal(t, expected, signature)
}

func TestDefaultSignatureFormatter(t *testing.T) {
	formatter := destwebhook.DefaultSignatureFormatter{}
	timestamp := time.Unix(1234567890, 0)
	body := []byte(`{"hello":"world"}`)

	result := formatter.Format(timestamp, body)
	expected := "1234567890.{\"hello\":\"world\"}"

	assert.Equal(t, expected, result)
}

func TestDefaultHeaderFormatter(t *testing.T) {
	formatter := destwebhook.DefaultHeaderFormatter{}
	timestamp := time.Unix(1234567890, 0)
	signatures := []string{"abc123", "def456"}

	result := formatter.Format(timestamp, signatures)
	expected := "t=1234567890,v0=abc123,def456"

	assert.Equal(t, expected, result)
}

func TestSignatureEncoders(t *testing.T) {
	tests := []struct {
		name     string
		encoder  destwebhook.SignatureEncoder
		input    []byte
		expected string
	}{
		{
			name:     "hex encoder",
			encoder:  destwebhook.HexEncoder{},
			input:    []byte("test123"),
			expected: "74657374313233", // hex representation of "test123"
		},
		{
			name:     "base64 encoder",
			encoder:  destwebhook.Base64Encoder{},
			input:    []byte("test123"),
			expected: "dGVzdDEyMw==", // base64 representation of "test123"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.encoder.Encode(tt.input)
			if result != tt.expected {
				t.Errorf("got %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestSignatureManager(t *testing.T) {
	t.Run("no secrets", func(t *testing.T) {
		manager := destwebhook.NewSignatureManager(nil)
		signatures := manager.GenerateSignatures(time.Now(), []byte("test"))
		assert.Nil(t, signatures)

		header := manager.GenerateSignatureHeader(time.Now(), []byte("test"))
		assert.Empty(t, header)
	})

	t.Run("single old secret", func(t *testing.T) {
		oldSecret := destwebhook.WebhookSecret{
			Key:       "old_secret",
			CreatedAt: time.Now().Add(-48 * time.Hour), // 48 hours old
		}
		body := []byte("test")
		timestamp := time.Now()

		manager := destwebhook.NewSignatureManager([]destwebhook.WebhookSecret{oldSecret})
		signatures := manager.GenerateSignatures(timestamp, body)
		assert.Len(t, signatures, 1, "should generate signature for single secret regardless of age")

		// Verify signature is valid with correct key
		assert.True(t, manager.VerifySignature(
			signatures[0],
			oldSecret.Key,
			timestamp,
			body,
		), "signature should be valid with correct key")
	})

	t.Run("latest secret priority", func(t *testing.T) {
		now := time.Now()
		secrets := []destwebhook.WebhookSecret{
			{Key: "oldest", CreatedAt: now.Add(-96 * time.Hour)},
			{Key: "older", CreatedAt: now.Add(-72 * time.Hour)},
			{Key: "latest", CreatedAt: now.Add(-48 * time.Hour)}, // Old but latest
		}
		body := []byte("test")
		timestamp := time.Now()

		manager := destwebhook.NewSignatureManager(secrets)
		signatures := manager.GenerateSignatures(timestamp, body)
		assert.Len(t, signatures, 1, "should only use latest secret")

		// Verify signature is valid with latest key
		assert.True(t, manager.VerifySignature(
			signatures[0],
			"latest",
			timestamp,
			body,
		), "signature should be valid with latest key")

		// Verify signature is invalid with older keys
		assert.False(t, manager.VerifySignature(
			signatures[0],
			"older",
			timestamp,
			body,
		), "signature should be invalid with older key")
	})

	t.Run("multiple valid secrets", func(t *testing.T) {
		now := time.Now()
		secrets := []destwebhook.WebhookSecret{
			{Key: "latest", CreatedAt: now},
			{Key: "recent1", CreatedAt: now.Add(-12 * time.Hour)},
			{Key: "recent2", CreatedAt: now.Add(-20 * time.Hour)},
			{Key: "expired", CreatedAt: now.Add(-25 * time.Hour)},
		}

		manager := destwebhook.NewSignatureManager(secrets)
		timestamp := time.Unix(1234567890, 0)
		body := []byte(`{"hello":"world"}`)

		signatures := manager.GenerateSignatures(timestamp, body)
		assert.Len(t, signatures, 3, "should include latest + 2 recent secrets")

		// Verify each signature is valid with its corresponding key
		validKeys := []string{"latest", "recent1", "recent2"}
		for i, sig := range signatures {
			assert.True(t, manager.VerifySignature(
				sig,
				validKeys[i],
				timestamp,
				body,
			), "signature should be valid with its corresponding key")
		}

		// Verify signature is invalid with expired key
		assert.False(t, manager.VerifySignature(
			signatures[0],
			"expired",
			timestamp,
			body,
		), "signature should be invalid with expired key")

		header := manager.GenerateSignatureHeader(timestamp, body)
		assert.Contains(t, header, "t=1234567890")
		assert.Equal(t, 3, strings.Count(header, ","), "should have correct number of commas in header")
	})
}
