package adapters

import (
	"context"

	"github.com/hookdeck/EventKit/internal/ingest"
)

type DestinationAdapter interface {
	Validate(ctx context.Context, destination DestinationAdapterValue) error
	Publish(ctx context.Context, destination DestinationAdapterValue, event *ingest.Event) error
}

type DestinationAdapterValue struct {
	ID          string
	Type        string
	Config      map[string]string
	Credentials map[string]string
}
