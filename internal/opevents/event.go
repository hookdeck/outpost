package opevents

import (
	"encoding/json"
	"time"
)

// Topic constants for operation events.
const (
	TopicAlertConsecutiveFailure   = "alert.destination.consecutive_failure"
	TopicAlertDestinationDisabled  = "alert.destination.disabled"
	TopicAlertExhaustedRetries     = "alert.event.exhausted_retries"
	TopicTenantSubscriptionUpdated = "tenant.subscription.updated"
)

// OperationEvent is the envelope for all operation events emitted by Outpost.
type OperationEvent struct {
	ID           string          `json:"id"`
	Topic        string          `json:"topic"`
	Time         time.Time       `json:"time"`
	DeploymentID string          `json:"deployment_id,omitempty"`
	TenantID     string          `json:"tenant_id,omitempty"`
	Data         json.RawMessage `json:"data"`
}
