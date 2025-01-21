package destwebhook

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/models"
)

const (
	DefaultEncoding  = "hex"
	DefaultAlgorithm = "hmac-sha256"
)

type WebhookDestination struct {
	*destregistry.BaseProvider
	headerPrefix             string
	signatureContentTemplate string
	signatureHeaderTemplate  string
	disableEventIDHeader     bool
	disableSignatureHeader   bool
	disableTimestampHeader   bool
	disableTopicHeader       bool
	encoding                 string
	algorithm                string
}

type WebhookDestinationConfig struct {
	URL string
}

type WebhookSecret struct {
	Key       string     `json:"key"`
	CreatedAt time.Time  `json:"created_at"`
	InvalidAt *time.Time `json:"invalid_at,omitempty"`
}

type WebhookDestinationCredentials struct {
	Secret                  string    `json:"secret"`
	PreviousSecret          string    `json:"previous_secret,omitempty"`
	PreviousSecretInvalidAt time.Time `json:"previous_secret_invalid_at,omitempty"`
}

var _ destregistry.Provider = (*WebhookDestination)(nil)

// Option is a functional option for configuring WebhookDestination
type Option func(*WebhookDestination)

// WithHeaderPrefix sets a custom prefix for webhook request headers
func WithHeaderPrefix(prefix string) Option {
	return func(w *WebhookDestination) {
		w.headerPrefix = prefix
	}
}

// Add these options after the existing Option definitions
func WithDisableDefaultEventIDHeader(disable bool) Option {
	return func(w *WebhookDestination) {
		w.disableEventIDHeader = disable
	}
}

func WithDisableDefaultSignatureHeader(disable bool) Option {
	return func(w *WebhookDestination) {
		w.disableSignatureHeader = disable
	}
}

func WithDisableDefaultTimestampHeader(disable bool) Option {
	return func(w *WebhookDestination) {
		w.disableTimestampHeader = disable
	}
}

func WithDisableDefaultTopicHeader(disable bool) Option {
	return func(w *WebhookDestination) {
		w.disableTopicHeader = disable
	}
}

func WithSignatureContentTemplate(template string) Option {
	return func(w *WebhookDestination) {
		w.signatureContentTemplate = template
	}
}

func WithSignatureHeaderTemplate(template string) Option {
	return func(w *WebhookDestination) {
		w.signatureHeaderTemplate = template
	}
}

func WithSignatureEncoding(encoding string) Option {
	return func(w *WebhookDestination) {
		w.encoding = encoding
	}
}

func WithSignatureAlgorithm(algorithm string) Option {
	return func(w *WebhookDestination) {
		w.algorithm = algorithm
	}
}

func New(loader metadata.MetadataLoader, opts ...Option) (*WebhookDestination, error) {
	base, err := destregistry.NewBaseProvider(loader, "webhook")
	if err != nil {
		return nil, err
	}
	destination := &WebhookDestination{
		BaseProvider: base,
		headerPrefix: "x-outpost-",
		encoding:     DefaultEncoding,
		algorithm:    DefaultAlgorithm,
	}
	for _, opt := range opts {
		opt(destination)
	}
	return destination, nil
}

