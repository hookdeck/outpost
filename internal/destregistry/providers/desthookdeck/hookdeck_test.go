package desthookdeck_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry/providers/desthookdeck"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// verifySignature validates that the given signature is a valid HMAC SHA256 signature
// It reads the request body, verifies the signature, and returns the body for further use
func verifySignature(t *testing.T, req *http.Request, signingKey string) ([]byte, bool) {
	t.Helper()

	// Read the body for verification
	body, err := io.ReadAll(req.Body)
	require.NoError(t, err)

	// Get signature from header
	signature := req.Header.Get("X-Hookdeck-Signature")
	if signature == "" {
		return body, false
	}

	// Signature should start with "v0="
	if !strings.HasPrefix(signature, "v0=") {
		return body, false
	}

	// Extract the actual signature value (remove "v0=" prefix)
	actualSignature := signature[3:]

	// Calculate expected signature
	h := hmac.New(sha256.New, []byte(signingKey))
	h.Write(body)
	expectedSignature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	return body, actualSignature == expectedSignature
}

func TestParseHookdeckToken(t *testing.T) {
	// Test cases
	tests := []struct {
		name        string
		token       string
		expectError bool
		expectedID  string
		expectedErr error
	}{
		{
			name:        "Valid token",
			token:       base64.StdEncoding.EncodeToString([]byte("123:random_string")),
			expectError: false,
			expectedID:  "123",
		},
		{
			name:        "Invalid Base64",
			token:       "not-base64",
			expectError: true,
			expectedErr: desthookdeck.ErrInvalidTokenBase64,
		},
		{
			name:        "Invalid format - no colons",
			token:       base64.StdEncoding.EncodeToString([]byte("nocolons")),
			expectError: true,
			expectedErr: desthookdeck.ErrInvalidTokenFormat,
		},
		{
			name:        "Extra colons are included in random string part",
			token:       base64.StdEncoding.EncodeToString([]byte("123:extra:colons")),
			expectError: false,
			expectedID:  "123",
		},
	}

	// Run tests
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := desthookdeck.ParseHookdeckToken(tc.token)

			if tc.expectError {
				require.Error(t, err)
				if tc.expectedErr != nil {
					assert.ErrorIs(t, err, tc.expectedErr)
				}
			} else {
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, tc.expectedID, result.ID)
				// The signing key should be the full original token
				assert.Equal(t, tc.token, result.SigningKey)
			}
		})
	}
}

func TestCreateRequest(t *testing.T) {
	// Set up test data
	tokenStr := base64.StdEncoding.EncodeToString([]byte("abc123:signing-key"))
	token, err := desthookdeck.ParseHookdeckToken(tokenStr)
	require.NoError(t, err)

	// Create an event using EventFactory with custom data and metadata
	event := testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithID("event123"),
		testutil.EventFactory.WithTopic("order.created"),
		testutil.EventFactory.WithData(map[string]interface{}{
			"order_id": "1234567890",
			"amount":   100.50,
			"currency": "USD",
		}),
		testutil.EventFactory.WithMetadata(map[string]string{
			"correlation-id": "corr123",
			"tenant-id":      "tenant456",
		}),
	)

	// Create metadata map
	metadata := map[string]string{
		"timestamp":      "1234567890",
		"event-id":       event.ID,
		"topic":          event.Topic,
		"correlation-id": "corr123",
		"tenant-id":      "tenant456",
	}

	// Create the request
	req, err := desthookdeck.CreateRequest(token, event, metadata)
	require.NoError(t, err)

	// Test URL
	assert.Equal(t, "https://hkdk.events/abc123", req.URL.String())

	// Test method
	assert.Equal(t, "POST", req.Method)

	// Test content type
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))

	// Verify signature and get body for further testing
	body, isValid := verifySignature(t, req, token.SigningKey)
	assert.True(t, isValid, "Signature should be a valid HMAC SHA256 signature")

	// Test metadata headers from the metadata map
	assert.Equal(t, event.ID, req.Header.Get("X-Outpost-event-id"))
	assert.Equal(t, event.Topic, req.Header.Get("X-Outpost-topic"))
	assert.Equal(t, "1234567890", req.Header.Get("X-Outpost-timestamp"))
	assert.Equal(t, "corr123", req.Header.Get("X-Outpost-correlation-id"))
	assert.Equal(t, "tenant456", req.Header.Get("X-Outpost-tenant-id"))

	// Verify body content
	var bodyData map[string]interface{}
	err = json.Unmarshal(body, &bodyData)
	require.NoError(t, err)

	// Compare with the original data
	assert.Equal(t, "1234567890", bodyData["order_id"])
	assert.Equal(t, 100.50, bodyData["amount"])
	assert.Equal(t, "USD", bodyData["currency"])
}
