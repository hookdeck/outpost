package apirouter

import (
	"strings"
	"testing"
)

func TestRequestBodySanitizer_MCPParamsSecrets(t *testing.T) {
	sanitizer := &RequestBodySanitizer{}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "subscribe body",
			input:    `{"principal":"user_1","params":{"name":"order.created","arguments":{"total":{"$gte":100}},"delivery":{"mode":"webhook","url":"https://r.example.com/h","secret":"whsec_c2VjcmV0c2VjcmV0c2VjcmV0c2VjcmV0"}}}`,
			expected: `{"params":{"arguments":{"total":{"$gte":100}},"delivery":{"mode":"webhook","secret":"[REDACTED]","url":"https://r.example.com/h"},"name":"order.created"},"principal":"user_1"}`,
		},
		{
			name:     "any depth and case",
			input:    `{"PARAMS":{"Secret":"a","x":[{"secret":"b"},{"y":{"SECRET":{"nested":"c"}}}],"other":"kept"}}`,
			expected: `{"PARAMS":{"Secret":"[REDACTED]","other":"kept","x":[{"secret":"[REDACTED]"},{"y":{"SECRET":"[REDACTED]"}}]}}`,
		},
		{
			name:     "secrets outside params are left to the other rules",
			input:    `{"secret":"top","filter":{"secret":"f"}}`,
			expected: `{"filter":{"secret":"f"},"secret":"top"}`,
		},
		{
			name:     "params of another shape",
			input:    `{"params":"whsec_x"}`,
			expected: `{"params":"whsec_x"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := sanitizer.SanitizeRequestBody(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("SanitizeRequestBody() error = %v", err)
			}
			if string(result) != tt.expected {
				t.Errorf("SanitizeRequestBody() = %s, want %s", result, tt.expected)
			}
		})
	}
}
