package gcpcreds_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"cloud.google.com/go/pubsub"
	"github.com/hookdeck/outpost/internal/util/gcpcreds"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/option"
	"google.golang.org/api/option/internaloption"
	"google.golang.org/api/transport"
)

func TestCheckServiceAccount(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr error
	}{
		{"service account", `{"type":"service_account"}`, nil},
		{"type key in other case", `{"TYPE":"service_account"}`, nil},
		{"escaped type key", `{"type":"service_account"}`, nil},
		{"repeated type key, same value", `{"type":"service_account","Type":"service_account"}`, nil},
		{"other credential type", `{"type":"authorized_user"}`, gcpcreds.ErrNotServiceAccount},
		{"type key in different case overrides service_account", `{"type":"service_account","Type":"authorized_user"}`, gcpcreds.ErrNotServiceAccount},
		{"escaped type key overrides service_account", `{"type":"service_account","Type":"authorized_user"}`, gcpcreds.ErrNotServiceAccount},
		{"last exact type key wins", `{"type":"service_account","type":"authorized_user"}`, gcpcreds.ErrNotServiceAccount},
		{"empty type", `{"type":""}`, gcpcreds.ErrNotServiceAccount},
		{"null type", `{"type":null}`, gcpcreds.ErrMissingType},
		{"missing type", `{"project_id":"p"}`, gcpcreds.ErrMissingType},
		{"nested type only", `{"x":{"type":"service_account"}}`, gcpcreds.ErrMissingType},
		{"non-string type", `{"type":1}`, gcpcreds.ErrInvalidJSON},
		{"array", `[{"type":"service_account"}]`, gcpcreds.ErrInvalidJSON},
		{"not JSON", `not-json`, gcpcreds.ErrInvalidJSON},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := gcpcreds.CheckServiceAccount([]byte(tt.json))
			if tt.wantErr == nil {
				assert.NoError(t, err)
			} else {
				assert.ErrorIs(t, err, tt.wantErr)
			}

			// The check must agree with the auth library's own
			// service-account-only loader on every input.
			_, libErr := google.CredentialsFromJSONWithType(context.Background(), []byte(tt.json), google.ServiceAccount, pubsub.ScopePubSub)
			assert.Equal(t, err == nil, libErr == nil, "check: %v, library: %v", err, libErr)
		})
	}
}

// The Pub/Sub destination and infra setup pass the key with
// option.WithCredentialsJSON; with Pub/Sub's client defaults the library signs
// access tokens locally from the key, without a network call.
func TestServiceAccountKeySignsTokensLocally(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)

	var key map[string]string
	require.NoError(t, json.Unmarshal([]byte(testutil.GCPServiceAccountKeyJSON(t)), &key))
	key["token_uri"] = server.URL
	keyJSON, err := json.Marshal(key)
	require.NoError(t, err)
	require.NoError(t, gcpcreds.CheckServiceAccount(keyJSON))

	creds, err := transport.Creds(context.Background(),
		option.WithCredentialsJSON(keyJSON),
		// Pub/Sub's client defaults (cloud.google.com/go/pubsub/apiv1).
		internaloption.WithDefaultAudience("https://pubsub.googleapis.com/"),
		internaloption.WithDefaultScopes(pubsub.ScopePubSub, pubsub.ScopeCloudPlatform),
		internaloption.EnableJwtWithScope(),
	)
	require.NoError(t, err)
	token, err := creds.TokenSource.Token()
	require.NoError(t, err)
	assert.NotEmpty(t, token.AccessToken)
	assert.Zero(t, requests.Load())
}