func (d *WebhookDestination) ComputeTarget(destination *models.Destination) string {
	return destination.Config["url"]
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

func (d *WebhookDestination) GetSignatureEncoding() string {
	return d.encoding
}

func (d *WebhookDestination) GetSignatureAlgorithm() string {
	return d.algorithm
}

func (d *WebhookDestination) CreatePublisher(ctx context.Context, destination *models.Destination) (destregistry.Publisher, error) {
	config, creds, err := d.resolveConfig(ctx, destination)
	if err != nil {
		return nil, err
	}

	// Convert credentials to WebhookSecret format
	now := time.Now()
	secrets := []WebhookSecret{
		{
			Key:       creds.Secret,
			CreatedAt: now,
		},
	}

	if creds.PreviousSecret != "" {
		secrets = append(secrets, WebhookSecret{
			Key:       creds.PreviousSecret,
			CreatedAt: now.Add(-1 * time.Hour), // Set to 1 hour before current secret
			InvalidAt: &creds.PreviousSecretInvalidAt,
		})
	}

	sm := NewSignatureManager(
		secrets,
		WithSignatureFormatter(NewSignatureFormatter(d.signatureContentTemplate)),
		WithHeaderFormatter(NewHeaderFormatter(d.signatureHeaderTemplate)),
		WithEncoder(GetEncoder(d.encoding)),
		WithAlgorithm(GetAlgorithm(d.algorithm)),
	)

	return &WebhookPublisher{
		BasePublisher:          &destregistry.BasePublisher{},
		url:                    config.URL,
		headerPrefix:           d.headerPrefix,
		secrets:                secrets,
		sm:                     sm,
		disableEventIDHeader:   d.disableEventIDHeader,
		disableSignatureHeader: d.disableSignatureHeader,
		disableTimestampHeader: d.disableTimestampHeader,
		disableTopicHeader:     d.disableTopicHeader,
	}, nil
}

func (d *WebhookDestination) resolveConfig(ctx context.Context, destination *models.Destination) (*WebhookDestinationConfig, *WebhookDestinationCredentials, error) {
	if err := d.BaseProvider.Validate(ctx, destination); err != nil {
		return nil, nil, err
	}

	config := &WebhookDestinationConfig{
		URL: destination.Config["url"],
	}

	// Parse credentials directly from map
	creds := &WebhookDestinationCredentials{
		Secret:         destination.Credentials["secret"],
		PreviousSecret: destination.Credentials["previous_secret"],
	}

	// Skip validation if no relevant credentials are passed
	if destination.Credentials["secret"] == "" &&
		destination.Credentials["previous_secret"] == "" &&
		destination.Credentials["previous_secret_invalid_at"] == "" {
		return config, creds, nil
	}

	// If any credentials are passed, secret is required
	if creds.Secret == "" {
		return nil, nil, destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{{
			Field: "credentials.secret",
			Type:  "required",
		}})
	}

	// Parse previous_secret_invalid_at if present
	if invalidAtStr := destination.Credentials["previous_secret_invalid_at"]; invalidAtStr != "" {
		invalidAt, err := time.Parse(time.RFC3339, invalidAtStr)
		if err != nil {
			return nil, nil, destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{{
				Field: "credentials.previous_secret_invalid_at",
				Type:  "pattern",
			}})
		}
		creds.PreviousSecretInvalidAt = invalidAt
	}

	// If previous secret is provided, validate invalidation time
	if creds.PreviousSecret != "" && creds.PreviousSecretInvalidAt.IsZero() {
		return nil, nil, destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{{
			Field: "credentials.previous_secret_invalid_at",
			Type:  "required",
		}})
	}

	// If previous_secret_invalid_at is provided, validate previous_secret
	if !creds.PreviousSecretInvalidAt.IsZero() && creds.PreviousSecret == "" {
		return nil, nil, destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{{
			Field: "credentials.previous_secret",
			Type:  "required",
		}})
	}

	return config, creds, nil
}

// rotateSecret handles secret rotation and returns clean credentials
func (d *WebhookDestination) rotateSecret(newDest, origDest *models.Destination) (map[string]string, error) {
	if origDest == nil {
		return nil, destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
			{
				Field: "credentials.rotate_secret",
				Type:  "invalid",
			},
		})
	}

	if origDest.Credentials["secret"] == "" {
		return nil, destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
			{
				Field: "credentials.secret",
				Type:  "required",
			},
		})
	}

	creds := make(map[string]string)

	// Store the current secret as the previous secret
	creds["previous_secret"] = origDest.Credentials["secret"]

	// Generate a new secret
	secret, err := generateSignatureSecret()
	if err != nil {
		return nil, err
	}
	creds["secret"] = secret

	// Keep custom invalidation time if provided, otherwise set default
	if newDest.Credentials["previous_secret_invalid_at"] != "" {
		creds["previous_secret_invalid_at"] = newDest.Credentials["previous_secret_invalid_at"]
	} else {
		creds["previous_secret_invalid_at"] = time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	}

	return creds, nil
}

