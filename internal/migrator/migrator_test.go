package migrator

import (
	"context"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigrator_CredentialExposure_Integration is an INTEGRATION TEST
//
// PURPOSE: Verify that database connection errors don't expose credentials in logs.
//
// WHY THIS TEST EXISTS:
// The migrator.New() function calls migrate.NewWithSourceInstance() with a database URL
// that contains credentials. When connection fails, the golang-migrate library includes
// the full URL in its error message, potentially exposing passwords in logs.
//
// POTENTIAL EXPOSURE SCENARIOS IN New():
// 1. Network errors: "dial tcp: connect: connection refused" with full URL
// 2. Auth failures: "authentication failed for user X" with password in URL
// 3. Invalid URL format: "parse postgres://user:pass@host" errors
// 4. DNS failures: "no such host" with credentials in URL
// 5. TLS/SSL errors: Certificate validation failures with full connection string
//
// NOTE: Methods like Up(), Down(), Force() won't expose credentials because they
// operate on an already-established connection and don't reference the URL anymore.
//
// Run with: go test ./internal/migrator
// Skip with: go test -short ./internal/migrator
func TestMigrator_CredentialExposure_Integration(t *testing.T) {
	testutil.Integration(t) // Skip if running with -short flag

	tests := []struct {
		name                 string
		opts                 MigrationOpts
		shouldFailValidation bool
		checkError           func(t *testing.T, err error)
	}{
		{
			name: "PostgreSQL - network connection failure",
			opts: MigrationOpts{
				PG: MigrationOptsPG{
					// Using port 54321 - very unlikely to have a real DB here
					// This simulates network-level connection failure
					URL: "postgres://dbuser:SuperSecret123!@localhost:54321/testdb?sslmode=disable",
				},
			},
			shouldFailValidation: false,
			checkError: func(t *testing.T, err error) {
				// We expect an error since the DB port is likely closed
				require.Error(t, err, "Should fail to connect to non-existent database")

				// The error should NOT contain the actual password
				assert.NotContains(t, err.Error(), "SuperSecret123!",
					"Error message exposed PostgreSQL password")
				// It should also not contain the credentials portion
				assert.NotContains(t, err.Error(), "dbuser:SuperSecret123!",
					"Error message exposed PostgreSQL credentials")

				// The error SHOULD provide useful context about the failure type
				// (e.g., "connection refused", "network error", etc.)
				assert.NotEqual(t, "migrate.New: failed to initialize database connection", err.Error(),
					"Error should provide more context than generic message")
			},
		},
		{
			name: "ClickHouse - connection timeout scenario",
			opts: MigrationOpts{
				CH: MigrationOptsCH{
					// Using non-routable IP to simulate timeout
					// 192.0.2.0/24 is reserved for documentation (RFC 5737)
					Addr:     "192.0.2.1:9000",
					Username: "admin",
					Password: "VerySecretPass456$",
					Database: "analytics_db",
				},
			},
			shouldFailValidation: false,
			checkError: func(t *testing.T, err error) {
				// We expect an error due to timeout/unreachable host
				require.Error(t, err, "Should fail to connect to unreachable host")

				// The error should NOT contain the actual password
				assert.NotContains(t, err.Error(), "VerySecretPass456$",
					"Error message exposed ClickHouse password")
				// It should also not contain the formatted credentials
				assert.NotContains(t, err.Error(), "admin:VerySecretPass456$",
					"Error message exposed ClickHouse credentials")
			},
		},
		{
			name: "PostgreSQL - malformed URL with special characters",
			opts: MigrationOpts{
				PG: MigrationOptsPG{
					// Invalid URL format to test parse errors
					// The ":invalid:port" will cause a parse error
					URL: "postgres://user:P@ssw0rd!#$%@:invalid:port/dbname",
				},
			},
			shouldFailValidation: false,
			checkError: func(t *testing.T, err error) {
				require.Error(t, err, "Should fail with invalid URL format")

				// Even parse errors should not expose the password
				assert.NotContains(t, err.Error(), "P@ssw0rd!#$%",
					"Error message exposed password with special characters")
				assert.NotContains(t, err.Error(), "user:P@ssw0rd",
					"Error message exposed credentials in parse error")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// First, verify the databaseURL() method creates URLs with credentials
			// This confirms we're actually testing something that could leak
			dbURL := tt.opts.databaseURL()
			if tt.opts.PG.URL != "" {
				assert.Contains(t, dbURL, "postgres://",
					"Expected PostgreSQL URL")
				if strings.Contains(tt.opts.PG.URL, ":") && strings.Contains(tt.opts.PG.URL, "@") {
					// If the URL has credentials, verify they're present in the generated URL
					parts := strings.Split(tt.opts.PG.URL, "@")
					if len(parts) > 1 && strings.Contains(parts[0], ":") {
						credParts := strings.Split(parts[0], ":")
						if len(credParts) >= 3 {
							password := credParts[2]
							password = strings.TrimPrefix(password, "//")
							assert.Contains(t, dbURL, password,
								"Test setup: password should be in the database URL")
						}
					}
				}
			} else if tt.opts.CH.Addr != "" {
				assert.Contains(t, dbURL, "clickhouse://",
					"Expected ClickHouse URL")
				assert.Contains(t, dbURL, tt.opts.CH.Password,
					"Test setup: password should be in the database URL")
			}

			// Now test that New() doesn't expose credentials in errors
			// THIS WILL ACTUALLY TRY TO CONNECT TO THE DATABASE
			migrator, err := New(tt.opts)

			if tt.shouldFailValidation {
				require.Error(t, err, "Expected validation error")
				assert.Nil(t, migrator, "Migrator should be nil on validation error")
			}

			// Check that any error doesn't contain credentials
			tt.checkError(t, err)

			// Clean up if migrator was created successfully
			if migrator != nil {
				migrator.Close(context.Background())
			}
		})
	}
}

// TestMigrator_DatabaseURLGeneration is a UNIT TEST
// It tests the databaseURL() method without any external dependencies.
// This verifies that the method correctly builds URLs with credentials,
// which confirms we have something that needs protection.
func TestMigrator_DatabaseURLGeneration(t *testing.T) {
	tests := []struct {
		name        string
		opts        MigrationOpts
		expectedURL string
		hasPassword bool
	}{
		{
			name: "PostgreSQL URL is passed through as-is",
			opts: MigrationOpts{
				PG: MigrationOptsPG{
					URL: "postgres://user:password123@localhost:5432/mydb?sslmode=disable",
				},
			},
			expectedURL: "postgres://user:password123@localhost:5432/mydb?sslmode=disable",
			hasPassword: true,
		},
		{
			name: "ClickHouse URL is constructed with credentials",
			opts: MigrationOpts{
				CH: MigrationOptsCH{
					Addr:     "localhost:9000",
					Username: "admin",
					Password: "secret123",
					Database: "outpost",
				},
			},
			expectedURL: "clickhouse://admin:secret123@localhost:9000/outpost?x-multi-statement=true",
			hasPassword: true,
		},
		{
			name:        "Empty options return empty string",
			opts:        MigrationOpts{},
			expectedURL: "",
			hasPassword: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.opts.databaseURL()
			assert.Equal(t, tt.expectedURL, result)

			if tt.hasPassword {
				// Verify that passwords are indeed present in the URL
				// This confirms the security risk we're trying to address
				if tt.opts.PG.URL != "" && strings.Contains(tt.opts.PG.URL, "password") {
					assert.Contains(t, result, "password",
						"Password should be in the database URL (this is the problem we need to fix)")
				}
				if tt.opts.CH.Password != "" {
					assert.Contains(t, result, tt.opts.CH.Password,
						"Password should be in the database URL (this is the problem we need to fix)")
				}
			}
		})
	}
}

