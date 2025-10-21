package destwebhookstandard

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
)

const (
	SecretPrefix = "whsec_"
	SecretLength = 32 // 32 bytes = 256 bits
)

// validateSecret checks if a secret has the correct whsec_ prefix and valid base64 encoding
func validateSecret(secret string) error {
	if !strings.HasPrefix(secret, SecretPrefix) {
		return fmt.Errorf("secret must have %s prefix", SecretPrefix)
	}

	encodedPart := strings.TrimPrefix(secret, SecretPrefix)
	if encodedPart == "" {
		return fmt.Errorf("secret is empty after prefix")
	}

	if _, err := base64.StdEncoding.DecodeString(encodedPart); err != nil {
		return fmt.Errorf("secret is not valid base64: %w", err)
	}

	return nil
}

// parseSecret extracts and decodes the secret portion after whsec_ prefix
// Returns the decoded bytes as a string for use with SignatureManager
func parseSecret(secret string) (string, error) {
	if err := validateSecret(secret); err != nil {
		return "", err
	}

	encodedPart := strings.TrimPrefix(secret, SecretPrefix)
	decoded, err := base64.StdEncoding.DecodeString(encodedPart)
	if err != nil {
		return "", fmt.Errorf("failed to decode secret: %w", err)
	}

	// Return as string - SignatureManager will convert to []byte for HMAC
	return string(decoded), nil
}

// generateStandardSecret creates a cryptographically secure random secret in Standard Webhooks format
// Format: whsec_<base64_encoded_32_bytes>
func generateStandardSecret() (string, error) {
	// Generate 32 random bytes (256 bits)
	randomBytes := make([]byte, SecretLength)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate random secret: %w", err)
	}

	// Encode and prefix
	encoded := base64.StdEncoding.EncodeToString(randomBytes)
	return SecretPrefix + encoded, nil
}
