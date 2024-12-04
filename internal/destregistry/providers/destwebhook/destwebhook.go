package destwebhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/models"
)

type WebhookDestination struct {
	*destregistry.BaseProvider
	timeout      time.Duration
	headerPrefix string
}

type WebhookDestinationConfig struct {
	URL string
}

type WebhookSecret struct {
	Key       string    `json:"key"`
	CreatedAt time.Time `json:"created_at"`
}

type WebhookDestinationCredentials struct {
	Secrets []WebhookSecret
}

var _ destregistry.Provider = (*WebhookDestination)(nil)

// Option is a functional option for configuring WebhookDestination
type Option func(*WebhookDestination)

// WithTimeout sets a custom timeout for the webhook requests
func WithTimeout(seconds int) Option {
	return func(w *WebhookDestination) {
		w.timeout = time.Duration(seconds) * time.Second
	}
}

// WithHeaderPrefix sets a custom prefix for webhook request headers
func WithHeaderPrefix(prefix string) Option {
	return func(w *WebhookDestination) {
		w.headerPrefix = prefix
	}
}

func New(loader *metadata.MetadataLoader, opts ...Option) (*WebhookDestination, error) {
	base, err := destregistry.NewBaseProvider(loader, "webhook")
	if err != nil {
		return nil, err
	}
	destination := &WebhookDestination{BaseProvider: base, timeout: 30 * time.Second, headerPrefix: "x-outpost-"}
	for _, opt := range opts {
		opt(destination)
	}
	return destination, nil
}

func (d *WebhookDestination) Validate(ctx context.Context, destination *models.Destination) error {
	if _, _, err := d.resolveConfig(ctx, destination); err != nil {
		return err
	}
	return nil
}

func (d *WebhookDestination) Publish(ctx context.Context, destination *models.Destination, event *models.Event) error {
	config, creds, err := d.resolveConfig(ctx, destination)
	if err != nil {
		return destregistry.NewErrDestinationPublish(err)
	}

	rawBody, err := json.Marshal(event.Data)
	if err != nil {
		return fmt.Errorf("failed to marshal event data: %w", err)
	}

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()

	webhookReq := NewWebhookRequest(config.URL, rawBody, event.Metadata, d.headerPrefix, creds.Secrets)
	httpReq, err := webhookReq.ToHTTPRequest(ctx)
	if err != nil {
		return destregistry.NewErrDestinationPublish(err)
	}

	client := &http.Client{}
	resp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		log.Println(resp) // TODO: use proper logger
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

func (d *WebhookDestination) resolveConfig(ctx context.Context, destination *models.Destination) (*WebhookDestinationConfig, *WebhookDestinationCredentials, error) {
	if err := d.BaseProvider.Validate(ctx, destination); err != nil {
		return nil, nil, err
	}

	// Extract URL from destination config
	config := &WebhookDestinationConfig{
		URL: destination.Config["url"],
	}

	// Parse secrets from destination credentials
	var creds WebhookDestinationCredentials
	if secretsJson, ok := destination.Credentials["secrets"]; ok {
		if err := json.Unmarshal([]byte(secretsJson), &creds.Secrets); err != nil {
			return nil, nil, destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{{
				Field: "credentials.secrets",
				Type:  "invalid_format",
			}})
		}
	}

	return config, &creds, nil
}