// TestMigrator_URLSanitization is a UNIT TEST
// It demonstrates how URLs should be sanitized to remove credentials.
// This is a blueprint for the actual implementation.
func TestMigrator_URLSanitization(t *testing.T) {
	testCases := []struct {
		name     string
		url      string
		expected []string // Things that should NOT appear in sanitized version
		allowed  []string // Things that should still appear
	}{
		{
			name:     "PostgreSQL with password",
			url:      "postgres://user:password123@localhost:5432/mydb?sslmode=disable",
			expected: []string{"password123", ":password123@"},
			allowed:  []string{"localhost", "5432", "mydb"},
		},
		{
			name:     "PostgreSQL with special characters in password",
			url:      "postgres://user:p@ss!word%20123@localhost:5432/mydb",
			expected: []string{"p@ss!word%20123", ":p@ss!word%20123@"},
			allowed:  []string{"localhost", "5432", "mydb"},
		},
		{
			name:     "ClickHouse URL format",
			url:      "clickhouse://admin:secret123@clickhouse.example.com/outpost?x-multi-statement=true",
			expected: []string{"secret123", ":secret123@", "admin:secret123"},
			allowed:  []string{"clickhouse.example.com", "outpost", "x-multi-statement=true"},
		},
		{
			name:     "URL with no password",
			url:      "postgres://user@localhost:5432/mydb",
			expected: []string{}, // Nothing to redact
			allowed:  []string{"user", "localhost", "5432", "mydb"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Simulate what a sanitization function should do
			sanitized := sanitizeURLForTesting(tc.url)

			// Check that sensitive data is not present
			for _, sensitive := range tc.expected {
				assert.NotContains(t, sanitized, sensitive,
					"Sanitized URL should not contain: %s", sensitive)
			}

			// Check that non-sensitive data is preserved
			for _, expected := range tc.allowed {
				assert.Contains(t, sanitized, expected,
					"Sanitized URL should still contain: %s", expected)
			}
		})
	}
}

