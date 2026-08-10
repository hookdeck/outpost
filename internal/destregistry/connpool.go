package destregistry

// Connection pool sizing for outbound delivery clients.
//
// Go's defaults are wrong for this workload in both directions: two idle
// connections per host means reuse collapses above two concurrent deliveries
// to a destination, and a 100 connection total means destinations evict each
// other in any fan-out deployment. The pool is sized from the delivery worker
// pool (DELIVERY_MAX_CONCURRENCY) as a sane default that scales with the
// deployment; an explicit config knob can be exposed later if a workload
// needs one.
//
// MaxIdleConns bounds only parked (idle) connections, never active ones —
// exceeding it causes connection churn, not errors. That makes oversizing
// cheap (idle FDs) and undersizing merely a reuse-rate loss.

const (
	// IdleConnsPerConcurrency scales the total idle pool with the delivery
	// worker count: at ~3s per delivery a worker revisits roughly 32 distinct
	// destinations within the 90s IdleConnTimeout, and slow destinations are
	// where reuse matters most.
	IdleConnsPerConcurrency = 32

	// MinTotalIdleConns floors the total for low-concurrency fanout.
	// Concurrency bounds simultaneous requests, not distinct hosts touched
	// over time — a single worker at ~100ms per delivery still cycles through
	// ~900 destinations per idle window.
	MinTotalIdleConns = 512

	// MaxTotalIdleConns caps the parked-FD/memory cost where the reuse hit
	// rate decays. The cap never binds below the concurrency level itself —
	// see SizeFanOutPool.
	MaxTotalIdleConns = 4096

	// MinIdleConnsPerHost is the floor for per-host depth, matching Go's
	// default. DELIVERY_MAX_CONCURRENCY defaults to 1, which would otherwise
	// size us below stock behavior.
	MinIdleConnsPerHost = 2
)

// PoolSizing is the resolved connection pool configuration.
type PoolSizing struct {
	// MaxIdleConns bounds breadth — how many distinct destinations can hold a
	// warm connection at all.
	MaxIdleConns int

	// MaxIdleConnsPerHost bounds depth — how many warm connections a single
	// destination keeps.
	MaxIdleConnsPerHost int
}

// SizeFanOutPool sizes a pool for a client that talks to arbitrarily many
// destination hosts (the webhook providers). Depth comes from the delivery
// worker pool — it caps how many deliveries can be in flight, so it caps how
// many connections one destination could need. Breadth scales with the same
// number: total = clamp(32×C, 512, max(4096, C)), the ceiling raised to C so
// the cap never undersizes the pool below the concurrency level.
//
// deliveryMaxConcurrency <= 0 means "unknown"; the floors apply.
func SizeFanOutPool(deliveryMaxConcurrency int) PoolSizing {
	total := deliveryMaxConcurrency * IdleConnsPerConcurrency
	if total < MinTotalIdleConns {
		total = MinTotalIdleConns
	}
	if ceiling := max(MaxTotalIdleConns, deliveryMaxConcurrency); total > ceiling {
		total = ceiling
	}

	perHost := deliveryMaxConcurrency
	if perHost < MinIdleConnsPerHost {
		perHost = MinIdleConnsPerHost
	}
	// Depth can't exceed the total pool — a per-host limit above the total
	// would silently never be reachable.
	if perHost > total {
		perHost = total
	}

	return PoolSizing{
		MaxIdleConns:        total,
		MaxIdleConnsPerHost: perHost,
	}
}

// SizeSingleHostPool sizes a pool for a client that talks to one host (the
// hookdeck provider). It needs depth, not breadth, so the total is the
// per-host value rather than a fan-out ceiling.
func SizeSingleHostPool(deliveryMaxConcurrency int) PoolSizing {
	perHost := deliveryMaxConcurrency
	if perHost < MinIdleConnsPerHost {
		perHost = MinIdleConnsPerHost
	}
	return PoolSizing{
		MaxIdleConns:        perHost,
		MaxIdleConnsPerHost: perHost,
	}
}
