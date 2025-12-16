package destwebhookstandard

/*
Standard Webhooks Destination Provider

This implementation is based on the destwebhook provider and reuses its core
signature management infrastructure (SignatureManager). The key differences are:

1. Secret Format:
   - destwebhook: Flexible format (any string, typically hex-encoded)
   - destwebhookstandard: Strict "whsec_<base64>" format per Standard Webhooks spec

2. Header Names:
   - destwebhook: Customizable via templates
   - destwebhookstandard: Uses configurable prefix for all headers: "id", "timestamp", "signature",
                          and metadata headers (topic, etc.)
   - Prefix defaults to "webhook-" in standard mode, "x-outpost-" in default mode
   - Examples: "webhook-id", "webhook-timestamp" OR "x-custom-id", "x-custom-timestamp"

3. Signature Format:
   - destwebhook: Customizable template
   - destwebhookstandard: Fixed "${webhook-id}.${timestamp}.${body}" signed content
                          and "v1,<base64>" signature format

ARCHITECTURE NOTES:

- We import and use destwebhook.SignatureManager for signature generation
- We use destwebhook.WebhookSecret for secret storage
- Secret validation, parsing, and rotation logic is currently duplicated here

FUTURE REFACTORING CONSIDERATIONS:

If this pattern proves useful, consider extracting shared logic into a common package:
- Secret validation and parsing helpers
- Secret rotation logic (with configurable format validators)
- Common credential preprocessing patterns
- Shared validation error construction

This would allow both destwebhook and destwebhookstandard to share the same
underlying infrastructure while maintaining their specific requirements.

For now, we keep them separate to:
1. Avoid breaking changes to the existing destwebhook implementation
2. Allow independent evolution of the two providers
3. Keep the Standard Webhooks implementation self-contained for easier review

Related files to consider for refactoring:
- internal/destregistry/providers/destwebhook/signature.go (SignatureManager)
- internal/destregistry/providers/destwebhook/destwebhook.go (rotation logic)
*/

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/models"
)

type StandardWebhookDestination struct {
	*destregistry.BaseProvider
	userAgent    string
	proxyURL     string
	headerPrefix string // Prefix for metadata headers (defaults to "webhook-")
}

type StandardWebhookDestinationConfig struct {
	URL           string            `json:"url"`
	CustomHeaders map[string]string `json:"custom_headers,omitempty"`
}

type StandardWebhookDestinationCredentials struct {
	Secret                  string     `json:"secret"`
	PreviousSecret          string     `json:"previous_secret,omitempty"`
	PreviousSecretInvalidAt *time.Time `json:"previous_secret_invalid_at,omitempty"`
}

var _ destregistry.Provider = (*StandardWebhookDestination)(nil)

// Option is a functional option for configuring StandardWebhookDestination
type Option func(*StandardWebhookDestination)

// WithUserAgent sets the user agent for the webhook request
func WithUserAgent(userAgent string) Option {
	return func(d *StandardWebhookDestination) {
		d.userAgent = userAgent
	}
}

// WithProxyURL sets the proxy URL for the webhook request
func WithProxyURL(proxyURL string) Option {
	return func(d *StandardWebhookDestination) {
		if proxyURL != "" {
			d.proxyURL = proxyURL
		}
	}
}

// WithHeaderPrefix sets the prefix for metadata headers (defaults to "webhook-")
func WithHeaderPrefix(prefix string) Option {
	return func(d *StandardWebhookDestination) {
		if prefix != "" {
			d.headerPrefix = prefix
		}
	}
}

func New(loader metadata.MetadataLoader, basePublisherOpts []destregistry.BasePublisherOption, opts ...Option) (*StandardWebhookDestination, error) {
	base, err := destregistry.NewBaseProvider(loader, "webhook_standard", basePublisherOpts...)
	if err != nil {
		return nil, err
	}
	destination := &StandardWebhookDestination{
		BaseProvider: base,
		headerPrefix: "webhook-", // Default to Standard Webhooks spec
	}
	for _, opt := range opts {
		opt(destination)
	}
	return destination, nil
}

func (d *StandardWebhookDestination) ComputeTarget(destination *models.Destination) destregistry.DestinationTarget {
	return destregistry.DestinationTarget{
		Target:    destination.Config["url"],
		TargetURL: "",
	}
}

