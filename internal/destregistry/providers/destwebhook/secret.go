package destwebhook

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	SecretEncodingRaw    = "raw"
	SecretEncodingBase64 = "base64"
	SecretEncodingHex    = "hex"
)

// WithSecretEncoding sets how the primary signature derives its HMAC key from
// the stored secret. See decodeSecretKey.
func WithSecretEncoding(encoding, prefix string) Option {
	return func(w *WebhookDestination) {
		w.secretEncoding = encoding
		w.secretPrefix = prefix
	}
}

func validateSecretEncoding(encoding string) error {
	switch encoding {
	case "", SecretEncodingRaw, SecretEncodingBase64, SecretEncodingHex:
		return nil
	default:
		return fmt.Errorf("invalid secret encoding %q: must be one of raw, base64, hex", encoding)
	}
}

// decodeSecretKey derives the HMAC key from a stored secret: the prefix is
// stripped when present, then the remainder is decoded. Raw uses the secret
// string as-is, prefix included.
func decodeSecretKey(secret, encoding, prefix string) (string, error) {
	switch encoding {
	case "", SecretEncodingRaw:
		return secret, nil
	case SecretEncodingBase64:
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, prefix))
		if err != nil {
			return "", fmt.Errorf("secret is not valid base64: %w", err)
		}
		return string(decoded), nil
	case SecretEncodingHex:
		decoded, err := hex.DecodeString(strings.TrimPrefix(secret, prefix))
		if err != nil {
			return "", fmt.Errorf("secret is not valid hex: %w", err)
		}
		return string(decoded), nil
	default:
		return "", validateSecretEncoding(encoding)
	}
}

func decodeSecretKeys(secrets []WebhookSecret, encoding, prefix string) ([]WebhookSecret, error) {
	decoded := make([]WebhookSecret, len(secrets))
	for i, secret := range secrets {
		key, err := decodeSecretKey(secret.Key, encoding, prefix)
		if err != nil {
			return nil, err
		}
		decoded[i] = secret
		decoded[i].Key = key
	}
	return decoded, nil
}
