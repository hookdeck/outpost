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

	signature := algo.Sign(key, content)
	// Pre-computed expected signature for the given inputs
	expected := "7054f74dae9f73e82b56ca73e8f81450097c698eeda0b00bb8728e89796baf2d"

	assert.Equal(t, expected, signature)
}

func TestDefaultSignatureFormatter_Format(t *testing.T) {
	formatter := destwebhook.DefaultSignatureFormatter{}
	timestamp := time.Unix(1234567890, 0)
	body := []byte(`{"hello":"world"}`)

	result := formatter.Format(timestamp, body)
	expected := "1234567890.{\"hello\":\"world\"}"

	assert.Equal(t, expected, result)
}

func TestDefaultHeaderFormatter_Format(t *testing.T) {
	formatter := destwebhook.DefaultHeaderFormatter{}
	timestamp := time.Unix(1234567890, 0)
	signatures := []string{"abc123", "def456"}

	result := formatter.FormatHeader(timestamp, signatures)
	expected := "t=1234567890,v0=abc123,def456"

	assert.Equal(t, expected, result)
}

func TestSignatureManager_GenerateSignatures(t *testing.T) {
	now := time.Now()
	secrets := []destwebhook.WebhookSecret{
		{Key: "latest", CreatedAt: now},
		{Key: "old-valid", CreatedAt: now.Add(-23 * time.Hour)},
		{Key: "expired", CreatedAt: now.Add(-25 * time.Hour)},
	}

	manager := destwebhook.NewSignatureManager(secrets)
	timestamp := time.Unix(1234567890, 0)
	body := []byte(`{"hello":"world"}`)

	signatures := manager.GenerateSignatures(timestamp, body)

	// Should contain timestamp and at least 2 signatures (latest and old-valid)
	assert.Len(t, signatures, 2)
}

func TestSignatureManager_GenerateSignatureHeader(t *testing.T) {
	now := time.Now()
	secrets := []destwebhook.WebhookSecret{
		{Key: "latest", CreatedAt: now},
		{Key: "old-valid", CreatedAt: now.Add(-23 * time.Hour)},
	}

	manager := destwebhook.NewSignatureManager(secrets)
	timestamp := time.Unix(1234567890, 0)
	body := []byte(`{"hello":"world"}`)

	header := manager.GenerateSignatureHeader(timestamp, body)

	assert.Contains(t, header, "t=1234567890")
	assert.Contains(t, header, "v0=")
	assert.Contains(t, header, "v0=")
	assert.Contains(t, header, ",")
}

func TestSignatureManager_NoSecrets(t *testing.T) {
	manager := destwebhook.NewSignatureManager(nil)
	signatures := manager.GenerateSignatures(time.Now(), []byte("test"))
	assert.Nil(t, signatures)

	header := manager.GenerateSignatureHeader(time.Now(), []byte("test"))
	assert.Empty(t, header)
}

func TestSignatureManager_MultipleValidSecrets(t *testing.T) {
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
	assert.Len(t, signatures, 3) // Should include latest + 2 recent secrets

	header := manager.GenerateSignatureHeader(timestamp, body)
	assert.Contains(t, header, "t=1234567890")
	// For 3 signatures, we expect:
	// - 1 comma between t= and v0=
	// - 2 commas between signatures
	assert.Equal(t, 3, strings.Count(header, ","))
}
