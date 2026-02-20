package logretention

import (
	"context"
	"fmt"

	"github.com/hookdeck/outpost/internal/clickhouse"
)

// ClickHouseExecer is a minimal interface for ClickHouse operations needed by ClickHouseTTL.
type ClickHouseExecer interface {
	Exec(ctx context.Context, query string, args ...any) error
}

// ClickHouseTTL implements LogStoreTTL for ClickHouse.
type ClickHouseTTL struct {
	conn          ClickHouseExecer
	eventsTable   string
	attemptsTable string
}

var _ logStoreTTL = (*ClickHouseTTL)(nil)

// NewClickHouseTTL creates a new ClickHouse TTL applier.
func NewClickHouseTTL(conn clickhouse.DB, deploymentID string) *ClickHouseTTL {
	return newClickHouseTTLWithExecer(conn, deploymentID)
}

// newClickHouseTTLWithExecer creates a ClickHouse TTL applier with a minimal execer (for testing).
func newClickHouseTTLWithExecer(conn ClickHouseExecer, deploymentID string) *ClickHouseTTL {
	prefix := ""
	if deploymentID != "" {
		prefix = deploymentID + "_"
	}
	return &ClickHouseTTL{
		conn:          conn,
		eventsTable:   prefix + "events",
		attemptsTable: prefix + "attempts",
	}
}

// ApplyTTL modifies the TTL on ClickHouse tables.
// If ttlDays is 0, the TTL is removed.
func (c *ClickHouseTTL) ApplyTTL(ctx context.Context, ttlDays int) error {
	// Apply TTL to events table
	if err := c.alterTableTTL(ctx, c.eventsTable, "event_time", ttlDays); err != nil {
		return fmt.Errorf("failed to alter TTL on events table: %w", err)
	}

	// Apply TTL to attempts table
	if err := c.alterTableTTL(ctx, c.attemptsTable, "attempt_time", ttlDays); err != nil {
		return fmt.Errorf("failed to alter TTL on attempts table: %w", err)
	}

	return nil
}

// alterTableTTL modifies the TTL on a single ClickHouse table.
// Table/column names are interpolated via fmt.Sprintf because ClickHouse doesn't support
// parameterized identifiers in DDL. All values are derived from operator config (deploymentID)
// and hardcoded column names — never from user input.
func (c *ClickHouseTTL) alterTableTTL(ctx context.Context, tableName, timeColumn string, ttlDays int) error {
	var query string
	if ttlDays == 0 {
		query = fmt.Sprintf("ALTER TABLE %s REMOVE TTL", tableName)
	} else {
		query = fmt.Sprintf("ALTER TABLE %s MODIFY TTL %s + INTERVAL %d DAY", tableName, timeColumn, ttlDays)
	}

	return c.conn.Exec(ctx, query)
}
