package desthookdeck

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/models"
)

// Configuration type - empty since we don't have config fields
type HookdeckConfig struct {
}

// Credentials type
type HookdeckCredentials struct {
	Token string
}

// ProviderOption defines a function that configures a HookdeckProvider
type ProviderOption func(*HookdeckProvider)

// WithHTTPClient sets a custom HTTP client for all publishers created by this provider
func WithHTTPClient(client *http.Client) ProviderOption {
	return func(p *HookdeckProvider) {
		p.httpClient = client
	}
}

// Provider implementation
type HookdeckProvider struct {
	*destregistry.BaseProvider
	httpClient *http.Client
}

// Ensure our provider implements the Provider interface
var _ destregistry.Provider = (*HookdeckProvider)(nil)

// Constructor
func New(loader metadata.MetadataLoader, opts ...ProviderOption) (*HookdeckProvider, error) {
	base, err := destregistry.NewBaseProvider(loader, "hookdeck")
	if err != nil {
		return nil, err
	}

	provider := &HookdeckProvider{
		BaseProvider: base,
		httpClient:   &http.Client{Timeout: 30 * time.Second}, // Default client
	}

	// Apply options
	for _, opt := range opts {
		opt(provider)
	}

	return provider, nil
}

// Validate performs destination-specific validation including token format
func (p *HookdeckProvider) Validate(ctx context.Context, destination *models.Destination) error {
	// First run the base validation
	if err := p.BaseProvider.Validate(ctx, destination); err != nil {
		return err
	}

	// Validate the token format
	token := destination.Credentials["token"]
	if token != "" {
		if _, err := ParseHookdeckToken(token); err != nil {
			return destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
				{
					Field: "credentials.token",
					Type:  "invalid_token_format",
				},
			})
		}
	}

	return nil
}

// PublisherOption defines a function that configures a HookdeckPublisher
type PublisherOption func(*HookdeckPublisher)

// PublisherWithClient sets a custom HTTP client for the publisher
func PublisherWithClient(client *http.Client) PublisherOption {
	return func(p *HookdeckPublisher) {
		p.client = client
	}
}

// NewPublisher creates a new HookdeckPublisher with the provided token and options
func NewPublisher(tokenString string, opts ...PublisherOption) (*HookdeckPublisher, error) {
	// Parse the token
	parsedToken, err := ParseHookdeckToken(tokenString)
	if err != nil {
		return nil, err
	}

	// Create publisher with default settings
	publisher := &HookdeckPublisher{
		BasePublisher: &destregistry.BasePublisher{},
		tokenString:   tokenString,
		parsedToken:   parsedToken,
		client:        &http.Client{Timeout: 30 * time.Second},
	}

	// Apply custom options
	for _, opt := range opts {
		opt(publisher)
	}

	return publisher, nil
}

// CreatePublisher creates a new publisher instance
func (p *HookdeckProvider) CreatePublisher(ctx context.Context, destination *models.Destination) (destregistry.Publisher, error) {
	// Validate destination
	if err := p.Validate(ctx, destination); err != nil {
		return nil, err
	}

	// Get the token from credentials
	tokenString := destination.Credentials["token"]

	// Create publisher options
	var opts []PublisherOption

	// Use the provider's HTTP client if set
	if p.httpClient != nil {
		opts = append(opts, PublisherWithClient(p.httpClient))
	}

	// Use NewPublisher to create the publisher with options
	publisher, err := NewPublisher(tokenString, opts...)
	if err != nil {
		return nil, destregistry.NewErrDestinationPublishAttempt(err, "hookdeck", map[string]interface{}{
			"error":   "invalid_token",
			"message": err.Error(),
		})
	}

	return publisher, nil
}

// ComputeTarget returns a human-readable target
func (p *HookdeckProvider) ComputeTarget(destination *models.Destination) destregistry.DestinationTarget {
	// Check if we have the source information in config
	if destination.Config != nil {
		sourceName, hasName := destination.Config["source_name"]
		sourceID, hasID := destination.Config["source_id"]

		if hasName && sourceName != "" && hasID && sourceID != "" {
			return destregistry.DestinationTarget{
				Target:    sourceName,
				TargetURL: "https://dashboard.hookdeck.com/sources/" + sourceID,
			}
		}
	}

	// Use token information as fallback
	token := destination.Credentials["token"]
	if token == "" {
		return destregistry.DestinationTarget{
			Target: "Hookdeck (no token)",
		}
	}

	hookdeckToken, err := ParseHookdeckToken(token)
	if err != nil {
		return destregistry.DestinationTarget{
			Target: "Hookdeck (invalid token)",
		}
	}

	return destregistry.DestinationTarget{
		Target:    "Hookdeck source ID: " + hookdeckToken.ID,
		TargetURL: "https://dashboard.hookdeck.com/sources/" + hookdeckToken.ID,
	}
}

