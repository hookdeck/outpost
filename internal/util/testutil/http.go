package testutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"testing"
)

// RequireJSONResponse fails the test unless an API response has Content-Type
// application/json and a non-empty, valid JSON body. request describes the
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
	if len(bytes.TrimSpace(body)) == 0 {
		return fmt.Errorf("empty body, want JSON")
	}
	if !json.Valid(body) {
		return fmt.Errorf("body is not valid JSON: %q", body)
	}
	return nil
}
