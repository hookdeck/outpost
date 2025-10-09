package destwebhookstandard_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strings"

	testsuite "github.com/hookdeck/outpost/internal/destregistry/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertValidStandardWebhookSignature verifies a Standard Webhooks signature
func assertValidStandardWebhookSignature(t testsuite.TestingT, secret, msgID, timestamp string, body []byte, signatureHeader string) {
	t.Helper()

	// Parse whsec_ secret
	encodedPart := strings.TrimPrefix(secret, "whsec_")
	decodedSecret, err := base64.StdEncoding.DecodeString(encodedPart)
	require.NoError(t, err, "secret should decode successfully")

	// Construct signed content: msg_id.timestamp.body
	signedContent := fmt.Sprintf("%s.%s.%s", msgID, timestamp, string(body))

	// Generate expected signature
	mac := hmac.New(sha256.New, decodedSecret)
	mac.Write([]byte(signedContent))
	expectedSig := base64.StdEncoding.EncodeToString(mac.Sum(nil))

	// Check if any signature in header matches
	signatures := strings.Split(signatureHeader, " ")
	found := false
	for _, sig := range signatures {
		sigPart := strings.TrimPrefix(sig, "v1,")
		if hmac.Equal([]byte(sigPart), []byte(expectedSig)) {
			found = true
			break
		}
	}

	assert.True(t, found, "no valid signature found in header")
}

// assertSignatureFormat verifies the signature header format
func assertSignatureFormat(t testsuite.TestingT, signatureHeader string, expectedCount int) {
	t.Helper()

	signatures := strings.Split(signatureHeader, " ")
	assert.Equal(t, expectedCount, len(signatures), "signature count mismatch")

	for i, sig := range signatures {
		assert.True(t, strings.HasPrefix(sig, "v1,"), "signature %d should have v1, prefix", i)

		// Verify it's valid base64
		sigPart := strings.TrimPrefix(sig, "v1,")
		_, err := base64.StdEncoding.DecodeString(sigPart)
		assert.NoError(t, err, "signature %d should be valid base64", i)
	}
}
