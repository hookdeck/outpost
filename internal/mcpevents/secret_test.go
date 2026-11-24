package mcpevents

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeSecret(t *testing.T) {
	enc := func(n int) string {
		return SecretPrefix + base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{7}, n))
	}
	tests := []struct {
		name   string
		secret string
		ok     bool
	}{
		{"24 bytes", enc(24), true},
		{"32 bytes", enc(32), true},
		{"64 bytes", enc(64), true},
		{"unpadded", strings.TrimRight(enc(32), "="), true},
		{"23 bytes", enc(23), false},
		{"65 bytes", enc(65), false},
		{"no prefix", strings.TrimPrefix(enc(32), SecretPrefix), false},
		{"url-safe alphabet", SecretPrefix + strings.NewReplacer("+", "-", "/", "_").Replace(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0xfb, 0xff}, 16))), false},
		{"line break", enc(32)[:10] + "\n" + enc(32)[10:], false},
		{"too much padding", enc(32) + "==", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, err := DecodeSecret(tt.secret)
			if tt.ok {
				require.NoError(t, err)
				require.NotEmpty(t, key)
			} else {
				require.Error(t, err)
			}
		})
	}
}
