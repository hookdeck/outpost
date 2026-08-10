package destregistry_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/stretchr/testify/assert"
)

func TestSizeFanOutPool_PerHostDerivesFromConcurrency(t *testing.T) {
	t.Parallel()

	// Per-host depth tracks the delivery worker pool, floored at Go's default
	// so DELIVERY_MAX_CONCURRENCY=1 never sizes us below stock behavior.
	assert.Equal(t, destregistry.MinIdleConnsPerHost, destregistry.SizeFanOutPool(0).MaxIdleConnsPerHost,
		"unknown concurrency should use the floor")
	assert.Equal(t, destregistry.MinIdleConnsPerHost, destregistry.SizeFanOutPool(1).MaxIdleConnsPerHost,
		"concurrency below the floor should use the floor")
	assert.Equal(t, 64, destregistry.SizeFanOutPool(64).MaxIdleConnsPerHost)
}

func TestSizeFanOutPool_TotalScalesWithConcurrency(t *testing.T) {
	t.Parallel()

	// total = clamp(32×C, 512, max(4096, C)).
	for _, tc := range []struct {
		concurrency int
		total       int
	}{
		{0, destregistry.MinTotalIdleConns},   // unknown → floor
		{1, destregistry.MinTotalIdleConns},   // 32 → floor
		{16, destregistry.MinTotalIdleConns},  // 512 → exactly the floor
		{64, 2048},                            // 32×64, between floor and cap
		{128, destregistry.MaxTotalIdleConns}, // 4096 → exactly the cap
		{1000, destregistry.MaxTotalIdleConns},
	} {
		assert.Equal(t, tc.total, destregistry.SizeFanOutPool(tc.concurrency).MaxIdleConns,
			"concurrency %d", tc.concurrency)
	}
}

func TestSizeFanOutPool_CapNeverBindsBelowConcurrency(t *testing.T) {
	t.Parallel()

	// Above the cap, the total rises to the concurrency level itself so the
	// per-host depth (== concurrency) is always reachable.
	pool := destregistry.SizeFanOutPool(10_000)
	assert.Equal(t, 10_000, pool.MaxIdleConns)
	assert.Equal(t, 10_000, pool.MaxIdleConnsPerHost)

	for _, concurrency := range []int{0, 1, 16, 512, 100_000} {
		pool := destregistry.SizeFanOutPool(concurrency)
		assert.LessOrEqual(t, pool.MaxIdleConnsPerHost, pool.MaxIdleConns,
			"a per-host limit above the total would never be reachable (concurrency %d)", concurrency)
	}
}

func TestSizeSingleHostPool_IsDepthOnly(t *testing.T) {
	t.Parallel()

	// One host: the total is the per-host value. No breadth needed, so this
	// stays small regardless of the fan-out formula.
	pool := destregistry.SizeSingleHostPool(32)
	assert.Equal(t, 32, pool.MaxIdleConnsPerHost)
	assert.Equal(t, 32, pool.MaxIdleConns)

	floored := destregistry.SizeSingleHostPool(1)
	assert.Equal(t, destregistry.MinIdleConnsPerHost, floored.MaxIdleConnsPerHost)
	assert.Equal(t, destregistry.MinIdleConnsPerHost, floored.MaxIdleConns)
}
