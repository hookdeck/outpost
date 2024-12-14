package destwebhook

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

func New(loader metadata.MetadataLoader, opts ...Option) (*WebhookDestination, error) {
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

// ObfuscateDestination overrides the base implementation to handle webhook secrets
func (d *WebhookDestination) ObfuscateDestination(destination *models.Destination) *models.Destination {
	result := *destination // shallow copy
	result.Config = make(map[string]string, len(destination.Config))
	result.Credentials = make(map[string]string, len(destination.Credentials))

	// Copy config values using base provider's logic
	for key, value := range destination.Config {
		result.Config[key] = value
	}

	// Copy credentials as is
	// NOTE: Webhook secrets are intentionally not obfuscated for now because:
	// 1. They're needed for secret rotation logic
	// 2. They're less security-critical than other provider credentials (e.g. AWS keys)
	// TODO: Implement proper secret obfuscation later if needed
	for key, value := range destination.Credentials {
		result.Credentials[key] = value
	}

	return &result
}

func (d *WebhookDestination) Validate(ctx context.Context, destination *models.Destination) error {
	if _, _, err := d.resolveConfig(ctx, destination); err != nil {
		return err
	}
	return nil
}

func (d *WebhookDestination) CreatePublisher(ctx context.Context, destination *models.Destination) (destregistry.Publisher, error) {
	config, creds, err := d.resolveConfig(ctx, destination)
	if err != nil {
		return nil, err
	}
	return &WebhookPublisher{
		BasePublisher: &destregistry.BasePublisher{},
		url:           config.URL,
		headerPrefix:  d.headerPrefix,
		secrets:       creds.Secrets,
		timeout:       d.timeout,
	}, nil
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

type WebhookPublisher struct {
	*destregistry.BasePublisher
	url          string
	headerPrefix string
	secrets      []WebhookSecret
	timeout      time.Duration
}

func (p *WebhookPublisher) Close() error {
	p.BasePublisher.StartClose()
	return nil
}

func (p *WebhookPublisher) Publish(ctx context.Context, event *models.Event) error {
	if err := p.BasePublisher.StartPublish(); err != nil {
		return err
	}
	defer p.BasePublisher.FinishPublish()

	rawBody, err := json.Marshal(event.Data)
	if err != nil {
		return fmt.Errorf("failed to marshal event data: %w", err)
	}

	// Create a context with timeout
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()

	webhookReq := NewWebhookRequest(p.url, rawBody, event.Metadata, p.headerPrefix, p.secrets)
	httpReq, err := webhookReq.ToHTTPRequest(ctx)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return destregistry.NewErrDestinationPublishAttempt(err, "webhook", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return destregistry.NewErrDestinationPublishAttempt(fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(bodyBytes)), "webhook", map[string]interface{}{
			"status": resp.StatusCode,
			"body":   string(bodyBytes),
		})
	}

	return nil
}
