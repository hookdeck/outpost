package adapters

import (
	"context"
	"time"
)

type Event struct {
	ID               string                 `json:"id"`
	TenantID         string                 `json:"tenant_id"`
	DestinationID    string                 `json:"destination_id"`
	Topic            string                 `json:"topic"`
	EligibleForRetry bool                   `json:"eligible_for_retry"`
	Time             time.Time              `json:"time"`
	Metadata         map[string]string      `json:"metadata"`
	Data             map[string]interface{} `json:"data"`
}

type DestinationAdapter interface {
	Validate(ctx context.Context, destination DestinationAdapterValue) error
	Publish(ctx context.Context, destination DestinationAdapterValue, event *Event) error
}

type DestinationAdapterValue struct {
	ID          string
	Type        string
	Config      map[string]string
	Credentials map[string]string
}
