package drivertest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func testMetrics(t *testing.T, newHarness HarnessMaker) {
	t.Helper()

	ctx := context.Background()
	h, err := newHarness(ctx, t)
	require.NoError(t, err)
	t.Cleanup(h.Close)

	logStore, err := h.MakeDriver(ctx)
	require.NoError(t, err)

	// Build and seed the dataset (shared across both sub-suites).
	ds := buildMetricsDataset()
	err = logStore.InsertMany(ctx, ds.entries)
	require.NoError(t, err)
	require.NoError(t, h.FlushWrites(ctx))

	t.Run("DataCorrectness", func(t *testing.T) {
		testMetricsDataCorrectness(t, ctx, logStore, ds)
	})
	t.Run("Characteristics", func(t *testing.T) {
		testMetricsCharacteristics(t, ctx, logStore, ds)
	})
}
