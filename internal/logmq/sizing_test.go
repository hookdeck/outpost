package logmq

// White-box: pins the pool-sizing derivation. The formulas encode
// the operating contract — line rate at any send latency the emit timeout
// accepts, one batch of queue slack, visibility-safe ack latency — so a
// change here is a behavior change, not a refactor.

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDerivePostprocessShards(t *testing.T) {
	t.Parallel()
	// shards × 250 evals/s must cover LogBatchSize entries/s.
	assert.Equal(t, 8, derivePostprocessShards(0), "floor: tests and tiny deployments")
	assert.Equal(t, 8, derivePostprocessShards(1000), "default batch: 1000/250=4, floored to 8")
	assert.Equal(t, 16, derivePostprocessShards(4000))
	assert.Equal(t, 64, derivePostprocessShards(20000), "20000/250=80, capped: past 64 the wall is Redis/intake")
}

func TestDerivePostprocessShardQueueDepth(t *testing.T) {
	t.Parallel()
	// depth × shards ≈ one full batch, so a healthy dispatch never blocks.
	assert.Equal(t, 125, derivePostprocessShardQueueDepth(1000, 8))
	assert.Equal(t, 313, derivePostprocessShardQueueDepth(20000, 64), "ceil(20000/64)")
	assert.Equal(t, 1, derivePostprocessShardQueueDepth(0, 8), "floor: never an unbuffered shard")
}

func TestDeriveDeliveryConcurrency(t *testing.T) {
	t.Parallel()
	// workers = rate × worst accepted latency = LogBatchSize/s × emitTimeout.
	assert.Equal(t, 10, deriveDeliveryConcurrency(0), "floor")
	assert.Equal(t, 10, deriveDeliveryConcurrency(2), "small test batches stay at the floor")
	assert.Equal(t, 5000, deriveDeliveryConcurrency(1000), "1000/s sustained with every send at the 5s timeout")
	assert.Equal(t, 8192, deriveDeliveryConcurrency(20000), "capped: horizontal-scale territory")
}

// The visibility invariant the sizes must satisfy: everything fetched reaches
// a terminal state within the broker's ~60s window even with every send at
// emitTimeout. held ≈ 2×batch (batcher + shard queues) + 3×W (delivery queue
// 2W + in-hand W); drain = W/emitTimeout, so
//
//	sojourn = 10×batch/W + 3×emitTimeout
//
// Below the worker cap (batch ≤ 1638) W = 5×batch and sojourn is a constant
// 17s. Above it W is pinned at 8192 and sojourn grows with batch, crossing
// 60s at batch ≈ 37k — the single-instance envelope's edge. Past that, an
// operator needs horizontal scale (or a longer visibility window), not a
// bigger pool; the second subtest documents that boundary rather than
// pretending the formula covers it.
func TestDerivedSizingRespectsVisibility(t *testing.T) {
	t.Parallel()
	const visibilityBudgetSecs = 60.0
	emitSecs := emitTimeout.Seconds()
	sojourn := func(batch int) float64 {
		w := deriveDeliveryConcurrency(batch)
		held := float64(2*batch + 3*w)
		return held / (float64(w) / emitSecs)
	}

	t.Run("within envelope", func(t *testing.T) {
		for _, batch := range []int{1, 100, 1000, 5000, 20000, 36000} {
			assert.LessOrEqualf(t, sojourn(batch), visibilityBudgetSecs,
				"batch=%d: worst-case ack latency %.1fs must fit the visibility window", batch, sojourn(batch))
		}
	})

	t.Run("envelope edge", func(t *testing.T) {
		assert.Greater(t, sojourn(50000), visibilityBudgetSecs,
			"past ~37k the capped pool can no longer make a timeout-pinned sink visibility-safe — horizontal-scale territory, by design")
	})
}
