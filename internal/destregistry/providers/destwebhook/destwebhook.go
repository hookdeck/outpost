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
	*destregistry.BaseProvider
}

type WebhookDestinationConfig struct {
	URL string
}

var _ destregistry.Provider = (*WebhookDestination)(nil)

func New() (*WebhookDestination, error) {
	base, err := destregistry.NewBaseProvider("webhook")
	if err != nil {
		return nil, err
	}
	return &WebhookDestination{BaseProvider: base}, nil
}

func (d *WebhookDestination) Validate(ctx context.Context, destination *models.Destination) error {
	return d.BaseProvider.Validate(ctx, destination)
}

func (d *WebhookDestination) Publish(ctx context.Context, destination *models.Destination, event *models.Event) error {
	config, err := d.resolveConfig(ctx, destination)
	if err != nil {
		return destregistry.NewErrDestinationPublish(err)
	}
	if err := makeRequest(ctx, config.URL, event); err != nil {
		return destregistry.NewErrDestinationPublish(err)
	}
	return nil
}

func (d *WebhookDestination) resolveConfig(ctx context.Context, destination *models.Destination) (*WebhookDestinationConfig, error) {
	if err := d.BaseProvider.Validate(ctx, destination); err != nil {
		return nil, err
	}
	return &WebhookDestinationConfig{
		URL: destination.Config["url"],
	}, nil
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