// updateSecret handles non-rotation updates and returns clean credentials
func (d *WebhookDestination) updateSecret(newDest, origDest *models.Destination, opts *destregistry.PreprocessDestinationOpts) (map[string]string, error) {
	creds := make(map[string]string)

	if opts.Role != "admin" {
		// For tenants, first check if they're trying to modify any credential fields
		if origDest != nil && origDest.Credentials != nil {
			// Updating existing destination - must match original values
			if newDest.Credentials["secret"] != "" && newDest.Credentials["secret"] != origDest.Credentials["secret"] {
				return nil, destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
					{
						Field: "credentials.secret",
						Type:  "forbidden",
					},
				})
			}
			if newDest.Credentials["previous_secret"] != "" && newDest.Credentials["previous_secret"] != origDest.Credentials["previous_secret"] {
				return nil, destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
					{
						Field: "credentials.previous_secret",
						Type:  "forbidden",
					},
				})
			}
			if newDest.Credentials["previous_secret_invalid_at"] != "" && newDest.Credentials["previous_secret_invalid_at"] != origDest.Credentials["previous_secret_invalid_at"] {
				return nil, destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
					{
						Field: "credentials.previous_secret_invalid_at",
						Type:  "forbidden",
					},
				})
			}
			// Copy original values
			for _, key := range []string{"secret", "previous_secret", "previous_secret_invalid_at"} {
				if value := origDest.Credentials[key]; value != "" {
					creds[key] = value
				}
			}
		} else {
			// First time creation - can't set any credentials
			if newDest.Credentials["secret"] != "" {
				return nil, destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
					{
						Field: "credentials.secret",
						Type:  "forbidden",
					},
				})
			}
			if newDest.Credentials["previous_secret"] != "" {
				return nil, destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
					{
						Field: "credentials.previous_secret",
						Type:  "forbidden",
					},
				})
			}
			if newDest.Credentials["previous_secret_invalid_at"] != "" {
				return nil, destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
					{
						Field: "credentials.previous_secret_invalid_at",
						Type:  "forbidden",
					},
				})
			}
		}
	} else {
		// Admin can set any values
		for _, key := range []string{"secret", "previous_secret", "previous_secret_invalid_at"} {
			if value := newDest.Credentials[key]; value != "" {
				creds[key] = value
			}
		}
	}

	return creds, nil
}

// ensureInitializedCredentials ensures credentials are initialized for new destinations
func (d *WebhookDestination) ensureInitializedCredentials(creds map[string]string) (map[string]string, error) {
	// If there are any credentials already, return them as is
	if creds["secret"] != "" || creds["previous_secret"] != "" || creds["previous_secret_invalid_at"] != "" {
		return creds, nil
	}

	// Otherwise generate a new secret
	secret, err := generateSignatureSecret()
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"secret": secret,
	}, nil
}

// validateAndSanitizeCredentials performs final validation and cleanup
func (d *WebhookDestination) validateAndSanitizeCredentials(creds map[string]string) (map[string]string, error) {
	// Set default previous_secret_invalid_at if previous_secret is set but invalid_at is not
	if creds["previous_secret"] != "" && creds["previous_secret_invalid_at"] == "" {
		creds["previous_secret_invalid_at"] = time.Now().Add(24 * time.Hour).Format(time.RFC3339)
	}

	// Clean up any extra fields
	cleanCreds := make(map[string]string)
	for _, key := range []string{"secret", "previous_secret", "previous_secret_invalid_at"} {
		if value := creds[key]; value != "" {
			cleanCreds[key] = value
		}
	}

	return cleanCreds, nil
}

