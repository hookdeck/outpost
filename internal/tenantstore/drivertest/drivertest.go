// Package drivertest provides a conformance test suite for tenantstore drivers.
package drivertest

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/tenantstore/driver"
)

// Harness provides the test infrastructure for a tenantstore driver implementation.
type Harness interface {
	// MakeDriver creates a driver with default settings.
	MakeDriver(ctx context.Context) (driver.TenantStore, error)
	// MakeDriverWithMaxDest creates a driver with a specific max destinations limit.
	MakeDriverWithMaxDest(ctx context.Context, maxDest int) (driver.TenantStore, error)
	// MakeIsolatedDrivers creates two drivers that share the same backend
	// but are isolated from each other (e.g., different deployment IDs).
	MakeIsolatedDrivers(ctx context.Context) (store1, store2 driver.TenantStore, err error)
	Close()
}

// HarnessMaker creates a new Harness for each test.
type HarnessMaker func(ctx context.Context, t *testing.T) (Harness, error)

// RunConformanceTests executes the core conformance test suite for a tenantstore driver.
// The suite is organized into four parts:
//   - CRUD: tenant and destination create/read/update/delete
//   - List: destination listing and filtering operations
//   - Match: event matching operations
//   - Misc: max destinations, deployment isolation
func RunConformanceTests(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	t.Run("CRUD", func(t *testing.T) {
		testCRUD(t, newHarness)
	})
	t.Run("List", func(t *testing.T) {
		testList(t, newHarness)
	})
	t.Run("Match", func(t *testing.T) {
		testMatch(t, newHarness)
	})
	t.Run("Misc", func(t *testing.T) {
		testMisc(t, newHarness)
	})
}

// RunListTenantTests executes the ListTenant test suite, which requires RediSearch.
// Run this only on backends that support RediSearch (Redis Stack, Dragonfly Stack).
// The suite covers:
//   - Enrichment: tenant list includes destinations count and topics
//   - ExcludesDeleted: deleted tenants are excluded from results
//   - InputValidation: limit, order direction, cursor validation
//   - KeysetPagination: cursor-based pagination edge cases
//   - PaginationSuite: comprehensive forward/backward/round-trip pagination
func RunListTenantTests(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	testListTenant(t, newHarness)
}
