package scheduler

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_generateRSMQID(t *testing.T) {
	tests := []struct {
		name   string
		taskID string
	}{
		{
			name:   "simple task id",
			taskID: "task_123",
		},
		{
			name:   "complex task id",
			taskID: "send_email_user_123_template_456",
		},
		{
			name:   "empty task id",
			taskID: "",
		},
	}

	// RSMQ ID format: 32 chars, alphanumeric only
	validIDPattern := regexp.MustCompile(`^[a-zA-Z0-9]{32}$`)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Generate ID twice to verify deterministic behavior
			id1 := generateRSMQID(tt.taskID)
			id2 := generateRSMQID(tt.taskID)

			// Verify format and length
			assert.Regexp(t, validIDPattern, id1, "ID should match RSMQ format")
			assert.Len(t, id1, 32, "ID should be exactly 32 chars")

			// Verify deterministic (same input = same output)
			assert.Equal(t, id1, id2, "Same input should produce same ID")

			// Verify first 10 chars are zeros (fixed timestamp)
			assert.Equal(t, "0000000000", id1[:10], "First 10 chars should be zeros")
		})
	}

	// Verify different inputs produce different outputs
	id1 := generateRSMQID("task1")
	id2 := generateRSMQID("task2")
	assert.NotEqual(t, id1, id2, "Different inputs should produce different IDs")
}
