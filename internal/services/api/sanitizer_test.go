package api

import (
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry/metadata"
)

func TestRequestBodySanitizer_SanitizeDestinationRequest(t *testing.T) {
	// Create sanitizer with a mock loader for direct testing
	sanitizer := &RequestBodySanitizer{}

	// Test data that simulates AWS Kinesis metadata
	awsKinesisMetadata := &metadata.ProviderMetadata{
		Type: "aws_kinesis",
		ConfigFields: []metadata.FieldSchema{
			{Key: "stream_name", Sensitive: false},
			{Key: "region", Sensitive: false},
		},
		CredentialFields: []metadata.FieldSchema{
			{Key: "key", Sensitive: true},
			{Key: "secret", Sensitive: true},
			{Key: "session", Sensitive: true},
		},
	}

	tests := []struct {
		name     string
		input    map[string]interface{}
		metadata *metadata.ProviderMetadata
		expected string
	}{
		{
			name: "sanitize AWS Kinesis credentials",
			input: map[string]interface{}{
				"type": "aws_kinesis",
				"config": map[string]interface{}{
					"stream_name": "my-stream",
					"region":      "us-east-1",
				},
				"credentials": map[string]interface{}{
					"key":     "AKIAIOSFODNN7EXAMPLE",
					"secret":  "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
					"session": "temporary-session-token",
				},
			},
			metadata: awsKinesisMetadata,
			expected: "[REDACTED]",
		},
		{
			name: "preserve non-sensitive config",
			input: map[string]interface{}{
				"type": "aws_kinesis",
				"config": map[string]interface{}{
					"stream_name": "my-stream",
					"region":      "us-east-1",
				},
			},
			metadata: awsKinesisMetadata,
			expected: "my-stream",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the sanitization logic directly by simulating what would happen
			// if we had the metadata
			if credentials, exists := tt.input["credentials"]; exists {
				result := sanitizer.sanitizeFieldsMap(
					credentials.(map[string]interface{}),
					tt.metadata.CredentialFields,
				)

				// Check that sensitive values are redacted
				if cred, ok := result["key"]; ok && cred != SensitiveFieldMask {
					t.Error("Sensitive key was not redacted")
				}
				if cred, ok := result["secret"]; ok && cred != SensitiveFieldMask {
					t.Error("Sensitive secret was not redacted")
				}
			} else {
				// For tests without credentials, just check that config fields work
				if config, exists := tt.input["config"]; exists {
					result := sanitizer.sanitizeFieldsMap(
						config.(map[string]interface{}),
						tt.metadata.ConfigFields,
					)

					// Check that non-sensitive config values are preserved
					if streamName, ok := result["stream_name"]; ok && streamName != "my-stream" {
						t.Error("Non-sensitive config field was incorrectly modified")
					}
				}
			}
		})
	}
}

func TestRequestBodySanitizer_SizeLimit(t *testing.T) {
	sanitizer := &RequestBodySanitizer{}

	// Create a large input that exceeds MaxRequestBodySize
	largeInput := strings.Repeat("a", MaxRequestBodySize+100)

	result, err := sanitizer.SanitizeRequestBody(strings.NewReader(largeInput))
	if err != nil {
		t.Fatalf("SanitizeRequestBody() error = %v", err)
	}

	resultStr := string(result)
	if !strings.Contains(resultStr, "[REQUEST_BODY_TOO_LARGE:") {
		t.Errorf("Large request body was not handled correctly. Got: %s", resultStr)
	}
}

func TestRequestBodySanitizer_BasicFunctionality(t *testing.T) {
	sanitizer := &RequestBodySanitizer{}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "handle non-JSON input",
			input:    "not json",
			expected: "not json",
		},
		{
			name:     "handle empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "handle valid JSON without type",
			input:    `{"test": "value"}`,
			expected: `{"test":"value"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := sanitizer.SanitizeRequestBody(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("SanitizeRequestBody() error = %v", err)
			}

			resultStr := string(result)
			if resultStr != tt.expected {
				t.Errorf("SanitizeRequestBody() = %v, want %v", resultStr, tt.expected)
			}
		})
	}
}
