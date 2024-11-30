package destwebhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/models"
)

type WebhookDestination struct {
}

type WebhookDestinationConfig struct {
	URL string
}

var _ destregistry.Provider = (*WebhookDestination)(nil)

func New() *WebhookDestination {
	return &WebhookDestination{}
}

func (d *WebhookDestination) Validate(ctx context.Context, destination *models.Destination) error {
	if _, err := parseConfig(destination); err != nil {
		return destregistry.NewErrDestinationValidation(err)
	}
	return nil
}

func (d *WebhookDestination) Publish(ctx context.Context, destination *models.Destination, event *models.Event) error {
	config, err := parseConfig(destination)
	if err != nil {
		return destregistry.NewErrDestinationPublish(err)
	}
	if err := makeRequest(ctx, config.URL, event); err != nil {
		return destregistry.NewErrDestinationPublish(err)
	}
	return nil
}

func parseConfig(destination *models.Destination) (*WebhookDestinationConfig, error) {
	if destination.Type != "webhook" {
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

func makeRequest(ctx context.Context, url string, event *models.Event) error {
	dataBytes, err := json.Marshal(event.Data)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(dataBytes))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	for key, value := range event.Metadata {
		req.Header.Set("x-outpost-"+key, value)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		// TODO: improve error handling to store response value
		// TODO: improve logger
		log.Println(resp)
		if bodyBytes, err := io.ReadAll(resp.Body); err == nil {
			bodyString := string(bodyBytes)
			log.Println("request error body:", bodyString)
		}
		return errors.New("request failed")
	}

	return nil
}
