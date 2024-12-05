package destwebhook_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"

	testsuite "github.com/hookdeck/outpost/internal/destregistry/testing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper function to assert webhook request content
func assertRequestContent(t *testing.T, rawBody []byte, expectedData map[string]interface{}, expectedMetadata map[string]string, headerPrefix string, request *http.Request) {
	t.Helper()

	// Verify body content
	var actualBody map[string]interface{}
	err := json.Unmarshal(rawBody, &actualBody)
	require.NoError(t, err, "body should be valid JSON")
	assert.Equal(t, expectedData, actualBody, "request body should match expected data")

	// Verify metadata in headers
	for key, value := range expectedMetadata {
		assert.Equal(t, value, request.Header.Get(headerPrefix+key),
			"metadata header %s should match expected value", key)
	}
}

// Helper function to assert signature format
func assertSignatureFormat(t testsuite.TestingT, signatureHeader string, expectedSignatureCount int) {
	t.Helper()

	parts := strings.SplitN(signatureHeader, ",", 2)
	require.True(t, len(parts) >= 2, "signature header should have timestamp and signature parts")

	// Verify timestamp format
	assert.True(t, strings.HasPrefix(parts[0], "t="), "should start with t=")
	timestampStr := strings.TrimPrefix(parts[0], "t=")
	_, err := strconv.ParseInt(timestampStr, 10, 64)
	require.NoError(t, err, "timestamp should be a valid integer")

	// Verify signature format and count
	assert.True(t, strings.HasPrefix(parts[1], "v0="), "should start with v0=")
	signatures := strings.Split(strings.TrimPrefix(parts[1], "v0="), ",")
	assert.Len(t, signatures, expectedSignatureCount, "should have exact number of signatures")
}

// Helper function to assert valid signature
func assertValidSignature(t testsuite.TestingT, secret string, rawBody []byte, signatureHeader string) {
	t.Helper()

	// Parse "t={timestamp},v0={signature1,signature2}" format
	parts := strings.SplitN(signatureHeader, ",", 2) // Split only on first comma
	require.True(t, len(parts) >= 2, "signature header should have timestamp and signature parts")

	timestampStr := strings.TrimPrefix(parts[0], "t=")
	signatures := strings.Split(strings.TrimPrefix(parts[1], "v0="), ",")

	timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
	require.NoError(t, err, "timestamp should be a valid integer")

	// Reconstruct the signed content
	signedContent := fmt.Sprintf("%d.%s", timestamp, rawBody)

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
