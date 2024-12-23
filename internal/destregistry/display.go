package destregistry

import (
	"github.com/hookdeck/outpost/internal/models"
)

// DestinationDisplay represents a destination with display-specific fields
type DestinationDisplay struct {
	*models.Destination
	Target string `json:"target"`
}