func (d *StandardWebhookDestination) ObfuscateDestination(destination *models.Destination) *models.Destination {
	result := *destination // shallow copy
	result.Config = make(map[string]string, len(destination.Config))
	result.Credentials = make(map[string]string, len(destination.Credentials))

	// Copy config values
	for key, value := range destination.Config {
		result.Config[key] = value
	}

	// Copy credentials as is
	// NOTE: Secrets are intentionally not obfuscated for now because:
	// 1. They're needed for secret rotation logic
	// 2. They're less security-critical than other provider credentials
	for key, value := range destination.Credentials {
		result.Credentials[key] = value
	}

	return &result
}

func (d *StandardWebhookDestination) Validate(ctx context.Context, destination *models.Destination) error {
	if _, _, err := d.resolveConfig(ctx, destination); err != nil {
		return err
	}
	return nil
}

func (d *StandardWebhookDestination) CreatePublisher(ctx context.Context, destination *models.Destination) (destregistry.Publisher, error) {
	config, creds, err := d.resolveConfig(ctx, destination)
	if err != nil {
		return nil, err
	}

	// Parse and validate secrets
	now := time.Now()
	var secrets []destwebhook.WebhookSecret

	// Parse current secret
	parsedSecret, err := parseSecret(creds.Secret)
	if err != nil {
		return nil, fmt.Errorf("failed to parse secret: %w", err)
	}
	secrets = append(secrets, destwebhook.WebhookSecret{
		Key:       parsedSecret,
		CreatedAt: now,
	})

	// Parse previous secret if present
	if creds.PreviousSecret != "" {
		parsedPrevSecret, err := parseSecret(creds.PreviousSecret)
		if err != nil {
			return nil, fmt.Errorf("failed to parse previous_secret: %w", err)
		}
		secrets = append(secrets, destwebhook.WebhookSecret{
			Key:       parsedPrevSecret,
			CreatedAt: now.Add(-1 * time.Hour), // Set to 1 hour before current secret
			InvalidAt: creds.PreviousSecretInvalidAt,
		})
	}

	// Create SignatureManager with Standard Webhooks templates
	sm := destwebhook.NewSignatureManager(
		secrets,
		destwebhook.WithSignatureFormatter(
			destwebhook.NewSignatureFormatter("{{.EventID}}.{{.Timestamp.Unix}}.{{.Body}}"),
		),
		destwebhook.WithHeaderFormatter(
			destwebhook.NewHeaderFormatter("v1,{{index .Signatures 0}}{{range slice .Signatures 1}} v1,{{.}}{{end}}"),
		),
		destwebhook.WithEncoder(destwebhook.GetEncoder("base64")),
		destwebhook.WithAlgorithm(destwebhook.GetAlgorithm("hmac-sha256")),
	)

	var proxyURL *string
	if d.proxyURL != "" {
		proxyURL = &d.proxyURL
	}

	httpClient, err := d.BaseProvider.MakeHTTPClient(destregistry.HTTPClientConfig{
		UserAgent: &d.userAgent,
		ProxyURL:  proxyURL,
	})
	if err != nil {
		return nil, err
	}

	return &StandardWebhookPublisher{
		BasePublisher: d.BaseProvider.NewPublisher(destregistry.WithDeliveryMetadata(destination.DeliveryMetadata)),
		httpClient:    httpClient,
		url:           config.URL,
		secrets:       secrets,
		sm:            sm,
		headerPrefix:  d.headerPrefix,
		customHeaders: config.CustomHeaders,
	}, nil
}

func (d *StandardWebhookDestination) resolveConfig(ctx context.Context, destination *models.Destination) (*StandardWebhookDestinationConfig, *StandardWebhookDestinationCredentials, error) {
	if err := d.BaseProvider.Validate(ctx, destination); err != nil {
		return nil, nil, err
	}

	config := &StandardWebhookDestinationConfig{
		URL: destination.Config["url"],
	}

	// Parse custom headers from config
	if headersJSON, ok := destination.Config["custom_headers"]; ok && headersJSON != "" {
		if err := json.Unmarshal([]byte(headersJSON), &config.CustomHeaders); err != nil {
			return nil, nil, destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{{
				Field: "config.custom_headers",
				Type:  "invalid",
			}})
		}
		if err := destwebhook.ValidateCustomHeaders(config.CustomHeaders); err != nil {
			return nil, nil, err
		}
	}

	// Parse credentials
	creds := &StandardWebhookDestinationCredentials{
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

	// Validate secret format
	if err := validateSecret(creds.Secret); err != nil {
		return nil, nil, destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{{
			Field: "credentials.secret",
			Type:  "pattern",
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
		creds.PreviousSecretInvalidAt = &invalidAt
	}

	// Validate previous_secret if provided
	if creds.PreviousSecret != "" {
		if err := validateSecret(creds.PreviousSecret); err != nil {
			return nil, nil, destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{{
				Field: "credentials.previous_secret",
				Type:  "pattern",
			}})
		}

		// Require invalidation time if previous secret is provided
		if creds.PreviousSecretInvalidAt == nil {
			return nil, nil, destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{{
				Field: "credentials.previous_secret_invalid_at",
				Type:  "required",
			}})
		}
	}

	// If previous_secret_invalid_at is provided, validate previous_secret
	if creds.PreviousSecretInvalidAt != nil && creds.PreviousSecret == "" {
		return nil, nil, destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{{
			Field: "credentials.previous_secret",
			Type:  "required",
		}})
	}

	return config, creds, nil
}

