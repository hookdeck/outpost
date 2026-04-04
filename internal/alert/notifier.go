package alert

import (
	"time"

	"github.com/hookdeck/outpost/internal/models"
)

// AlertDestination is the destination data included in alert event payloads.
type AlertDestination struct {
	ID         string        `json:"id" redis:"id"`
	TenantID   string        `json:"tenant_id" redis:"-"`
	Type       string        `json:"type" redis:"type"`
	Topics     models.Topics `json:"topics" redis:"-"`
	Config     models.Config `json:"config" redis:"-"`
	CreatedAt  time.Time     `json:"created_at" redis:"created_at"`
	DisabledAt *time.Time    `json:"disabled_at" redis:"disabled_at"`
}

// AlertDestinationFromDestination converts a models.Destination to an AlertDestination.
func AlertDestinationFromDestination(d *models.Destination) *AlertDestination {
	return &AlertDestination{
		ID:         d.ID,
		TenantID:   d.TenantID,
		Type:       d.Type,
		Topics:     d.Topics,
		Config:     d.Config,
		CreatedAt:  d.CreatedAt,
		DisabledAt: d.DisabledAt,
	}
}

// ConsecutiveFailures represents the nested consecutive failure state.
type ConsecutiveFailures struct {
	Current   int `json:"current"`
	Max       int `json:"max"`
	Threshold int `json:"threshold"`
}

// ConsecutiveFailureData is the data payload for alert.destination.consecutive_failure events.
type ConsecutiveFailureData struct {
	TenantID            string              `json:"tenant_id"`
	Event               *models.Event       `json:"event"`
	Attempt             *models.Attempt     `json:"attempt"`
	Destination         *AlertDestination   `json:"destination"`
	ConsecutiveFailures ConsecutiveFailures `json:"consecutive_failures"`
}
