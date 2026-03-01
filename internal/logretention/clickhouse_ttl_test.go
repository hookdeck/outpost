package logretention

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockClickHouseConn implements ClickHouseExecer for testing.
type mockClickHouseConn struct {
	execCalls []string
	execErr   error
	// failOnTable allows simulating failure on specific table
	failOnTable string
}

var _ ClickHouseExecer = (*mockClickHouseConn)(nil)

func (m *mockClickHouseConn) Exec(ctx context.Context, query string, args ...any) error {
	m.execCalls = append(m.execCalls, query)
	if m.failOnTable != "" && strings.Contains(query, m.failOnTable) {
		return m.execErr
	}
	if m.execErr != nil && m.failOnTable == "" {
		return m.execErr
	}
	return nil
}

func TestClickHouseTTL_ApplyTTL(t *testing.T) {
	tests := []struct {
		name            string
		deploymentID    string
		ttlDays         int
		wantQueries     []string
		wantQueryCount  int
		execErr         error
		failOnTable     string
		wantErr         bool
		wantErrContains string
	}{
		{
			name:         "set TTL - no deployment",
			deploymentID: "",
			ttlDays:      30,
			wantQueries: []string{
				"ALTER TABLE events MODIFY TTL event_time + INTERVAL 30 DAY",
				"ALTER TABLE attempts MODIFY TTL attempt_time + INTERVAL 30 DAY",
			},
			wantQueryCount: 2,
		},
		{
			name:         "set TTL - with deployment",
			deploymentID: "dpm_001",
			ttlDays:      7,
			wantQueries: []string{
				"ALTER TABLE dpm_001_events MODIFY TTL event_time + INTERVAL 7 DAY",
				"ALTER TABLE dpm_001_attempts MODIFY TTL attempt_time + INTERVAL 7 DAY",
			},
			wantQueryCount: 2,
		},
		{
			name:         "remove TTL - set to 0",
			deploymentID: "",
			ttlDays:      0,
			wantQueries: []string{
				"ALTER TABLE events REMOVE TTL",
				"ALTER TABLE attempts REMOVE TTL",
			},
			wantQueryCount: 2,
		},
		{
			name:            "events table fails - stops before attempts",
			deploymentID:    "",
			ttlDays:         30,
			execErr:         errors.New("table not found"),
			failOnTable:     "events",
			wantErr:         true,
			wantErrContains: "events table",
			wantQueryCount:  1,
		},
		{
			name:            "attempts table fails",
			deploymentID:    "",
			ttlDays:         30,
			execErr:         errors.New("permission denied"),
			failOnTable:     "attempts",
			wantErr:         true,
			wantErrContains: "attempts table",
			wantQueryCount:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn := &mockClickHouseConn{
				execErr:     tt.execErr,
				failOnTable: tt.failOnTable,
			}

			ch := newClickHouseTTLWithExecer(conn, tt.deploymentID)
			err := ch.ApplyTTL(context.Background(), tt.ttlDays)

			if tt.wantErr {
				require.Error(t, err)
				if tt.wantErrContains != "" {
					assert.Contains(t, err.Error(), tt.wantErrContains)
				}
			} else {
				require.NoError(t, err)
			}
			assert.Len(t, conn.execCalls, tt.wantQueryCount)
			if tt.wantQueries != nil && !tt.wantErr {
				for i, wantQuery := range tt.wantQueries {
					if i < len(conn.execCalls) {
						assert.Equal(t, wantQuery, conn.execCalls[i], "query %d", i)
					}
				}
			}
		})
	}
}
