package destregistry

import (
	"time"

	"github.com/hookdeck/outpost/internal/models"
)

// DestinationDisplay represents a destination with display-specific fields
type DestinationDisplay struct {
	*models.Destination
	DestinationTarget
	// ExpiresAt is set for destination types whose API exposes it.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type DestinationTarget struct {
	Target    string `json:"target"`
	TargetURL string `json:"target_url,omitempty"`
}
