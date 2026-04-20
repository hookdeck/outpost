package publisher

import (
	"encoding/json"
	"strings"
	"time"
)

type eventPayload struct {
	EventID  string `json:"event_id"`
	TenantID string `json:"tenant_id"`
	Time     string `json:"time"`
	Filler   string `json:"filler,omitempty"`
}

func generatePayload(eventID, tenantID string, targetBytes int) []byte {
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
