package destwebhook_test

import (
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/stretchr/testify/assert"
)

// defaultSignatureManagerOpts provides the standard formatters for tests that
// don't care about template behavior — they just need a working manager.
func defaultSignatureManagerOpts() []destwebhook.SignatureManagerOption {
	return []destwebhook.SignatureManagerOption{
		destwebhook.WithSignatureFormatter(destwebhook.NewSignatureFormatter(destwebhook.DefaultSignatureContentTmpl)),
		destwebhook.WithHeaderFormatter(destwebhook.NewHeaderFormatter(destwebhook.DefaultSignatureHeaderTmpl)),
	}
}

func TestHmacAlgorithms(t *testing.T) {
	key := "test-secret"
	content := `1234567890.{"hello":"world"}`

	tests := []struct {
		name     string
		algo     destwebhook.SigningAlgorithm
		expected string // hex-encoded signature
	}{
		{
			name:     "hmac-sha256",
			algo:     destwebhook.NewHmacSHA256(),
			expected: "7054f74dae9f73e82b56ca73e8f81450097c698eeda0b00bb8728e89796baf2d",
		},
		{
			name:     "hmac-sha1",
			algo:     destwebhook.NewHmacSHA1(),
			expected: "e2f4423c54f5385099d8e3fbb01237d415ee8fdf",
		},
		{
			name:     "hmac-md5",
			algo:     destwebhook.NewHmacMD5(),
			expected: "aa98470ad83d2d02006b1a67d2c3b4eb",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signature := tt.algo.Sign(key, content, destwebhook.HexEncoder{})
			assert.Equal(t, tt.expected, signature)

			// Basic verification test
			assert.True(t, tt.algo.Verify(key, content, signature, destwebhook.HexEncoder{}))
		})
	}
}

