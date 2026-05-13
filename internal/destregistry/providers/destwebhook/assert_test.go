package destwebhook_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"strings"

	testsuite "github.com/hookdeck/outpost/internal/destregistry/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to assert signature format
// Expected format: "v0={sig1,sig2,...}"
func assertSignatureFormat(t testsuite.TestingT, signatureHeader string, expectedSignatureCount int) {
	t.Helper()

	require.True(t, strings.HasPrefix(signatureHeader, "v0="), "signature header should start with v0=")
	signatures := strings.Split(strings.TrimPrefix(signatureHeader, "v0="), ",")
	assert.Len(t, signatures, expectedSignatureCount, "should have exact number of signatures")
}

// Helper function to assert valid signature
// Signed content is the raw body (no timestamp prefix)
func assertValidSignature(t testsuite.TestingT, secret string, rawBody []byte, signatureHeader string) {
	t.Helper()

	require.True(t, strings.HasPrefix(signatureHeader, "v0="), "signature header should start with v0=")
	signatures := strings.Split(strings.TrimPrefix(signatureHeader, "v0="), ",")

	// Signed content is just the body
	signedContent := string(rawBody)

	// Generate HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signedContent))
	expectedSignature := fmt.Sprintf("%x", mac.Sum(nil))

	// Check if any of the signatures match
	found := false
	for _, sig := range signatures {
		if sig == expectedSignature {
			found = true
			break
		}
	}
	assert.True(t, found, "none of the signatures matched expected value")
}
