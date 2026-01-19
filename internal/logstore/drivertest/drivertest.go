// Package drivertest provides a conformance test suite for logstore drivers.
package drivertest

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/logstore/driver"
)

// Harness provides the test infrastructure for a logstore driver implementation.
type Harness interface {
	MakeDriver(ctx context.Context) (driver.LogStore, error)
	// FlushWrites ensures all writes are fully persisted and visible.
	// For eventually consistent stores (e.g., ClickHouse ReplacingMergeTree),
	// this forces merge/compaction. For immediately consistent stores (e.g., PostgreSQL),
	// this is a no-op.
	FlushWrites(ctx context.Context) error
	Close()
}

// HarnessMaker creates a new Harness for each test.
type HarnessMaker func(ctx context.Context, t *testing.T) (Harness, error)

// RunConformanceTests executes the full conformance test suite for a logstore driver.
// The suite is organized into three parts:
//   - CRUD: basic insert, list, and retrieve operations
//   - Pagination: cursor-based pagination tests using paginationtest.Suite
//   - Misc: isolation, edge cases, and cursor validation
func RunConformanceTests(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	t.Run("CRUD", func(t *testing.T) {
		testCRUD(t, newHarness)
	})
	t.Run("Pagination", func(t *testing.T) {
		testPagination(t, newHarness)
	})
	t.Run("Misc", func(t *testing.T) {
		testMisc(t, newHarness)
	})
}