// Helper function to demonstrate how URL sanitization should work
// This is what the actual implementation should do
func sanitizeURLForTesting(dbURL string) string {
	// This is a simplified version - the actual implementation
	// would be more robust
	if strings.Contains(dbURL, "postgres://") || strings.Contains(dbURL, "clickhouse://") {
		// Find and replace password patterns
		if idx := strings.Index(dbURL, "://"); idx >= 0 {
			afterScheme := dbURL[idx+3:]
			if atIdx := strings.Index(afterScheme, "@"); atIdx >= 0 {
				userInfo := afterScheme[:atIdx]
				if colonIdx := strings.Index(userInfo, ":"); colonIdx >= 0 {
					// Has password - replace it
					user := userInfo[:colonIdx]
					return dbURL[:idx+3] + user + ":[REDACTED]" + dbURL[idx+3+atIdx:]
				}
			}
		}
	}
	return dbURL
}

// TestMigrator_ClickHouse_DeploymentSuffix tests that ClickHouse migrations
// correctly create tables with or without the deployment suffix.
func TestMigrator_ClickHouse_DeploymentSuffix(t *testing.T) {
	testutil.CheckIntegrationTest(t)
	t.Cleanup(testinfra.Start(t))

	chConfig := testinfra.NewClickHouseConfig(t)

	tests := []struct {
		name          string
		deploymentID  string
		expectedTable string
	}{
		{
			name:          "without deployment ID creates event_log table",
			deploymentID:  "",
			expectedTable: "event_log",
		},
		{
			name:          "with deployment ID creates event_log_{deploymentID} table",
			deploymentID:  "test_deployment",
			expectedTable: "event_log_test_deployment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			// Create migrator with optional deployment ID
			m, err := New(MigrationOpts{
				CH: MigrationOptsCH{
					Addr:         chConfig.Addr,
					Username:     chConfig.Username,
					Password:     chConfig.Password,
					Database:     chConfig.Database,
					DeploymentID: tt.deploymentID,
				},
			})
			require.NoError(t, err)

			// Run migrations up
			version, applied, err := m.Up(ctx, -1)
			require.NoError(t, err)
			assert.Equal(t, 1, version, "should be at version 1")
			assert.Equal(t, 1, applied, "should have applied 1 migration")

			// Verify the correct table was created
			chDB, err := clickhouse.New(&chConfig)
			require.NoError(t, err)
			defer chDB.Close()

			// Check table exists by querying it
			var count uint64
			err = chDB.QueryRow(ctx, "SELECT count() FROM "+tt.expectedTable).Scan(&count)
			require.NoError(t, err, "table %s should exist", tt.expectedTable)

			// Roll back migrations
			version, rolledBack, err := m.Down(ctx, -1)
			require.NoError(t, err)
			assert.Equal(t, 0, version, "should be at version 0 after rollback")
			assert.Equal(t, 1, rolledBack, "should have rolled back 1 migration")

			// Verify table was dropped
			err = chDB.QueryRow(ctx, "SELECT count() FROM "+tt.expectedTable).Scan(&count)
			require.Error(t, err, "table %s should not exist after rollback", tt.expectedTable)

			m.Close(ctx)
		})
	}
}

