package migrator

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSanitizeConnectionError verifies that our sanitization function removes
// credentials from error messages while preserving the rest of the error context.
func TestSanitizeConnectionError(t *testing.T) {
	tests := []struct {
		name        string
		err         error
		dbURL       string
		contains    []string // Things that SHOULD be in the result
		notContains []string // Things that should NOT be in the result
	}{
		{
			name:  "Connection refused error with full URL in message",
			err:   errors.New(`dial tcp 127.0.0.1:5432: connect: connection refused for "postgres://user:password123@localhost:5432/db"`),
			dbURL: "postgres://user:password123@localhost:5432/db",
			contains: []string{
				"migrate.New:",
				"connection refused",
				"postgres://[REDACTED]@localhost:5432/[REDACTED]", // Full URL should be sanitized
			},
			notContains: []string{
				"password123",
				"user:password123",
				"/db", // Even the database name is redacted for safety
			},
		},
		{
			name:  "Parse error with malformed URL",
			err:   errors.New(`parse "postgres://user:mypass@:invalid:port/db": invalid port ":port" after host`),
			dbURL: "postgres://user:mypass@:invalid:port/db",
			contains: []string{
				"migrate.New:",
				"parse",
				"invalid port",
				"[DATABASE_URL_REDACTED]", // Malformed URL gets fully redacted
			},
			notContains: []string{
				"mypass",
				"user:mypass",
				"postgres://",
			},
		},
		{
			name:  "Authentication failure with password in error",
			err:   errors.New(`pq: password authentication failed for user "admin" with password "secretpass"`),
			dbURL: "postgres://admin:secretpass@localhost/db",
			contains: []string{
				"migrate.New:",
				"authentication failed",
				"admin",
			},
			notContains: []string{
				"secretpass",
				"admin:secretpass",
			},
		},
		{
			name:  "Error without URL but password mentioned separately",
			err:   errors.New(`authentication failed: invalid password "supersecret123" for database`),
			dbURL: "postgres://dbuser:supersecret123@host/db",
			contains: []string{
				"migrate.New:",
				"authentication failed",
				"invalid password",
				"[REDACTED]", // Password should be replaced
			},
			notContains: []string{
				"supersecret123",
			},
		},
		{
			name:  "ClickHouse URL format",
			err:   errors.New(`failed to connect to clickhouse://admin:verysecret@localhost:9000/analytics`),
			dbURL: "clickhouse://admin:verysecret@localhost:9000/analytics",
			contains: []string{
				"migrate.New:",
				"failed to connect",
				"clickhouse://[REDACTED]@localhost:9000/[REDACTED]",
			},
			notContains: []string{
				"verysecret",
				"admin:verysecret",
				"/analytics",
			},
		},
		{
			name:  "URL with special characters in password",
			err:   errors.New(`connection to "postgres://user:p@ss!word%20@localhost/db" failed`),
			dbURL: "postgres://user:p@ss!word%20@localhost/db",
			contains: []string{
				"migrate.New:",
				"connection",
				"failed",
				"postgres://[REDACTED]@localhost/[REDACTED]",
			},
			notContains: []string{
				"p@ss!word%20",
				"p@ss!word",
				"user:p@ss",
				"/db",
			},
		},
		{
			name:  "Error with URL-encoded password",
			err:   errors.New(`failed: postgres://user:pass%40word@host/db`),
			dbURL: "postgres://user:pass@word@host/db", // @ in password
			contains: []string{
				"migrate.New:",
				"failed",
			},
			notContains: []string{
				"pass@word",
				"pass%40word", // URL-encoded version
			},
		},
		{
			name:  "Nil error",
			err:   nil,
			dbURL: "postgres://user:password@localhost/db",
			// For nil error, we expect nil result
		},
		{
			name:  "Empty database URL",
			err:   errors.New(`connection failed with credentials visible`),
			dbURL: "",
			contains: []string{
				"migrate.New:",
				"connection failed with credentials visible", // Should pass through as-is
			},
		},
		{
			name:  "Malformed URL - fallback to pattern matching",
			err:   errors.New(`error with admin:secretpass@host in the message`),
			dbURL: "not-a-valid-url",
			contains: []string{
				"migrate.New:",
				"admin:[REDACTED]@host", // Pattern matching should catch this
			},
			notContains: []string{
				"secretpass",
				"admin:secretpass",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeConnectionError(tt.err, tt.dbURL)

			if tt.err == nil {
				assert.Nil(t, result)
				return
			}

			assert.NotNil(t, result)
			resultStr := result.Error()

			// Check for things that should be present
			for _, expected := range tt.contains {
				assert.Contains(t, resultStr, expected,
					"Expected to find '%s' in error message", expected)
			}

			// Check for things that should NOT be present (credentials)
			for _, forbidden := range tt.notContains {
				assert.NotContains(t, resultStr, forbidden,
					"Found credential '%s' that should have been redacted", forbidden)
			}
		})
	}
}

