package testutil_test

import (
	"net/http"
	"testing"

	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
)

func TestCheckJSONResponse(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		ok          bool
	}{
		{"json object", "application/json; charset=utf-8", `{"status":404}`, true},
		{"json array", "application/json", `[]`, true},
		{"no body, no content type", "", "", false},
		{"json content type, no body", "application/json", "", false},
		{"text body", "text/plain", "404 page not found", false},
		{"json body, text content type", "text/plain", `{"status":404}`, false},
		{"json content type, invalid body", "application/json", "not json", false},
		{"json null", "application/json", "null", false},
		{"json string", "application/json", `"not found"`, false},
		{"json number", "application/json", "404", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := http.Header{}
			if tt.contentType != "" {
				header.Set("Content-Type", tt.contentType)
			}
			err := testutil.CheckJSONResponse(header, []byte(tt.body))
			if tt.ok {
				assert.NoError(t, err)
			} else {
				assert.Error(t, err)
			}
		})
	}
}
