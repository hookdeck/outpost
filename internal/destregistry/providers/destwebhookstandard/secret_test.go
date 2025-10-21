package destwebhookstandard

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateSecret(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		secret  string
		wantErr bool
	}{
		{
			name:    "valid whsec secret",
			secret:  "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
			wantErr: false,
		},
		{
			name:    "missing whsec prefix",
			secret:  "MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
			wantErr: true,
		},
		{
			name:    "empty after prefix",
			secret:  "whsec_",
			wantErr: true,
		},
		{
			name:    "invalid base64",
			secret:  "whsec_not-valid-base64!!!",
			wantErr: true,
		},
		{
			name:    "empty string",
			secret:  "",
			wantErr: true,
		},
		{
			name:    "wrong prefix",
			secret:  "whsk_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := validateSecret(tt.secret)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestParseSecret(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		secret  string
		wantErr bool
	}{
		{
			name:    "valid whsec secret",
			secret:  "whsec_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
			wantErr: false,
		},
		{
			name:    "invalid prefix",
			secret:  "invalid_MfKQ9r8GKYqrTwjUPD8ILPZIo2LaLaSw",
			wantErr: true,
		},
		{
			name:    "invalid base64",
			secret:  "whsec_not-valid-base64!!!",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := parseSecret(tt.secret)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Empty(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, result)

				// Verify that the result is the decoded version
				encodedPart := strings.TrimPrefix(tt.secret, SecretPrefix)
				decoded, _ := base64.StdEncoding.DecodeString(encodedPart)
				assert.Equal(t, string(decoded), result)
			}
		})
	}
}

func TestGenerateStandardSecret(t *testing.T) {
	t.Parallel()

	t.Run("generates valid whsec secret", func(t *testing.T) {
		t.Parallel()
		secret, err := generateStandardSecret()
		require.NoError(t, err)

		// Should have whsec_ prefix
		assert.True(t, strings.HasPrefix(secret, SecretPrefix))

		// Should be valid base64 after prefix
		err = validateSecret(secret)
		assert.NoError(t, err)

		// Should decode to 32 bytes
		encodedPart := strings.TrimPrefix(secret, SecretPrefix)
		decoded, err := base64.StdEncoding.DecodeString(encodedPart)
		require.NoError(t, err)
		assert.Equal(t, SecretLength, len(decoded))
	})

	t.Run("generates unique secrets", func(t *testing.T) {
		t.Parallel()
		secret1, err := generateStandardSecret()
		require.NoError(t, err)

		secret2, err := generateStandardSecret()
		require.NoError(t, err)

		// Should generate different secrets
		assert.NotEqual(t, secret1, secret2)
	})
}
