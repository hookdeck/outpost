package partitionkey_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry/partitionkey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		template    string
		payload     map[string]interface{}
		fallbackKey string
		expected    string
		expectError bool
	}{
		{
			name:        "empty template returns fallback",
			template:    "",
			payload:     map[string]interface{}{"key": "value"},
			fallbackKey: "fallback-123",
			expected:    "fallback-123",
		},
		{
			name:     "simple field access",
			template: "metadata.topic",
			payload: map[string]interface{}{
				"metadata": map[string]interface{}{"topic": "test-topic"},
			},
			fallbackKey: "fallback",
			expected:    "test-topic",
		},
		{
			name:     "nested field access",
			template: "data.user.id",
			payload: map[string]interface{}{
				"data": map[string]interface{}{
					"user": map[string]interface{}{"id": "user-456"},
				},
			},
			fallbackKey: "fallback",
			expected:    "user-456",
		},
		{
			name:     "join expression",
			template: "join('-', [metadata.topic, metadata.\"event-id\"])",
			payload: map[string]interface{}{
				"metadata": map[string]interface{}{
					"topic":    "test-topic",
					"event-id": "event-123",
				},
			},
			fallbackKey: "fallback",
			expected:    "test-topic-event-123",
		},
		{
			name:     "non-existent field returns fallback",
			template: "metadata.nonexistent",
			payload: map[string]interface{}{
				"metadata": map[string]interface{}{"topic": "test"},
			},
			fallbackKey: "fallback-123",
			expected:    "fallback-123",
		},
		{
			name:        "invalid template syntax returns error",
			template:    "metadata.topic[",
			payload:     map[string]interface{}{},
			fallbackKey: "fallback",
			expectError: true,
		},
		{
			name:     "numeric value",
			template: "data.count",
			payload: map[string]interface{}{
				"data": map[string]interface{}{"count": float64(123)},
			},
			fallbackKey: "fallback",
			expected:    "123",
		},
		{
			name:     "boolean value",
			template: "data.active",
			payload: map[string]interface{}{
				"data": map[string]interface{}{"active": true},
			},
			fallbackKey: "fallback",
			expected:    "true",
		},
		{
			name:     "empty string result returns fallback",
			template: "data.empty",
			payload: map[string]interface{}{
				"data": map[string]interface{}{"empty": ""},
			},
			fallbackKey: "fallback-123",
			expected:    "fallback-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := partitionkey.Evaluate(tt.template, tt.payload, tt.fallbackKey)
			if tt.expectError {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}