func TestSignatureFormatter(t *testing.T) {
	timestamp := time.Unix(1234567890, 0)
	body := `{"hello":"world"}`

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{
			name:     "body only (new default)",
			template: "{{.Body}}",
			want:     `{"hello":"world"}`,
		},
		{
			name:     "custom template",
			template: "ts={{.Timestamp.Unix}};content={{.Body}}",
			want:     "ts=1234567890;content={\"hello\":\"world\"}",
		},
		{
			name:     "timestamp dot body (legacy format)",
			template: "{{.Timestamp.Unix}}.{{.Body}}",
			want:     "1234567890.{\"hello\":\"world\"}",
		},
		{
			name:     "template with event data",
			template: "ts={{.Timestamp.Unix}};id={{.EventID}};topic={{.Topic}};data={{.Body}}",
			want:     "ts=1234567890;id=test-id;topic=test-topic;data={\"hello\":\"world\"}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			formatter := destwebhook.NewSignatureFormatter(tt.template)
			result := formatter.Format(destwebhook.SignaturePayload{
				Timestamp: timestamp,
				Body:      body,
				EventID:   "test-id",
				Topic:     "test-topic",
			})
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestSignatureFormatter_PanicsOnEmpty(t *testing.T) {
	assert.Panics(t, func() {
		destwebhook.NewSignatureFormatter("")
	})
}

func TestSignatureFormatter_PanicsOnInvalidSyntax(t *testing.T) {
	assert.Panics(t, func() {
		destwebhook.NewSignatureFormatter("{{.Timestamp.{{.Body}}")
	})
}

func TestHeaderFormatter(t *testing.T) {
	timestamp := time.Unix(1234567890, 0)
	signatures := []string{"abc123", "def456"}

	tests := []struct {
		name     string
		template string
		want     string
	}{
		{
			name:     "v0 only (new default)",
			template: `v0={{.Signatures | join ","}}`,
			want:     "v0=abc123,def456",
		},
		{
			name:     "custom template",
			template: `timestamp={{.Timestamp.Unix}};signatures={{.Signatures | join ","}}`,
			want:     "timestamp=1234567890;signatures=abc123,def456",
		},
		{
			name:     "t= prefix (legacy format)",
			template: `t={{.Timestamp.Unix}},v0={{.Signatures | join ","}}`,
			want:     "t=1234567890,v0=abc123,def456",
		},
		{
			name:     "template with event data",
			template: `t={{.Timestamp.Unix}},id={{.EventID}},topic={{.Topic}},v0={{.Signatures | join ","}}`,
			want:     "t=1234567890,id=test-id,topic=test-topic,v0=abc123,def456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			formatter := destwebhook.NewHeaderFormatter(tt.template)
			result := formatter.Format(destwebhook.HeaderPayload{
				Timestamp:  timestamp,
				Signatures: signatures,
				EventID:    "test-id",
				Topic:      "test-topic",
			})
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestHeaderFormatter_PanicsOnEmpty(t *testing.T) {
	assert.Panics(t, func() {
		destwebhook.NewHeaderFormatter("")
	})
}

func TestHeaderFormatter_PanicsOnInvalidSyntax(t *testing.T) {
	assert.Panics(t, func() {
		destwebhook.NewHeaderFormatter("t={{.Timestamp},v0={{.Signatures}")
	})
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
		manager := destwebhook.NewSignatureManager(nil, defaultSignatureManagerOpts()...)
		signatures := manager.GenerateSignatures(destwebhook.SignaturePayload{
			Timestamp: time.Now(),
			Body:      "test",
		})
		assert.Nil(t, signatures)

		header := manager.GenerateSignatureHeader(destwebhook.SignaturePayload{
			Timestamp: time.Now(),
			Body:      "test",
		})
		assert.Empty(t, header)
	})

	t.Run("single old secret", func(t *testing.T) {
		oldSecret := destwebhook.WebhookSecret{
			Key:       "old_secret",
			CreatedAt: time.Now().Add(-48 * time.Hour), // 48 hours old
		}
		body := "test"
		timestamp := time.Now()
		payload := destwebhook.SignaturePayload{
			Timestamp: timestamp,
			Body:      body,
			EventID:   "test-id",
			Topic:     "test-topic",
		}

		manager := destwebhook.NewSignatureManager([]destwebhook.WebhookSecret{oldSecret}, defaultSignatureManagerOpts()...)
		signatures := manager.GenerateSignatures(payload)
		assert.Len(t, signatures, 1, "should generate signature for single secret regardless of age")

		// Verify signature is valid with correct key
		assert.True(t, manager.VerifySignature(
			signatures[0],
			oldSecret.Key,
			payload,
		), "signature should be valid with correct key")
	})

	t.Run("latest secret priority", func(t *testing.T) {
		now := time.Now()
		secrets := []destwebhook.WebhookSecret{
			{Key: "oldest", CreatedAt: now.Add(-96 * time.Hour)},
			{Key: "older", CreatedAt: now.Add(-72 * time.Hour)},
			{Key: "latest", CreatedAt: now.Add(-48 * time.Hour)}, // Old but latest
		}
		body := "test"
		timestamp := time.Now()
		payload := destwebhook.SignaturePayload{
			Timestamp: timestamp,
			Body:      body,
			EventID:   "test-id",
			Topic:     "test-topic",
		}

		manager := destwebhook.NewSignatureManager(secrets, defaultSignatureManagerOpts()...)
		signatures := manager.GenerateSignatures(payload)
		assert.Len(t, signatures, 1, "should only use latest secret")

		// Verify signature is valid with latest key
		assert.True(t, manager.VerifySignature(
			signatures[0],
			"latest",
			payload,
		), "signature should be valid with latest key")

		// Verify signature is invalid with older keys
		assert.False(t, manager.VerifySignature(
			signatures[0],
			"older",
			payload,
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

		manager := destwebhook.NewSignatureManager(secrets, defaultSignatureManagerOpts()...)
		timestamp := time.Unix(1234567890, 0)
		body := `{"hello":"world"}`

		signatures := manager.GenerateSignatures(destwebhook.SignaturePayload{
			Timestamp: timestamp,
			Body:      body,
			EventID:   "test-id",
			Topic:     "test-topic",
		})
		assert.Len(t, signatures, 3, "should include latest + 2 recent secrets")

		// Verify each signature is valid with its corresponding key
		validKeys := []string{"latest", "recent1", "recent2"}
		for i, sig := range signatures {
			assert.True(t, manager.VerifySignature(
				sig,
				validKeys[i],
				destwebhook.SignaturePayload{
					Timestamp: timestamp,
					Body:      body,
					EventID:   "test-id",
					Topic:     "test-topic",
				},
			), "signature should be valid with its corresponding key")
		}

		// Verify signature is invalid with expired key
		assert.False(t, manager.VerifySignature(
			signatures[0],
			"expired",
			destwebhook.SignaturePayload{
				Timestamp: timestamp,
				Body:      body,
				EventID:   "test-id",
				Topic:     "test-topic",
			},
		), "signature should be invalid with expired key")

		header := manager.GenerateSignatureHeader(destwebhook.SignaturePayload{
			Timestamp: timestamp,
			Body:      body,
			EventID:   "test-id",
			Topic:     "test-topic",
		})
		assert.True(t, strings.HasPrefix(header, "v0="), "header should start with v0=")
		assert.Equal(t, 2, strings.Count(header, ","), "should have correct number of commas in header")
	})

	t.Run("custom invalidation time", func(t *testing.T) {
		now := time.Now()
		invalidAt := now.Add(-1 * time.Hour)       // Invalidated 1 hour ago
		futureInvalidAt := now.Add(12 * time.Hour) // Will be invalid in 12 hours

		secrets := []destwebhook.WebhookSecret{
			{Key: "latest", CreatedAt: now},
			{Key: "valid_custom", CreatedAt: now.Add(-12 * time.Hour), InvalidAt: &futureInvalidAt},
			{Key: "invalid_custom", CreatedAt: now.Add(-12 * time.Hour), InvalidAt: &invalidAt},
			{Key: "valid_default", CreatedAt: now.Add(-12 * time.Hour)},
		}

		manager := destwebhook.NewSignatureManager(secrets, defaultSignatureManagerOpts()...)
		timestamp := time.Unix(1234567890, 0)
		body := `{"hello":"world"}`
		payload := destwebhook.SignaturePayload{
			Timestamp: timestamp,
			Body:      body,
			EventID:   "test-id",
			Topic:     "test-topic",
		}

		signatures := manager.GenerateSignatures(payload)
		assert.Len(t, signatures, 3, "should include latest + valid secrets")

		// Verify each signature is valid with its corresponding key
		validKeys := []string{"latest", "valid_custom", "valid_default"}
		for i, sig := range signatures {
			assert.True(t, manager.VerifySignature(
				sig,
				validKeys[i],
				payload,
			), "signature should be valid with its corresponding key")
		}

		// Verify signature is invalid with manually invalidated key
		assert.False(t, manager.VerifySignature(
			signatures[0],
			"invalid_custom",
			payload,
		), "signature should be invalid with manually invalidated key")
	})

	t.Run("invalid latest secret", func(t *testing.T) {
		now := time.Now()
		invalidAt := now.Add(-1 * time.Hour) // Invalidated 1 hour ago

		t.Run("with no other valid secrets", func(t *testing.T) {
			secrets := []destwebhook.WebhookSecret{
				{Key: "latest", CreatedAt: now, InvalidAt: &invalidAt},
				{Key: "old1", CreatedAt: now.Add(-25 * time.Hour)}, // Past 24h window
				{Key: "old2", CreatedAt: now.Add(-26 * time.Hour)}, // Past 24h window
			}

			manager := destwebhook.NewSignatureManager(secrets, defaultSignatureManagerOpts()...)
			signatures := manager.GenerateSignatures(destwebhook.SignaturePayload{
				Timestamp: time.Unix(1234567890, 0),
				Body:      "test",
				EventID:   "test-id",
				Topic:     "test-topic",
			})
			assert.Empty(t, signatures, "should return empty signatures when latest is invalid and no other valid secrets")
		})

		t.Run("with other valid secrets", func(t *testing.T) {
			secrets := []destwebhook.WebhookSecret{
				{Key: "latest", CreatedAt: now, InvalidAt: &invalidAt},
				{Key: "recent", CreatedAt: now.Add(-12 * time.Hour)}, // Within 24h window
				{Key: "old", CreatedAt: now.Add(-25 * time.Hour)},    // Past 24h window
			}

			manager := destwebhook.NewSignatureManager(secrets, defaultSignatureManagerOpts()...)
			signatures := manager.GenerateSignatures(destwebhook.SignaturePayload{
				Timestamp: time.Unix(1234567890, 0),
				Body:      "test",
				EventID:   "test-id",
				Topic:     "test-topic",
			})
			assert.Len(t, signatures, 1, "should only include valid non-latest secrets")

			// Verify signature is valid with the recent key
			assert.True(t, manager.VerifySignature(
				signatures[0],
				"recent",
				destwebhook.SignaturePayload{
					Timestamp: time.Unix(1234567890, 0),
					Body:      "test",
					EventID:   "test-id",
					Topic:     "test-topic",
				},
			), "signature should be valid with recent key")
		})
	})
}