// TestRemoveCredentialsFromError tests the credential removal function directly
func TestRemoveCredentialsFromError(t *testing.T) {
	tests := []struct {
		name     string
		errMsg   string
		dbURL    string
		expected string
	}{
		{
			name:     "Full URL replacement",
			errMsg:   `connection to "postgres://user:pass@host/db" failed`,
			dbURL:    "postgres://user:pass@host/db",
			expected: `connection to "postgres://user:[REDACTED]@host/db" failed`,
		},
		{
			name:     "Password appears multiple times",
			errMsg:   `auth failed for pass123, password "pass123" is invalid`,
			dbURL:    "postgres://user:pass123@host/db",
			expected: `auth failed for [REDACTED], password "[REDACTED]" is invalid`,
		},
		{
			name:     "User:password pattern",
			errMsg:   `credentials admin:secret were rejected`,
			dbURL:    "postgres://admin:secret@host/db",
			expected: `credentials admin:[REDACTED] were rejected`,
		},
		{
			name:     "URL-encoded password",
			errMsg:   `url contains pass%40word which is encoded`,
			dbURL:    "postgres://user:pass@word@host/db",
			expected: `url contains [REDACTED] which is encoded`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeCredentialsFromError(tt.errMsg, tt.dbURL)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestExtractHostPort verifies that we can safely extract host and port from URLs without exposing credentials.
func TestExtractHostPort(t *testing.T) {
	tests := []struct {
		name         string
		dbURL        string
		expectedHost string
		expectedPort string
	}{
		{
			name:         "PostgreSQL with explicit port",
			dbURL:        "postgres://user:password@localhost:5432/mydb",
			expectedHost: "localhost",
			expectedPort: "5432",
		},
		{
			name:         "PostgreSQL with default port",
			dbURL:        "postgres://user:password@dbserver/mydb",
			expectedHost: "dbserver",
			expectedPort: "5432", // Should infer default
		},
		{
			name:         "ClickHouse with explicit port",
			dbURL:        "clickhouse://admin:secret@clickhouse.local:9000/analytics",
			expectedHost: "clickhouse.local",
			expectedPort: "9000",
		},
		{
			name:         "ClickHouse with default port",
			dbURL:        "clickhouse://admin:secret@ch-server/analytics",
			expectedHost: "ch-server",
			expectedPort: "9000", // Should infer default
		},
		{
			name:         "Invalid URL",
			dbURL:        "not-a-valid-url",
			expectedHost: "unknown",
			expectedPort: "unknown",
		},
		{
			name:         "Empty URL",
			dbURL:        "",
			expectedHost: "unknown",
			expectedPort: "unknown",
		},
		{
			name:         "URL with IPv4 address",
			dbURL:        "postgres://user:pass@192.168.1.1:5433/db",
			expectedHost: "192.168.1.1",
			expectedPort: "5433",
		},
		{
			name:         "URL with IPv6 address",
			dbURL:        "postgres://user:pass@[::1]:5432/db",
			expectedHost: "::1",
			expectedPort: "5432",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host, port := extractHostPort(tt.dbURL)
			assert.Equal(t, tt.expectedHost, host)
			assert.Equal(t, tt.expectedPort, port)

			// Verify no credentials are in the output
			assert.NotContains(t, host, "password")
			assert.NotContains(t, host, "secret")
			assert.NotContains(t, host, "user")
			assert.NotContains(t, host, "admin")
			assert.NotContains(t, port, "password")
			assert.NotContains(t, port, "secret")
		})
	}
}
