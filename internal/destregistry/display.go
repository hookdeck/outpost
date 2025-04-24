package destregistry

import (
	"github.com/hookdeck/outpost/internal/models"
)

// DestinationDisplay represents a destination with display-specific fields
type DestinationDisplay struct {
	*models.Destination
	DestinationTarget
}

type DestinationTarget struct {
	Target    string `json:"target"`
	TargetURL string `json:"target_url,omitempty"`
}