// TestMigrator_ClickHouse_DeploymentSuffix_Isolation tests that different deployments
// have isolated tables and migrations tracking.
func TestMigrator_ClickHouse_DeploymentSuffix_Isolation(t *testing.T) {
	testutil.CheckIntegrationTest(t)
	t.Cleanup(testinfra.Start(t))

	chConfig := testinfra.NewClickHouseConfig(t)
	ctx := context.Background()

	// Create two migrators with different deployment IDs
	m1, err := New(MigrationOpts{
		CH: MigrationOptsCH{
			Addr:         chConfig.Addr,
			Username:     chConfig.Username,
			Password:     chConfig.Password,
			Database:     chConfig.Database,
			DeploymentID: "deployment_a",
		},
	})
	require.NoError(t, err)
	defer m1.Close(ctx)

	m2, err := New(MigrationOpts{
		CH: MigrationOptsCH{
			Addr:         chConfig.Addr,
			Username:     chConfig.Username,
			Password:     chConfig.Password,
			Database:     chConfig.Database,
			DeploymentID: "deployment_b",
		},
	})
	require.NoError(t, err)
	defer m2.Close(ctx)

	// Run migrations for deployment_a
	_, _, err = m1.Up(ctx, -1)
	require.NoError(t, err)

	// Run migrations for deployment_b
	_, _, err = m2.Up(ctx, -1)
	require.NoError(t, err)

	// Verify both tables exist independently
	chDB, err := clickhouse.New(&chConfig)
	require.NoError(t, err)
	defer chDB.Close()

	var count uint64
	err = chDB.QueryRow(ctx, "SELECT count() FROM event_log_deployment_a").Scan(&count)
	require.NoError(t, err, "event_log_deployment_a should exist")

	err = chDB.QueryRow(ctx, "SELECT count() FROM event_log_deployment_b").Scan(&count)
	require.NoError(t, err, "event_log_deployment_b should exist")

	// Roll back deployment_a - should not affect deployment_b
	_, _, err = m1.Down(ctx, -1)
	require.NoError(t, err)

	// deployment_a table should be gone
	err = chDB.QueryRow(ctx, "SELECT count() FROM event_log_deployment_a").Scan(&count)
	require.Error(t, err, "event_log_deployment_a should not exist after rollback")

	// deployment_b table should still exist
	err = chDB.QueryRow(ctx, "SELECT count() FROM event_log_deployment_b").Scan(&count)
	require.NoError(t, err, "event_log_deployment_b should still exist")

	// Clean up deployment_b
	_, _, err = m2.Down(ctx, -1)
	require.NoError(t, err)
}
