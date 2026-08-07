package destregistry

// Connection pool sizing for outbound delivery clients.
//
// Go's defaults are wrong for this workload in both directions: two idle
// connections per host means reuse collapses above two concurrent deliveries
// to a destination, and a 100 connection total means destinations evict each
// other in any fan-out deployment. Neither is exposed as configuration —
// the correct total depends on the active destination count and the host's
// file descriptor limit, so the process sizes itself instead.

const (
	// MinTotalIdleConns is the floor for the total idle pool. Matches Go's
	// default, so a very restrictive FD limit never sizes us below stock
	// behavior.
	MinTotalIdleConns = 100

	// MaxTotalIdleConns caps the total idle pool. Beyond this the FD cost
	// (one per idle connection, held for up to IdleConnTimeout) stops buying
	// meaningful reuse for realistic destination counts.
	MaxTotalIdleConns = 4096

	// MinIdleConnsPerHost is the floor for per-host depth, matching Go's
	// default. DELIVERY_MAX_CONCURRENCY defaults to 1, which would otherwise
	// size us below stock behavior.
	MinIdleConnsPerHost = 2

	// fdLimitShare is the fraction of the host's soft FD limit the idle pool
	// may claim. The rest is left for database, queue, Redis and listener
	// descriptors, which share the same per-process budget.
	fdLimitShare = 4

	// fallbackFDLimit is assumed when RLIMIT_NOFILE can't be read (Windows,
	// or a failing syscall). Conservative on purpose: the common Unix soft
	// default.
	fallbackFDLimit = 1024
)

// PoolSizing is the resolved connection pool configuration, along with the FD
// limit it was derived from so the value can be logged at startup.
type PoolSizing struct {
	// MaxIdleConns bounds breadth — how many distinct destinations can hold a
	// warm connection at all.
	MaxIdleConns int

	// MaxIdleConnsPerHost bounds depth — how many warm connections a single
	// destination keeps.
	MaxIdleConnsPerHost int

	// FDLimit is the soft RLIMIT_NOFILE the total was derived from, or
	// fallbackFDLimit when it could not be read.
	FDLimit int

	// FDLimitKnown reports whether FDLimit came from the OS rather than the
	// fallback.
	FDLimitKnown bool
}

// SizeFanOutPool sizes a pool for a client that talks to arbitrarily many
// destination hosts (the webhook providers). Depth comes from the delivery
// worker pool — it caps how many deliveries can be in flight, so it caps how
// many connections one destination could need. Breadth comes from the host's
// FD limit, since that is what actually bounds the total.
//
// deliveryMaxConcurrency <= 0 means "unknown"; the per-host floor applies.
func SizeFanOutPool(deliveryMaxConcurrency int) PoolSizing {
	fdLimit, known := softFDLimit()
	if fdLimit <= 0 {
		fdLimit, known = fallbackFDLimit, false
	}

	total := fdLimit / fdLimitShare
	if total < MinTotalIdleConns {
		total = MinTotalIdleConns
	}
	if total > MaxTotalIdleConns {
		total = MaxTotalIdleConns
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
		FDLimit:             fdLimit,
		FDLimitKnown:        known,
	}
}

// SizeSingleHostPool sizes a pool for a client that talks to one host (the
// hookdeck provider). It needs depth, not breadth, so the total is the
// per-host value rather than an FD-derived ceiling.
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
