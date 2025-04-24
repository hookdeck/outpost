package desthookdeck_test

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/desthookdeck"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// CustomRoundTripper redirects requests to our test server
type CustomRoundTripper struct {
	handler http.Handler
	t       *testing.T
}

func (t *CustomRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Record the original request for possible inspection
	originalURL := req.URL.String()
	originalMethod := req.Method

	// Create a test response recorder
	w := httptest.NewRecorder()

	// Serve the request to our handler
	t.handler.ServeHTTP(w, req)

	// Convert test response to http.Response
	resp := w.Result()

	// For debugging
	if resp.StatusCode >= 400 {
		t.t.Logf("Request to %s %s failed with status %d",
			originalMethod, originalURL, resp.StatusCode)
	}

	return resp, nil
}

func TestPreprocess(t *testing.T) {
	// Create test cases
	testCases := []struct {
		name               string
		tokenID            string
		sourceName         string
		tokenValue         string
		originalToken      string
		expectVerification bool
		expectedError      bool
		expectedErrorType  string
		expectedSourceName string
		expectedSourceID   string
	}{
		{
			name:               "Success_New_Destination",
			tokenID:            "src_GcxGhypwnBIX",
			sourceName:         "test-source",
			tokenValue:         "src_GcxGhypwnBIX:random_string",
			originalToken:      "",
			expectVerification: true,
			expectedError:      false,
			expectedSourceName: "test-source",
			expectedSourceID:   "src_GcxGhypwnBIX",
		},
		{
			name:               "Success_Same_Token",
			tokenID:            "src_GcxGhypwnBIX",
			sourceName:         "test-source",
			tokenValue:         "src_GcxGhypwnBIX:random_string",
			originalToken:      "src_GcxGhypwnBIX:random_string",
			expectVerification: false, // Should not verify if token didn't change
			expectedError:      false,
			expectedSourceName: "",
			expectedSourceID:   "",
		},
		{
			name:               "Success_Token_Change",
			tokenID:            "src_NewToken123",
			sourceName:         "new-source",
			tokenValue:         "src_NewToken123:new_random",
			originalToken:      "src_OldToken456:old_random",
			expectVerification: true,
			expectedError:      false,
			expectedSourceName: "new-source",
			expectedSourceID:   "src_NewToken123",
		},
		{
			name:               "Error_Empty_Token",
			tokenID:            "",
			sourceName:         "",
			tokenValue:         "",
			originalToken:      "",
			expectVerification: false,
			expectedError:      true,
			expectedErrorType:  "token_required",
			expectedSourceName: "",
			expectedSourceID:   "",
		},
		{
			name:               "Error_Invalid_Token_Format",
			tokenID:            "invalid",
			sourceName:         "",
			tokenValue:         "not_a_valid_token", // Missing colon
			originalToken:      "",
			expectVerification: false,
			expectedError:      true,
			expectedErrorType:  "invalid_token_format",
			expectedSourceName: "",
			expectedSourceID:   "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a counter to track API calls
			var verificationCalled bool

			// Create a handler that will respond to the verification request
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Logf("Request received: %s %s", r.Method, r.URL.String())

				// Check if this is a verification request to the expected URL
				expectedPath := fmt.Sprintf("/2025-01-01/sources/%s/managed/verify", tc.tokenID)
				if r.URL.Path == expectedPath && r.Host == "api.hookdeck.com" {
					verificationCalled = true

					// Check token header
					token := r.Header.Get("X-Hookdeck-Source-Token")
					encodedToken := base64.StdEncoding.EncodeToString([]byte(tc.tokenValue))
					assert.Equal(t, encodedToken, token)

					// Generate current timestamps for response
					now := time.Now().UTC().Format(time.RFC3339)

					// Return mock response
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusOK)
					response := fmt.Sprintf(`{
						"name": "%s",
						"url": "https://events.hookdeck.com/e/%s",
						"created_at": "%s",
						"updated_at": "%s",
						"disabled_at": null
					}`, tc.sourceName, tc.tokenID, now, now)
					_, _ = io.WriteString(w, response)
					return
				}

				// Any other request
				t.Errorf("Unexpected request to %s %s", r.Method, r.URL.String())
				w.WriteHeader(http.StatusNotFound)
			})

			// Create a client with our custom transport
			client := &http.Client{
				Transport: &CustomRoundTripper{handler: handler, t: t},
			}

			// Create the provider with our custom client
			provider, err := desthookdeck.New(
				testutil.Registry.MetadataLoader(),
				desthookdeck.WithHTTPClient(client),
			)
			require.NoError(t, err)

			// Create a new destination
			var tokenString string
			if tc.tokenValue != "" {
				tokenString = base64.StdEncoding.EncodeToString([]byte(tc.tokenValue))
			}

			newDestination := testutil.DestinationFactory.Any(
				testutil.DestinationFactory.WithID("dest123"),
				testutil.DestinationFactory.WithType("hookdeck"),
				testutil.DestinationFactory.WithCredentials(map[string]string{
					"token": tokenString,
				}),
			)

			// Create original destination if needed
			var originalDestination *models.Destination
			if tc.originalToken != "" {
				origTokenString := base64.StdEncoding.EncodeToString([]byte(tc.originalToken))
				original := testutil.DestinationFactory.Any(
					testutil.DestinationFactory.WithID("dest123"),
					testutil.DestinationFactory.WithType("hookdeck"),
					testutil.DestinationFactory.WithCredentials(map[string]string{
						"token": origTokenString,
					}),
				)
				originalDestination = &original
			}

			// Execute the Preprocess method
			err = provider.Preprocess(&newDestination, originalDestination, &destregistry.PreprocessDestinationOpts{})

			// Check error result
			if tc.expectedError {
				require.Error(t, err)
				var validationErr *destregistry.ErrDestinationValidation
				require.ErrorAs(t, err, &validationErr)

				if len(validationErr.Errors) > 0 {
					assert.Equal(t, tc.expectedErrorType, validationErr.Errors[0].Type)
				}
			} else {
				require.NoError(t, err)
			}

			// Check if verification was called as expected
			assert.Equal(t, tc.expectVerification, verificationCalled,
				"Verification API call expectation mismatch")

			// Check if source information was stored in config
			if tc.expectedSourceName != "" {
				require.NotNil(t, newDestination.Config)

				// Check source name
				sourceName, exists := newDestination.Config["source_name"]
				assert.True(t, exists, "source_name should exist in config")
				assert.Equal(t, tc.expectedSourceName, sourceName)

				// Check source ID
				sourceID, exists := newDestination.Config["source_id"]
				assert.True(t, exists, "source_id should exist in config")
				assert.Equal(t, tc.expectedSourceID, sourceID)
			}
		})
	}
}

