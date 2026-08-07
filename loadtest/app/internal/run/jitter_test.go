package run

import (
	"fmt"
	"strings"
	"testing"
)

// jitterSpec builds a one-profile spec with the two jitters substituted in, so
// each case below differs only in the values under test.
func jitterSpec(payload, payloadJitter, responseMs, responseJitter int) string {
	return fmt.Sprintf(`
name: jittered
target: direct
concurrency_budget: 1000
warmup: 30s
window: 2m
drain: 1m
profiles:
  - name: baseline
    tenants: 4
    destinations_per_tenant: 1
    rate_per_tenant: 25
    payload_bytes: %d
    payload_jitter_bytes: %d
    response_ms: %d
    response_jitter_ms: %d
`, payload, payloadJitter, responseMs, responseJitter)
}

func TestJittersReachTheGroupConfig(t *testing.T) {
	s := mustParse(t, jitterSpec(65536, 16384, 10000, 2000))
	if err := s.Validate(); err != nil {
		t.Fatalf("spec should be valid: %v", err)
	}

	cfg := s.Profiles[0].GroupConfig()
	if got := cfg.Publish.PayloadJitterBytes; got != 16384 {
		t.Errorf("payload jitter = %d, want 16384", got)
	}
	if got := cfg.MockProfile.JitterMs; got != 2000 {
		t.Errorf("response jitter = %d, want 2000", got)
	}
}

// Symmetric jitter wider than its centre would spend half its draws clamped at
// the floor, which is a narrower spread than the spec asked for and no error.
func TestJitterWiderThanItsCentreIsRejected(t *testing.T) {
	for _, tc := range []struct{ name, spec, want string }{
		{"payload", jitterSpec(1024, 2048, 250, 50), "payload_jitter_bytes"},
		{"response", jitterSpec(1024, 256, 250, 500), "response_jitter_ms"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := mustParse(t, tc.spec).Validate()
			if err == nil {
				t.Fatal("jitter wider than its centre should be rejected")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error does not name %s: %v", tc.want, err)
			}
		})
	}
}

// Bandwidth is reported from the mean, so a symmetric jitter must not move it —
// the budget check would otherwise change meaning with the spread.
func TestPayloadJitterDoesNotChangeBudget(t *testing.T) {
	plain := mustParse(t, jitterSpec(65536, 0, 250, 0)).Budget()
	wide := mustParse(t, jitterSpec(65536, 32768, 250, 0)).Budget()

	if plain.BytesPerSec != wide.BytesPerSec {
		t.Errorf("bytes/s changed with jitter: %.0f then %.0f",
			plain.BytesPerSec, wide.BytesPerSec)
	}
}

func TestZeroJitterIsValid(t *testing.T) {
	if err := mustParse(t, jitterSpec(1024, 0, 250, 0)).Validate(); err != nil {
		t.Fatalf("omitted jitter should be valid: %v", err)
	}
}
