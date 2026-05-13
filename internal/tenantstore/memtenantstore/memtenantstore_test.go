package memtenantstore

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/tenantstore/driver"
	"github.com/hookdeck/outpost/internal/tenantstore/drivertest"
)

type memTenantStoreHarness struct{}

func (h *memTenantStoreHarness) MakeDriver(_ context.Context) (driver.TenantStore, error) {
	return New(), nil
}

func (h *memTenantStoreHarness) MakeDriverWithMaxDest(_ context.Context, maxDest int) (driver.TenantStore, error) {
	return New(WithMaxDestinationsPerTenant(maxDest)), nil
}

func (h *memTenantStoreHarness) MakeIsolatedDrivers(_ context.Context) (driver.TenantStore, driver.TenantStore, error) {
	return New(), New(), nil
}

func (h *memTenantStoreHarness) Close() {}

func newHarness(_ context.Context, _ *testing.T) (drivertest.Harness, error) {
	return &memTenantStoreHarness{}, nil
}

func TestMemTenantStoreConformance(t *testing.T) {
	drivertest.RunConformanceTests(t, newHarness)
}
