package webhook

import (
	"context"
	"errors"

	"github.com/hookdeck/EventKit/internal/destinationadapter/adapters"
	"github.com/hookdeck/EventKit/internal/ingest"
)

type WebhookDestination struct {
}

type WebhookDestinationConfig struct {
	URL string
}

var _ adapters.DestinationAdapter = (*WebhookDestination)(nil)

func New() *WebhookDestination {
	return &WebhookDestination{}
}

func (d *WebhookDestination) Validate(ctx context.Context, destination adapters.DestinationAdapterValue) error {
	webhookDestinationConfig := WebhookDestinationConfig{
		URL: destination.Config["url"],
	}

	if webhookDestinationConfig.URL == "" {
		return errors.New("url is required for webhook destination config")
	}

	return nil
}

func (d *WebhookDestination) Publish(ctx context.Context, destination adapters.DestinationAdapterValue, event *ingest.Event) error {
	return nil
}
