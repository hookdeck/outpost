package mcpevents

import (
	"encoding/base64"
	"errors"
	"strings"
)

const (
	SecretPrefix   = "whsec_"
	MinSecretBytes = 24
	MaxSecretBytes = 64
)

var errInvalidSecret = errors.New("mcpevents: secret must be whsec_ followed by base64 of 24 to 64 bytes")

func DecodeSecret(secret string) ([]byte, error) {
	encoded, ok := strings.CutPrefix(secret, SecretPrefix)
	if !ok {
		return nil, errInvalidSecret
	}
	data := strings.TrimRight(encoded, "=")
	if padding := len(encoded) - len(data); padding > 2 || (padding > 0 && len(encoded)%4 != 0) {
		return nil, errInvalidSecret
	}
	for i := 0; i < len(data); i++ {
		c := data[i]
		if !('A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || '0' <= c && c <= '9' || c == '+' || c == '/') {
			return nil, errInvalidSecret
		}
	}
	key, err := base64.RawStdEncoding.DecodeString(data)
	if err != nil || len(key) < MinSecretBytes || len(key) > MaxSecretBytes {
		return nil, errInvalidSecret
	}
	return key, nil
}