// Test with a server that returns an error
func TestPreprocess_ServerError(t *testing.T) {
	// Create a handler that will respond with an error
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("Request received: %s %s", r.Method, r.URL.String())

		// Any request should return 401
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error": "Invalid token"}`)
	})

	// Create a client with our custom transport
	client := &http.Client{
		Transport: &CustomRoundTripper{handler: handler, t: t},
	}

	// Create the provider with our custom client
	provider, err := desthookdeck.New(
		testutil.Registry.MetadataLoader(),
		desthookdeck.WithHTTPClient(client),
	)
	require.NoError(t, err)

	// Create token string
	tokenString := base64.StdEncoding.EncodeToString([]byte("src_GcxGhypwnBIX:random_string"))

	// Create a new destination
	newDestination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithID("dest123"),
		testutil.DestinationFactory.WithType("hookdeck"),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"token": tokenString,
		}),
	)

	// Execute the Preprocess method
	err = provider.Preprocess(&newDestination, nil, &destregistry.PreprocessDestinationOpts{})

	// Should error with token verification failed
	require.Error(t, err)
	var validationErr *destregistry.ErrDestinationValidation
	require.ErrorAs(t, err, &validationErr)
	assert.Equal(t, "token_verification_failed", validationErr.Errors[0].Type)

	// Verify that no source information was added to the config
	if newDestination.Config != nil {
		_, hasSourceName := newDestination.Config["source_name"]
		assert.False(t, hasSourceName, "source_name should not exist in config after failed verification")

		_, hasSourceID := newDestination.Config["source_id"]
		assert.False(t, hasSourceID, "source_id should not exist in config after failed verification")
	}
}

// Test with timeout
func TestPreprocess_Timeout(t *testing.T) {
	// Create a handler that will hang
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("Request received: %s %s", r.Method, r.URL.String())

		// Sleep longer than the client timeout
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	})

	// Create a client with a very short timeout
	client := &http.Client{
		Transport: &CustomRoundTripper{handler: handler, t: t},
		Timeout:   100 * time.Millisecond,
	}

	// Create the provider with our custom client
	provider, err := desthookdeck.New(
		testutil.Registry.MetadataLoader(),
		desthookdeck.WithHTTPClient(client),
	)
	require.NoError(t, err)

	// Create token string
	tokenString := base64.StdEncoding.EncodeToString([]byte("src_GcxGhypwnBIX:random_string"))

	// Create a new destination
	newDestination := testutil.DestinationFactory.Any(
		testutil.DestinationFactory.WithID("dest123"),
		testutil.DestinationFactory.WithType("hookdeck"),
		testutil.DestinationFactory.WithCredentials(map[string]string{
			"token": tokenString,
		}),
	)

	// Execute the Preprocess method
	err = provider.Preprocess(&newDestination, nil, &destregistry.PreprocessDestinationOpts{})

	// Should error with token verification failed
	require.Error(t, err)
	var validationErr *destregistry.ErrDestinationValidation
	require.ErrorAs(t, err, &validationErr)
	assert.Equal(t, "token_verification_failed", validationErr.Errors[0].Type)

	// Verify that no source information was added to the config
	if newDestination.Config != nil {
		_, hasSourceName := newDestination.Config["source_name"]
		assert.False(t, hasSourceName, "source_name should not exist in config after timeout")

		_, hasSourceID := newDestination.Config["source_id"]
		assert.False(t, hasSourceID, "source_id should not exist in config after timeout")
	}
}
