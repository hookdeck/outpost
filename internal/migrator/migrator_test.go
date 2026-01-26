package migrator

import (
	"context"
	"net/url"
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
			name: "ClickHouse - connection failure scenario",
			opts: MigrationOpts{
				CH: MigrationOptsCH{
					// Using localhost:1 - port 1 is reserved and will be refused immediately
					// This tests credential sanitization without waiting for network timeout
					Addr:     "localhost:1",
					Username: "admin",
					Password: "VerySecretPass456$",
					Database: "analytics_db",
				},
			},
			shouldFailValidation: false,
			checkError: func(t *testing.T, err error) {
				// We expect an error due to connection refused
				require.Error(t, err, "Should fail to connect to closed port")

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
				// Password is URL-encoded in the query string
				assert.Contains(t, dbURL, url.QueryEscape(tt.opts.CH.Password),
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
			expectedURL: "clickhouse://localhost:9000/outpost?username=admin&password=secret123&x-multi-statement=true&x-migrations-table-engine=MergeTree",
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

// TestMigrator_DeploymentID_TableNaming tests that deployment ID creates prefixed tables.
func TestMigrator_DeploymentID_TableNaming(t *testing.T) {
	testutil.Integration(t)

	ctx := context.Background()
	chConfig := setupClickHouseConfig(t)

	// Run migrations with deployment ID
	m, err := New(MigrationOpts{
		CH: MigrationOptsCH{
			Addr:         chConfig.Addr,
			Username:     chConfig.Username,
			Password:     chConfig.Password,
			Database:     chConfig.Database,
			DeploymentID: "testdeploy",
		},
	})
	require.NoError(t, err)
	defer m.Close(ctx)

	_, _, err = m.Up(ctx, -1)
	require.NoError(t, err)

	// Verify prefixed tables exist
	chDB, err := clickhouse.New(&chConfig)
	require.NoError(t, err)
	defer chDB.Close()

	var count uint64
	err = chDB.QueryRow(ctx, "SELECT count() FROM system.tables WHERE database = ? AND name = ?",
		chConfig.Database, "testdeploy_events").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), count, "testdeploy_events table should exist")

	err = chDB.QueryRow(ctx, "SELECT count() FROM system.tables WHERE database = ? AND name = ?",
		chConfig.Database, "testdeploy_deliveries").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), count, "testdeploy_deliveries table should exist")
}

// TestMigrator_DeploymentID_Isolation tests that multiple deployments are isolated.
func TestMigrator_DeploymentID_Isolation(t *testing.T) {
	testutil.Integration(t)

	ctx := context.Background()
	chConfig := setupClickHouseConfig(t)

	// Run migrations for deployment A
	mA, err := New(MigrationOpts{
		CH: MigrationOptsCH{
			Addr:         chConfig.Addr,
			Username:     chConfig.Username,
			Password:     chConfig.Password,
			Database:     chConfig.Database,
			DeploymentID: "deploy_a",
		},
	})
	require.NoError(t, err)
	_, _, err = mA.Up(ctx, -1)
	require.NoError(t, err)
	mA.Close(ctx)

	// Run migrations for deployment B
	mB, err := New(MigrationOpts{
		CH: MigrationOptsCH{
			Addr:         chConfig.Addr,
			Username:     chConfig.Username,
			Password:     chConfig.Password,
			Database:     chConfig.Database,
			DeploymentID: "deploy_b",
		},
	})
	require.NoError(t, err)
	_, _, err = mB.Up(ctx, -1)
	require.NoError(t, err)
	mB.Close(ctx)

	// Verify both deployments have their own tables
	chDB, err := clickhouse.New(&chConfig)
	require.NoError(t, err)
	defer chDB.Close()

	tables := []string{
		"deploy_a_events", "deploy_a_deliveries",
		"deploy_b_events", "deploy_b_deliveries",
	}
	for _, table := range tables {
		var count uint64
		err = chDB.QueryRow(ctx, "SELECT count() FROM system.tables WHERE database = ? AND name = ?",
			chConfig.Database, table).Scan(&count)
		require.NoError(t, err)
		assert.Equal(t, uint64(1), count, "%s table should exist", table)
	}

	// Insert data into deployment A
	err = chDB.Exec(ctx, `
		INSERT INTO deploy_a_events (event_id, tenant_id, destination_id, topic, eligible_for_retry, event_time, metadata, data)
		VALUES ('evt_a', 'tenant_a', 'dest_a', 'topic_a', false, now(), '{}', '{}')
	`)
	require.NoError(t, err)

	// Insert data into deployment B
	err = chDB.Exec(ctx, `
		INSERT INTO deploy_b_events (event_id, tenant_id, destination_id, topic, eligible_for_retry, event_time, metadata, data)
		VALUES ('evt_b', 'tenant_b', 'dest_b', 'topic_b', false, now(), '{}', '{}')
	`)
	require.NoError(t, err)

	// Verify isolation: deployment A should only see its own data
	var eventID string
	err = chDB.QueryRow(ctx, "SELECT event_id FROM deploy_a_events WHERE event_id = 'evt_a'").Scan(&eventID)
	require.NoError(t, err)
	assert.Equal(t, "evt_a", eventID)

	// deployment A should not see deployment B's data
	err = chDB.QueryRow(ctx, "SELECT event_id FROM deploy_a_events WHERE event_id = 'evt_b'").Scan(&eventID)
	require.Error(t, err) // Should be no rows

	// deployment B should only see its own data
	err = chDB.QueryRow(ctx, "SELECT event_id FROM deploy_b_events WHERE event_id = 'evt_b'").Scan(&eventID)
	require.NoError(t, err)
	assert.Equal(t, "evt_b", eventID)
}

// TestMigrator_NoDeploymentID_DefaultTables tests that no deployment ID uses default table names.
func TestMigrator_NoDeploymentID_DefaultTables(t *testing.T) {
	testutil.Integration(t)

	ctx := context.Background()
	chConfig := setupClickHouseConfig(t)

	// Run migrations without deployment ID
	m, err := New(MigrationOpts{
		CH: MigrationOptsCH{
			Addr:     chConfig.Addr,
			Username: chConfig.Username,
			Password: chConfig.Password,
			Database: chConfig.Database,
			// DeploymentID intentionally omitted
		},
	})
	require.NoError(t, err)
	defer m.Close(ctx)

	_, _, err = m.Up(ctx, -1)
	require.NoError(t, err)

	// Verify default tables exist (no prefix)
	chDB, err := clickhouse.New(&chConfig)
	require.NoError(t, err)
	defer chDB.Close()

	var count uint64
	err = chDB.QueryRow(ctx, "SELECT count() FROM system.tables WHERE database = ? AND name = ?",
		chConfig.Database, "events").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), count, "events table should exist")

	err = chDB.QueryRow(ctx, "SELECT count() FROM system.tables WHERE database = ? AND name = ?",
		chConfig.Database, "deliveries").Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), count, "deliveries table should exist")
}

func setupClickHouseConfig(t *testing.T) clickhouse.ClickHouseConfig {
	t.Helper()
	t.Cleanup(testinfra.Start(t))
	return testinfra.NewClickHouseConfig(t)
}
