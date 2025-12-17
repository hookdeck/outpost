package memlogstore

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/logstore/driver"
	"github.com/hookdeck/outpost/internal/logstore/drivertest"
)

type memLogStoreHarness struct {
	logStore driver.LogStore
}

func (h *memLogStoreHarness) MakeDriver(ctx context.Context) (driver.LogStore, error) {
	return h.logStore, nil
}

func (h *memLogStoreHarness) Close() {
	// No-op for in-memory store
}

func (h *memLogStoreHarness) FlushWrites(ctx context.Context) error {
	// In-memory store is immediately consistent
	return nil
}

func newHarness(ctx context.Context, t *testing.T) (drivertest.Harness, error) {
	return &memLogStoreHarness{
		logStore: NewLogStore(),
	}, nil
}

func TestMemLogStoreConformance(t *testing.T) {
	drivertest.RunConformanceTests(t, newHarness)
}
