package app

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsLockRelatedError verifies lock error detection for all known lock error patterns
func TestIsLockRelatedError(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		shouldMatch bool
	}{
		// Lock errors that should be retried
		{
			name:        "database.ErrLocked",
			err:         errors.New("can't acquire lock"),
			shouldMatch: true,
		},
		{
			name:        "postgres advisory lock failure with pg_advisory_lock",
			err:         errors.New("migrate.New: failed to open database: try lock failed in line 0: SELECT pg_advisory_lock($1) (details: pq: unnamed prepared statement does not exist)"),
			shouldMatch: true,
		},
		{
			name:        "try lock failed",
			err:         errors.New("try lock failed"),
			shouldMatch: true,
		},

		// Non-lock errors that should NOT be retried
		{
			name:        "connection refused",
			err:         errors.New("connection refused"),
			shouldMatch: false,
		},
		{
			name:        "SQL syntax error",
			err:         errors.New("syntax error at or near"),
			shouldMatch: false,
		},
		{
			name:        "authentication error",
			err:         errors.New("password authentication failed"),
			shouldMatch: false,
		},
		{
			name:        "timeout error",
			err:         errors.New("context deadline exceeded"),
			shouldMatch: false,
		},
		{
			name:        "nil error",
			err:         nil,
			shouldMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isLockRelatedError(tt.err)
			assert.Equal(t, tt.shouldMatch, result,
				"isLockRelatedError should return %v for: %v", tt.shouldMatch, tt.err)
		})
	}
}
