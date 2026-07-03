package opevents

import (
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"go.uber.org/zap"
)

// Operator-event payloads: the wire contract for each topic. The typed
// constructors below are the only way these events are built, keeping topic,
// tenant, and payload shape in one place.

// TenantSubscriptionUpdatedData is the data payload for
// tenant.subscription.updated events.
type TenantSubscriptionUpdatedData struct {
	TenantID                  string   `json:"tenant_id"`
	Topics                    []string `json:"topics"`
	PreviousTopics            []string `json:"previous_topics"`
	DestinationsCount         int      `json:"destinations_count"`
	PreviousDestinationsCount int      `json:"previous_destinations_count"`
}

// TenantSubscriptionUpdatedEvent builds the tenant.subscription.updated event.
func TenantSubscriptionUpdatedEvent(data TenantSubscriptionUpdatedData) Event {
	return Event{
		Topic:    TopicTenantSubscriptionUpdated,
		TenantID: data.TenantID,
		Data:     data,
	}
}

// AlertDestination is the destination projection included in alert payloads.
type AlertDestination struct {
	ID         string        `json:"id"`
	TenantID   string        `json:"tenant_id"`
	Type       string        `json:"type"`
	Topics     models.Topics `json:"topics"`
	Config     models.Config `json:"config"`
	CreatedAt  time.Time     `json:"created_at"`
	DisabledAt *time.Time    `json:"disabled_at"`
}

// NewAlertDestination projects a models.Destination into the payload shape.
func NewAlertDestination(d *models.Destination) *AlertDestination {
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

// attemptLogFields is the delivery-audit log context shared by the
// per-attempt event constructors.
func attemptLogFields(dest *AlertDestination, event *models.Event, attempt *models.Attempt) []zap.Field {
	return []zap.Field{
		zap.String("attempt_id", attempt.ID),
		zap.String("event_id", event.ID),
		zap.String("destination_id", dest.ID),
		zap.String("destination_type", dest.Type),
	}
}

// ConsecutiveFailures represents the nested consecutive failure state.
type ConsecutiveFailures struct {
	Current   int `json:"current"`
	Max       int `json:"max"`
	Threshold int `json:"threshold"`
}

// DestinationDisabledData is the data payload for alert.destination.disabled events.
type DestinationDisabledData struct {
	TenantID    string            `json:"tenant_id"`
	Destination *AlertDestination `json:"destination"`
	DisabledAt  time.Time         `json:"disabled_at"`
	Reason      string            `json:"reason"`
	Event       *models.Event     `json:"event"`
	Attempt     *models.Attempt   `json:"attempt"`
}

// ConsecutiveFailureData is the data payload for alert.destination.consecutive_failure events.
type ConsecutiveFailureData struct {
	TenantID            string              `json:"tenant_id"`
	Event               *models.Event       `json:"event"`
	Attempt             *models.Attempt     `json:"attempt"`
	Destination         *AlertDestination   `json:"destination"`
	ConsecutiveFailures ConsecutiveFailures `json:"consecutive_failures"`
}

// ExhaustedRetriesData is the data payload for alert.attempt.exhausted_retries events.
type ExhaustedRetriesData struct {
	TenantID    string            `json:"tenant_id"`
	Event       *models.Event     `json:"event"`
	Attempt     *models.Attempt   `json:"attempt"`
	Destination *AlertDestination `json:"destination"`
}

// ConsecutiveFailureEvent builds the alert.destination.consecutive_failure event.
func ConsecutiveFailureEvent(dest *AlertDestination, event *models.Event, attempt *models.Attempt, current, max, threshold int) Event {
	return Event{
		Topic:     TopicAlertConsecutiveFailure,
		TenantID:  dest.TenantID,
		LogFields: attemptLogFields(dest, event, attempt),
		Data: ConsecutiveFailureData{
			TenantID:    dest.TenantID,
			Event:       event,
			Attempt:     attempt,
			Destination: dest,
			ConsecutiveFailures: ConsecutiveFailures{
				Current:   current,
				Max:       max,
				Threshold: threshold,
			},
		},
	}
}

// DestinationDisabledEvent builds the alert.destination.disabled event.
func DestinationDisabledEvent(dest *AlertDestination, event *models.Event, attempt *models.Attempt, disabledAt time.Time) Event {
	return Event{
		Topic:     TopicAlertDestinationDisabled,
		TenantID:  dest.TenantID,
		LogFields: attemptLogFields(dest, event, attempt),
		Data: DestinationDisabledData{
			TenantID:    dest.TenantID,
			Destination: dest,
			DisabledAt:  disabledAt,
			Reason:      "consecutive_failure",
			Event:       event,
			Attempt:     attempt,
		},
	}
}

// ExhaustedRetriesEvent builds the alert.attempt.exhausted_retries event.
func ExhaustedRetriesEvent(dest *AlertDestination, event *models.Event, attempt *models.Attempt) Event {
	return Event{
		Topic:     TopicAlertExhaustedRetries,
		TenantID:  dest.TenantID,
		LogFields: attemptLogFields(dest, event, attempt),
		Data: ExhaustedRetriesData{
			TenantID:    dest.TenantID,
			Event:       event,
			Attempt:     attempt,
			Destination: dest,
		},
	}
}

// AttemptData is the data payload for attempt.success and attempt.failed
// events. The two topics share one shape — the split exists for subscription
// filtering, and Attempt.Status carries the outcome.
type AttemptData struct {
	TenantID    string            `json:"tenant_id"`
	Event       *models.Event     `json:"event"`
	Attempt     *models.Attempt   `json:"attempt"`
	Destination *AlertDestination `json:"destination"`
}

// AttemptSuccessEvent builds the attempt.success event.
func AttemptSuccessEvent(dest *AlertDestination, event *models.Event, attempt *models.Attempt) Event {
	return Event{
		Topic:     TopicAttemptSuccess,
		TenantID:  dest.TenantID,
		LogFields: attemptLogFields(dest, event, attempt),
		Data: AttemptData{
			TenantID:    dest.TenantID,
			Event:       event,
			Attempt:     attempt,
			Destination: dest,
		},
	}
}

// AttemptFailedEvent builds the attempt.failed event.
func AttemptFailedEvent(dest *AlertDestination, event *models.Event, attempt *models.Attempt) Event {
	return Event{
		Topic:     TopicAttemptFailed,
		TenantID:  dest.TenantID,
		LogFields: attemptLogFields(dest, event, attempt),
		Data: AttemptData{
			TenantID:    dest.TenantID,
			Event:       event,
			Attempt:     attempt,
			Destination: dest,
		},
	}
}