// rotateSecret handles secret rotation and returns clean credentials
func (d *StandardWebhookDestination) rotateSecret(newDest, origDest *models.Destination) (map[string]string, error) {
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
	secret, err := generateStandardSecret()
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
func (d *StandardWebhookDestination) updateSecret(newDest, origDest *models.Destination, opts *destregistry.PreprocessDestinationOpts) (map[string]string, error) {
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
func (d *StandardWebhookDestination) ensureInitializedCredentials(creds map[string]string) (map[string]string, error) {
	// If there are any credentials already, return them as is
	if creds["secret"] != "" || creds["previous_secret"] != "" || creds["previous_secret_invalid_at"] != "" {
		return creds, nil
	}

	// Otherwise generate a new secret
	secret, err := generateStandardSecret()
	if err != nil {
		return nil, err
	}
	return map[string]string{
		"secret": secret,
	}, nil
}

// validateAndSanitizeCredentials performs final validation and cleanup
func (d *StandardWebhookDestination) validateAndSanitizeCredentials(creds map[string]string) (map[string]string, error) {
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
func (d *StandardWebhookDestination) Preprocess(newDestination *models.Destination, originalDestination *models.Destination, opts *destregistry.PreprocessDestinationOpts) error {
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

type StandardWebhookPublisher struct {
	*destregistry.BasePublisher
	httpClient    *http.Client
	url           string
	secrets       []destwebhook.WebhookSecret
	sm            *destwebhook.SignatureManager
	headerPrefix  string
	customHeaders map[string]string
}

func (p *StandardWebhookPublisher) Close() error {
	p.BasePublisher.StartClose()
	return nil
}

func (p *StandardWebhookPublisher) Publish(ctx context.Context, event *models.Event) (*destregistry.Delivery, error) {
	if err := p.BasePublisher.StartPublish(); err != nil {
		return nil, err
	}
	defer p.BasePublisher.FinishPublish()

	httpReq, err := p.Format(ctx, event)
	if err != nil {
		return nil, err
	}

	result := destwebhook.ExecuteHTTPRequest(ctx, p.httpClient, httpReq, "webhook_standard")
	return result.Delivery, result.Error
}

// Format creates an HTTP request formatted according to Standard Webhooks specification
func (p *StandardWebhookPublisher) Format(ctx context.Context, event *models.Event) (*http.Request, error) {
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

	// Add custom headers FIRST (so metadata can override if there's a conflict)
	for key, value := range p.customHeaders {
		req.Header.Set(key, value)
	}

	// Use event ID directly as the message ID
	// This ensures the same message ID is used across retry attempts
	// TODO: Support configurable ID generator/template (e.g., "msg_" prefix)
	messageID := event.ID

	// Set Standard Webhooks headers with configurable prefix
	req.Header.Set(p.headerPrefix+"id", messageID)
	req.Header.Set(p.headerPrefix+"timestamp", strconv.FormatInt(now.Unix(), 10))

	// Generate and set signature header
	signatureHeader := p.sm.GenerateSignatureHeader(destwebhook.SignaturePayload{
		EventID:   messageID,
		Topic:     event.Topic,
		Timestamp: now,
		Body:      string(rawBody),
	})
	if signatureHeader != "" {
		req.Header.Set(p.headerPrefix+"signature", signatureHeader)
	}

	// Add event metadata as custom headers
	// Get merged metadata (system + event metadata) using BasePublisher
	metadata := p.BasePublisher.MakeMetadata(event, now)
	for key, value := range metadata {
		// Skip system metadata that's already handled by Standard Webhooks headers
		// (webhook-id replaces event-id, webhook-timestamp replaces timestamp)
		if key == "event-id" || key == "timestamp" {
			continue
		}
		// Add with configured prefix (defaults to "webhook-")
		req.Header.Set(p.headerPrefix+key, value)
	}

	// Also add custom event metadata without prefix (user-defined metadata)
	for key, value := range event.Metadata {
		req.Header.Set(key, value)
	}

	return req, nil
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
