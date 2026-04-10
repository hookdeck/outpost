package opevents

import (
	"encoding/json"
	"time"
)

// Topic constants for operator events.
const (
	TopicAlertConsecutiveFailure   = "alert.destination.consecutive_failure"
	TopicAlertDestinationDisabled  = "alert.destination.disabled"
	TopicAlertExhaustedRetries     = "alert.attempt.exhausted_retries"
	TopicTenantSubscriptionUpdated = "tenant.subscription.updated"
)

// OperatorEvent is the envelope for all operator events emitted by Outpost.
type OperatorEvent struct {
	ID           string          `json:"id"`
	Topic        string          `json:"topic"`
	Time         time.Time       `json:"time"`
	DeploymentID string          `json:"deployment_id,omitempty"`
	TenantID     string          `json:"tenant_id,omitempty"`
	Data         json.RawMessage `json:"data"`
}
