package publisher

import (
	"encoding/json"
	"math/rand"
	"strings"
	"time"
)

type eventPayload struct {
	EventID  string `json:"event_id"`
	TenantID string `json:"tenant_id"`
	Time     string `json:"time"`
	Filler   string `json:"filler,omitempty"`
}

// generatePayload builds one event of roughly targetBytes, varied uniformly by
// ±jitterBytes. The jitter is symmetric, so the mean size — and with it the
// spec's bandwidth estimate — is targetBytes regardless of how wide it is.
//
// A fixed size is the unrealistic case: every event compresses the same, hits
// the same allocation size class, and lands in the same buffer bucket the whole
// way down. Varying it is what stops a run from measuring one lucky size.
func generatePayload(eventID, tenantID string, targetBytes, jitterBytes int) []byte {
	if jitterBytes > 0 {
		targetBytes += rand.Intn(2*jitterBytes+1) - jitterBytes
	}

	p := eventPayload{
		EventID:  eventID,
		TenantID: tenantID,
		Time:     time.Now().UTC().Format(time.RFC3339Nano),
	}

	// Marshal without filler to see base size
	base, _ := json.Marshal(p)
	if len(base) < targetBytes {
		// Add filler to reach target size
		fillerSize := targetBytes - len(base) - 12 // account for "filler":"..."
		if fillerSize > 0 {
			p.Filler = strings.Repeat("x", fillerSize)
		}
	}

	data, _ := json.Marshal(p)
	return data
}
