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

func TestSizeFanOutPool_TotalIsClamped(t *testing.T) {
	t.Parallel()

	// The total is FD-derived, so its exact value depends on the host running
	// the test. What's invariant is that it stays inside the clamp and never
	// sits below the per-host depth.
	for _, concurrency := range []int{0, 1, 16, 512, 100_000} {
		pool := destregistry.SizeFanOutPool(concurrency)
		assert.GreaterOrEqual(t, pool.MaxIdleConns, destregistry.MinTotalIdleConns)
		assert.LessOrEqual(t, pool.MaxIdleConns, destregistry.MaxTotalIdleConns)
		assert.LessOrEqual(t, pool.MaxIdleConnsPerHost, pool.MaxIdleConns,
			"a per-host limit above the total would never be reachable")
	}
}

func TestSizeFanOutPool_ReportsFDLimit(t *testing.T) {
	t.Parallel()

	pool := destregistry.SizeFanOutPool(16)
	assert.Positive(t, pool.FDLimit, "FDLimit should always be populated, fallback included")
}

func TestSizeSingleHostPool_IsDepthOnly(t *testing.T) {
	t.Parallel()

	// One host: the total is the per-host value. No breadth needed, so this
	// stays small regardless of the host's FD limit.
	pool := destregistry.SizeSingleHostPool(32)
	assert.Equal(t, 32, pool.MaxIdleConnsPerHost)
	assert.Equal(t, 32, pool.MaxIdleConns)

	floored := destregistry.SizeSingleHostPool(1)
	assert.Equal(t, destregistry.MinIdleConnsPerHost, floored.MaxIdleConnsPerHost)
	assert.Equal(t, destregistry.MinIdleConnsPerHost, floored.MaxIdleConns)
}