// Preprocess sets defaults and standardizes values
func (p *HookdeckProvider) Preprocess(newDestination *models.Destination, originalDestination *models.Destination, opts *destregistry.PreprocessDestinationOpts) error {
	// Check if token is available
	token := newDestination.Credentials["token"]
	if token == "" {
		return destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
			{
				Field: "credentials.token",
				Type:  "token_required",
			},
		})
	}

	// Parse token to validate format
	parsedToken, err := ParseHookdeckToken(token)
	if err != nil {
		// Return validation error
		return destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
			{
				Field: "credentials.token",
				Type:  "invalid_token_format",
			},
		})
	}

	// Only verify token if we're creating a new destination or updating the token
	shouldVerify := originalDestination == nil || // New destination
		(originalDestination.Credentials["token"] != token) // Updated token

	if shouldVerify {
		// TODO: Preprocess should receive a context?
		// Create a background context for the verification request
		ctx := context.Background()

		// Verify token to get source information
		sourceResponse, err := VerifyHookdeckToken(p.httpClient, ctx, parsedToken)
		if err != nil {
			// Return error from verification
			return destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
				{
					Field: "credentials.token",
					Type:  "token_verification_failed",
				},
			})
		}

		// Initialize config map if nil
		if newDestination.Config == nil {
			newDestination.Config = make(models.MapStringString)
		}

		// Store source information in the destination config
		// These are hidden configs that won't be shown in the form
		newDestination.Config["source_name"] = sourceResponse.Name
		newDestination.Config["source_id"] = parsedToken.ID
	}

	return nil
}

// Publisher implementation
type HookdeckPublisher struct {
	*destregistry.BasePublisher
	tokenString string
	parsedToken *HookdeckToken
	client      *http.Client
}

// Format is a helper method that formats an event into an HTTP request for Hookdeck
func (p *HookdeckPublisher) Format(ctx context.Context, event *models.Event) (*http.Request, error) {
	metadata := p.BasePublisher.MakeMetadata(event, time.Now())

	// Create the HTTP request - defer to CreateRequest for implementation details
	req, err := CreateRequest(p.parsedToken, event, metadata)
	if err != nil {
		return nil, err
	}

	// Ensure the request has the context
	return req.WithContext(ctx), nil
}

// Close closes any connections or resources
func (p *HookdeckPublisher) Close() error {
	p.BasePublisher.StartClose()
	// No resources to clean up
	return nil
}

// Publish publishes an event to the Hookdeck destination
func (p *HookdeckPublisher) Publish(ctx context.Context, event *models.Event) (*destregistry.Delivery, error) {
	// Use base publisher start/finish methods for tracking
	if err := p.BasePublisher.StartPublish(); err != nil {
		return nil, err
	}
	defer p.BasePublisher.FinishPublish()

	// Create the HTTP request using the publisher's Format method
	req, err := p.Format(ctx, event)
	if err != nil {
		return &destregistry.Delivery{
				Status: "failed",
				Code:   "ERROR",
				Response: map[string]interface{}{
					"error": "Failed to create request",
				},
			}, destregistry.NewErrDestinationPublishAttempt(err, "hookdeck", map[string]interface{}{
				"error":   "request_creation_failed",
				"message": err.Error(),
			})
	}

	// Send the request
	resp, err := p.client.Do(req)
	if err != nil {
		return &destregistry.Delivery{
				Status: "failed",
				Code:   "ERROR",
				Response: map[string]interface{}{
					"error": "Request failed",
				},
			}, destregistry.NewErrDestinationPublishAttempt(err, "hookdeck", map[string]interface{}{
				"error":   "request_failed",
				"message": err.Error(),
			})
	}
	defer resp.Body.Close()

	// Read the response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return &destregistry.Delivery{
				Status: "uncertain",
				Code:   fmt.Sprintf("%d", resp.StatusCode),
				Response: map[string]interface{}{
					"error": "Failed to read response body",
				},
			}, destregistry.NewErrDestinationPublishAttempt(err, "hookdeck", map[string]interface{}{
				"error":   "response_read_failed",
				"message": err.Error(),
			})
	}

	// Determine delivery status based on HTTP status code
	status := "success"
	if resp.StatusCode >= 400 {
		status = "failed"
	}

	// Create response with a structured headers object
	responseData := map[string]interface{}{
		"status_code": resp.StatusCode,
		"body":        string(respBody),
		"headers":     make(map[string]interface{}),
	}

	// Add response headers as a structured object
	headers := make(map[string]interface{})
	for key, values := range resp.Header {
		if len(values) == 1 {
			headers[key] = values[0]
		} else if len(values) > 1 {
			headers[key] = values
		}
	}
	responseData["headers"] = headers

	return &destregistry.Delivery{
		Status:   status,
		Code:     fmt.Sprintf("%d", resp.StatusCode),
		Response: responseData,
	}, nil
}
