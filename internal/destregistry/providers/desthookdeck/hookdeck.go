package desthookdeck

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/models"
)

// Standard error definitions for token parsing
var (
	ErrInvalidTokenBase64 = errors.New("invalid token: not base64 encoded")
	ErrInvalidTokenFormat = errors.New("invalid token format: expected 'id:random_string'")
	ErrJSONMarshalFailed  = errors.New("failed to marshal event data to JSON")
	ErrTokenVerification  = errors.New("failed to verify token with Hookdeck API")
)

// HookdeckToken represents the decoded Hookdeck token
type HookdeckToken struct {
	ID         string
	SigningKey string // This is the original full token
}

// HookdeckSourceResponse represents the response from the Hookdeck API for source verification
type HookdeckSourceResponse struct {
	Name       string     `json:"name"`
	URL        string     `json:"url"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
	DisabledAt *time.Time `json:"disabled_at"`
}

// ParseHookdeckToken parses a Hookdeck token from the base64 encoded string
func ParseHookdeckToken(token string) (*HookdeckToken, error) {
	// Decode base64
	decoded, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return nil, ErrInvalidTokenBase64
	}

	// Split the decoded string
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return nil, ErrInvalidTokenFormat
	}

	return &HookdeckToken{
		ID:         parts[0],
		SigningKey: token, // Use the full original token as signing key
	}, nil
}

// VerifyHookdeckToken makes an API call to Hookdeck to verify the token and get the source information
// This function can be used to verify if a token is valid by making an API call to Hookdeck
func VerifyHookdeckToken(client *http.Client, ctx context.Context, token *HookdeckToken) (*HookdeckSourceResponse, error) {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	// Create the URL for token verification
	verifyURL := fmt.Sprintf("https://api.hookdeck.com/2025-01-01/sources/%s/managed/verify", token.ID)

	// Create the request
	req, err := http.NewRequestWithContext(ctx, "GET", verifyURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating verification request: %w", err)
	}

	// Add the source token header
	req.Header.Set("X-Hookdeck-Source-Token", token.SigningKey)

	// Make the request
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("verification request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%w: received status %d: %s",
			ErrTokenVerification,
			resp.StatusCode,
			string(bodyBytes))
	}

	// Parse response
	var sourceResponse HookdeckSourceResponse
	if err := json.NewDecoder(resp.Body).Decode(&sourceResponse); err != nil {
		return nil, fmt.Errorf("parsing verification response: %w", err)
	}

	return &sourceResponse, nil
}

// CreateRequest creates an HTTP request for a Hookdeck destination
func CreateRequest(token *HookdeckToken, event *models.Event, metadata map[string]string) (*http.Request, error) {
	// Create the Hookdeck URL with the token ID
	hookdeckURL := fmt.Sprintf("https://hkdk.events/%s", token.ID)

	// Marshal the event data to JSON
	payloadBytes, err := json.Marshal(event.Data)
	if err != nil {
		return nil, ErrJSONMarshalFailed
	}

	// Create HMAC signature using the signing key
	h := hmac.New(sha256.New, []byte(token.SigningKey))
	h.Write(payloadBytes)
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	// Create the HTTP request
	req, err := http.NewRequest("POST", hookdeckURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	// Set standard headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hookdeck-Signature", "v0="+signature)

	// Set metadata headers from the provided metadata map with "x-outpost-" prefix
	for key, value := range metadata {
		req.Header.Set("X-Outpost-"+key, value)
	}

	return req, nil
}
