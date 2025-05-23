package destregistry

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/models"
)

// ObfuscateValue masks a sensitive value with the following rules:
// - For strings with length >= 10: show first 4 characters + asterisks for the rest
// - For strings with length < 10: replace each character with an asterisk
func ObfuscateValue(value string) string {
	if len(value) < 10 {
		return strings.Repeat("*", len(value))
	}
	return value[:4] + strings.Repeat("*", len(value)-4)
}

// BaseProvider provides common functionality for all destination providers
type BaseProvider struct {
	metadata *metadata.ProviderMetadata
}

// NewBaseProvider creates a new base provider with loaded metadata
func NewBaseProvider(loader metadata.MetadataLoader, providerType string) (*BaseProvider, error) {
	meta, err := loader.Load(providerType)
	if err != nil {
		return nil, fmt.Errorf("loading provider metadata: %w", err)
	}

	return &BaseProvider{
		metadata: meta,
	}, nil
}

// Metadata returns the provider metadata
func (p *BaseProvider) Metadata() *metadata.ProviderMetadata {
	return p.metadata
}

// ObfuscateDestination returns a copy of the destination with sensitive fields masked
func (p *BaseProvider) ObfuscateDestination(destination *models.Destination) *models.Destination {
	result := *destination // shallow copy
	result.Config = make(map[string]string, len(destination.Config))
	result.Credentials = make(map[string]string, len(destination.Credentials))

	// Create maps of sensitive fields for quick lookup
	sensitiveConfigFields := make(map[string]bool)
	for _, field := range p.metadata.ConfigFields {
		if field.Sensitive {
			sensitiveConfigFields[field.Key] = true
		}
	}

	sensitiveCredFields := make(map[string]bool)
	for _, field := range p.metadata.CredentialFields {
		if field.Sensitive {
			sensitiveCredFields[field.Key] = true
		}
	}

	// Copy all config values, masking only sensitive ones
	for key, value := range destination.Config {
		if sensitiveConfigFields[key] {
			result.Config[key] = ObfuscateValue(value)
		} else {
			result.Config[key] = value
		}
	}

	// Copy all credential values, masking only sensitive ones
	for key, value := range destination.Credentials {
		if sensitiveCredFields[key] {
			result.Credentials[key] = ObfuscateValue(value)
		} else {
			result.Credentials[key] = value
		}
	}

	return &result
}

// Validate performs field-level validation using the provider's metadata
func (p *BaseProvider) Validate(ctx context.Context, destination *models.Destination) error {
	if destination.Type != p.metadata.Type {
		return NewErrDestinationValidation([]ValidationErrorDetail{{
			Field: "type",
			Type:  "invalid_type",
		}})
	}

	var errors []ValidationErrorDetail

	// Validate config fields
	for _, field := range p.metadata.ConfigFields {
		if err := validateField(field, destination.Config[field.Key], "config."+field.Key); err != nil {
			errors = append(errors, *err)
		}
	}

	// Validate credential fields
	for _, field := range p.metadata.CredentialFields {
		if err := validateField(field, destination.Credentials[field.Key], "credentials."+field.Key); err != nil {
			errors = append(errors, *err)
		}
	}

	if len(errors) > 0 {
		return NewErrDestinationValidation(errors)
	}

	return nil
}

func validateField(field metadata.FieldSchema, value string, path string) *ValidationErrorDetail {
	// Check existence/required first
	if value == "" {
		if field.Required {
			return &ValidationErrorDetail{
				Field: path,
				Type:  "required",
			}
		}
		return nil
	}

	if field.Type == "number" {
		num, err := strconv.Atoi(value)
		if err != nil {
			return &ValidationErrorDetail{
				Field: path,
				Type:  "type",
			}
		}

		if field.Min != nil && num < *field.Min {
			return &ValidationErrorDetail{
				Field: path,
				Type:  "min",
			}
		}

		if field.Max != nil && num > *field.Max {
			return &ValidationErrorDetail{
				Field: path,
				Type:  "max",
			}
		}

		return nil
	}

	// String validation
	if field.MinLength != nil && len(value) < *field.MinLength {
		return &ValidationErrorDetail{
			Field: path,
			Type:  "minlength",
		}
	}

	if field.MaxLength != nil && len(value) > *field.MaxLength {
		return &ValidationErrorDetail{
			Field: path,
			Type:  "maxlength",
		}
	}

	if field.Pattern != nil {
		matched, err := regexp.MatchString(*field.Pattern, value)
		if err != nil || !matched {
			return &ValidationErrorDetail{
				Field: path,
				Type:  "pattern",
			}
		}
	}

	return nil
}

// Preprocess is a noop by default
func (p *BaseProvider) Preprocess(newDestination *models.Destination, originalDestination *models.Destination, opts *PreprocessDestinationOpts) error {
	return nil
}

type HTTPClientConfig struct {
	Timeout   *time.Duration
	UserAgent *string
}

func (p *BaseProvider) MakeHTTPClient(config HTTPClientConfig) *http.Client {
	client := &http.Client{}

	if config.Timeout != nil {
		client.Timeout = *config.Timeout
	}

	if config.UserAgent != nil {
		client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			req.Header.Set("User-Agent", *config.UserAgent)
			return http.DefaultTransport.RoundTrip(req)
		})
	}

	return client
}

// Helper type for custom RoundTripper
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
