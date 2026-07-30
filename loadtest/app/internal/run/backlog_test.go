package run

import "testing"

// The limits are checked against runs that actually happened, so the numbers
// below are observations rather than invented cases. A guard that fires on a
// healthy run is worse than no guard: it would void good measurements and
// train the operator to raise the limit until it never fires.
func TestInFlightLimit(t *testing.T) {
	cases := []struct {
		name string
		spec Spec
		// observed is the real steady-state in-flight figure where we have one.
		observed  int
		wantLimit int
	}{
		{
			// The 4h run: designed for 497 concurrent, held 564 all afternoon.
			name: "healthy 4h run sits far below its limit",
			spec: Spec{Profiles: []Profile{
				{TenantCount: 10, Destinations: 1, RatePerTenant: 40, ResponseMs: 250},
				{TenantCount: 10, Destinations: 5, RatePerTenant: 3, ResponseMs: 250},
				{TenantCount: 5, Destinations: 20, RatePerTenant: 2, ResponseMs: 250},
				{TenantCount: 5, Destinations: 1, RatePerTenant: 14, ResponseMs: 1000},
				{TenantCount: 5, Destinations: 1, RatePerTenant: 4, ResponseMs: 10000},
			}},
			observed:  564,
			wantLimit: 4575, // 10 x 457.5 design concurrency
		},
		{
			// A local four-profile check: design concurrency 13, so the ratio
			// alone would abort at 130 — well inside normal jitter.
			name: "small run gets the floor",
			spec: Spec{Profiles: []Profile{
				{TenantCount: 2, Destinations: 1, RatePerTenant: 5, ResponseMs: 250},
			}},
			wantLimit: inFlightLimitFloor,
		},
		{
			// 3x the 24h spec's 1025.75 design concurrency.
			name: "explicit ratio tightens the default",
			spec: Spec{MaxInFlightRatio: 3, Profiles: []Profile{
				{TenantCount: 10, Destinations: 1, RatePerTenant: 50, ResponseMs: 250},
				{TenantCount: 20, Destinations: 5, RatePerTenant: 5, ResponseMs: 250},
				{TenantCount: 5, Destinations: 20, RatePerTenant: 5, ResponseMs: 250},
				{TenantCount: 5, Destinations: 50, RatePerTenant: 2, ResponseMs: 250},
				{TenantCount: 8, Destinations: 1, RatePerTenant: 11, ResponseMs: 250},
				{TenantCount: 5, Destinations: 1, RatePerTenant: 11, ResponseMs: 250},
				{TenantCount: 5, Destinations: 1, RatePerTenant: 25, ResponseMs: 1000},
				{TenantCount: 5, Destinations: 1, RatePerTenant: 12, ResponseMs: 2000},
				{TenantCount: 5, Destinations: 1, RatePerTenant: 5, ResponseMs: 5000},
				{TenantCount: 4, Destinations: 1, RatePerTenant: 3, ResponseMs: 10000},
			}},
			wantLimit: 3077, // 3 x 1025.75
		},
		{
			// The floor must not quietly override a ratio the operator chose:
			// asking for 2x on a small run means 2x, not 1000.
			name: "explicit ratio is not raised to the floor",
			spec: Spec{MaxInFlightRatio: 2, Profiles: []Profile{
				{TenantCount: 2, Destinations: 1, RatePerTenant: 5, ResponseMs: 250},
			}},
			wantLimit: 5,
		},
		{
			name:      "explicit absolute wins over ratio",
			spec:      Spec{MaxInFlight: 42, MaxInFlightRatio: 3, Profiles: []Profile{{TenantCount: 10, Destinations: 1, RatePerTenant: 400, ResponseMs: 250}}},
			wantLimit: 42,
		},
		{
			name:      "explicit value wins",
			spec:      Spec{MaxInFlight: 42, Profiles: []Profile{{TenantCount: 10, Destinations: 1, RatePerTenant: 400, ResponseMs: 250}}},
			wantLimit: 42,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.spec.InFlightLimit()
			if got != tc.wantLimit {
				t.Fatalf("InFlightLimit() = %d, want %d", got, tc.wantLimit)
			}
			if tc.observed > 0 && tc.observed >= got {
				t.Fatalf("limit %d would fire on a healthy run that held %d in flight",
					got, tc.observed)
			}
		})
	}
}

// The smoke run that took out the delivery pipeline: the guard has to fire
// while the backlog is still five figures, not six.
func TestInFlightLimitCatchesRunawayBacklog(t *testing.T) {
	spec := Spec{Profiles: []Profile{
		{TenantCount: 10, Destinations: 1, RatePerTenant: 50, ResponseMs: 250},
		{TenantCount: 20, Destinations: 5, RatePerTenant: 5, ResponseMs: 250},
		{TenantCount: 5, Destinations: 20, RatePerTenant: 5, ResponseMs: 250},
		{TenantCount: 5, Destinations: 50, RatePerTenant: 2, ResponseMs: 250},
		{TenantCount: 5, Destinations: 1, RatePerTenant: 25, ResponseMs: 1000},
		{TenantCount: 5, Destinations: 1, RatePerTenant: 12, ResponseMs: 2000},
		{TenantCount: 5, Destinations: 1, RatePerTenant: 5, ResponseMs: 5000},
		{TenantCount: 4, Destinations: 1, RatePerTenant: 3, ResponseMs: 10000},
	}}

	limit := spec.InFlightLimit()
	const (
		observedAtCollapse = 70958 // in flight when deliveries reached zero
		observedPeak       = 82760
	)
	if limit >= observedAtCollapse {
		t.Fatalf("limit %d does not fire before the pipeline collapses at %d",
			limit, observedAtCollapse)
	}
	// It should also fire with a lot of room to spare, not moments before.
	if limit > observedPeak/4 {
		t.Fatalf("limit %d fires too late — peak backlog was %d", limit, observedPeak)
	}
}
