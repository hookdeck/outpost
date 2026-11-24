package testutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// RequireJSONResponse fails the test unless an API response has Content-Type
// application/json and a JSON object or array as its body. request describes the
// request in the failure message, e.g. "GET /api/v1/tenants".
func RequireJSONResponse(t testing.TB, request string, status int, header http.Header, body []byte) {
	t.Helper()
	if err := CheckJSONResponse(header, body); err != nil {
		t.Fatalf("%s: status %d: %v", request, status, err)
	}
}

// CheckJSONResponse reports why a response is not JSON with a body.
func CheckJSONResponse(header http.Header, body []byte) error {
	contentType := header.Get("Content-Type")
	if mediaType, _, _ := mime.ParseMediaType(contentType); mediaType != "application/json" {
		return fmt.Errorf("content type is %q, want application/json (body: %q)", contentType, body)
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return fmt.Errorf("empty body, want JSON")
	}
	if !json.Valid(body) {
		return fmt.Errorf("body is not valid JSON: %q", body)
	}
	if trimmed[0] != '{' && trimmed[0] != '[' {
		return fmt.Errorf("body is not a JSON object or array: %q", body)
	}
	return nil
}

// RequireErrorResponse asserts that resp is the JSON error envelope with the
// given status code and message.
func RequireErrorResponse(t testing.TB, resp *httptest.ResponseRecorder, status int, message string) {
	t.Helper()
	require.Equal(t, status, resp.Code)

	mediaType, _, _ := mime.ParseMediaType(resp.Header().Get("Content-Type"))
	assert.Equal(t, "application/json", mediaType)

	var body map[string]any
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body), "body: %s", resp.Body.String())
	assert.Equal(t, float64(status), body["status"])
	assert.Equal(t, message, body["message"])
}