// Preprocess sets a default secret if one isn't provided and handles secret rotation
func (d *WebhookDestination) Preprocess(newDestination *models.Destination, originalDestination *models.Destination, opts *destregistry.PreprocessDestinationOpts) error {
	// Initialize credentials if nil
	if newDestination.Credentials == nil {
		newDestination.Credentials = make(map[string]string)
	}

	// Get clean credentials based on operation type
	var cleanCredentials map[string]string
	var err error
	if isTruthy(newDestination.Credentials["rotate_secret"]) {
		cleanCredentials, err = d.rotateSecret(newDestination, originalDestination)
	} else {
		cleanCredentials, err = d.updateSecret(newDestination, originalDestination, opts)
		// For new destinations, ensure credentials are initialized if needed
		if err == nil && originalDestination == nil {
			cleanCredentials, err = d.ensureInitializedCredentials(cleanCredentials)
		}
	}
	if err != nil {
		return err
	}

	// Final validation and sanitization
	cleanCredentials, err = d.validateAndSanitizeCredentials(cleanCredentials)
	if err != nil {
		return err
	}

	newDestination.Credentials = cleanCredentials
	return nil
}

type WebhookPublisher struct {
	*destregistry.BasePublisher
	url                    string
	headerPrefix           string
	secrets                []WebhookSecret
	sm                     *SignatureManager
	disableEventIDHeader   bool
	disableSignatureHeader bool
	disableTimestampHeader bool
	disableTopicHeader     bool
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

	httpReq, err := p.Format(ctx, event)
	if err != nil {
		return err
	}

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return destregistry.NewErrDestinationPublishAttempt(err, "webhook", map[string]interface{}{
			"error":   "request_failed",
			"message": err.Error(),
		})
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return destregistry.NewErrDestinationPublishAttempt(fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(bodyBytes)), "webhook", map[string]interface{}{
			"error": "request_failed",
			"response": map[string]interface{}{
				"status": resp.StatusCode,
				"body":   string(bodyBytes),
			},
		})
	}

	return nil
}

// Format is a helper function to format the event data into an HTTP request.
func (p *WebhookPublisher) Format(ctx context.Context, event *models.Event) (*http.Request, error) {
	now := time.Now()
	rawBody, err := json.Marshal(event.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event data: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", p.url, bytes.NewBuffer(rawBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	// Add default headers unless disabled
	if !p.disableTimestampHeader {
		req.Header.Set(p.headerPrefix+"timestamp", fmt.Sprintf("%d", now.Unix()))
	}
	if !p.disableEventIDHeader {
		req.Header.Set(p.headerPrefix+"event-id", event.ID)
	}
	if !p.disableTopicHeader {
		req.Header.Set(p.headerPrefix+"topic", event.Topic)
	}
	if !p.disableSignatureHeader {
		signatureHeader := p.sm.GenerateSignatureHeader(SignaturePayload{
			EventID:   event.ID,
			Topic:     event.Topic,
			Timestamp: now,
			Body:      string(rawBody),
		})
		if signatureHeader != "" {
			req.Header.Set(p.headerPrefix+"signature", signatureHeader)
		}
	}

	// Add metadata headers with the specified prefix
	for key, value := range event.Metadata {
		req.Header.Set(p.headerPrefix+strings.ToLower(key), value)
	}

	return req, nil
}

// generateSignatureSecret creates a cryptographically secure random secret suitable for HMAC signatures.
// The secret is 32 bytes (256 bits) encoded as a hex string.
func generateSignatureSecret() (string, error) {
	// Generate a random 32-byte hex string
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate random secret: %w", err)
	}
	return hex.EncodeToString(randomBytes), nil
}

// GetEncoder returns the appropriate SignatureEncoder for the given encoding
func GetEncoder(encoding string) SignatureEncoder {
	switch encoding {
	case "base64":
		return Base64Encoder{}
	case "hex":
		return HexEncoder{}
	default:
		return HexEncoder{} // default to hex
	}
}

// GetAlgorithm returns the appropriate SigningAlgorithm for the given algorithm name
func GetAlgorithm(algorithm string) SigningAlgorithm {
	switch algorithm {
	case "hmac-sha1":
		return NewHmacSHA1()
	case "hmac-sha256":
		return NewHmacSHA256()
	default:
		return NewHmacSHA256() // default to hmac-sha256
	}
}

// isTruthy checks if a string value represents a truthy value
func isTruthy(value string) bool {
	switch strings.ToLower(value) {
	case "true", "1", "on", "yes":
		return true
	default:
		return false
	}
}
