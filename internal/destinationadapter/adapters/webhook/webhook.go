package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/hookdeck/EventKit/internal/destinationadapter/adapters"
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
	_, err := parseConfig(destination)
	return err
}

func (d *WebhookDestination) Publish(ctx context.Context, destination adapters.DestinationAdapterValue, event *adapters.Event) error {
	config, err := parseConfig(destination)
	if err != nil {
		return err
	}
	return makeRequest(ctx, config.URL, event.Data)
}

func parseConfig(destination adapters.DestinationAdapterValue) (*WebhookDestinationConfig, error) {
	if destination.Type != "webhooks" {
		return nil, errors.New("invalid destination type")
	}

	destinationConfig := &WebhookDestinationConfig{
		URL: destination.Config["url"],
	}

	if destinationConfig.URL == "" {
		return nil, errors.New("url is required for webhook destination config")
	}

	return destinationConfig, nil
}

func makeRequest(ctx context.Context, url string, data map[string]interface{}) error {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(dataBytes))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	return nil
}
