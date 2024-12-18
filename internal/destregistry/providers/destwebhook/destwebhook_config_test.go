package destwebhook_test

import (
	"testing"

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
			dest, err := destwebhook.New(testutil.Registry.MetadataLoader(), tt.opts...)
			assert.NoError(t, err)
			assert.Equal(t, tt.wantEncoding, dest.GetSignatureEncoding())
			assert.Equal(t, tt.wantAlgorithm, dest.GetSignatureAlgorithm())
		})
	}
}
