package destregistry

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

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
	metadata          *metadata.ProviderMetadata
	basePublisherOpts []BasePublisherOption
	configKeys        map[string]struct{}
	credentialKeys    map[string]struct{}
}

// NewBaseProvider creates a new base provider with loaded metadata
func NewBaseProvider(loader metadata.MetadataLoader, providerType string, opts ...BasePublisherOption) (*BaseProvider, error) {
	meta, err := loader.Load(providerType)
	if err != nil {
		return nil, fmt.Errorf("loading provider metadata: %w", err)
	}

	configKeys := make(map[string]struct{}, len(meta.ConfigFields))
	for _, f := range meta.ConfigFields {
		configKeys[f.Key] = struct{}{}
	}
	credentialKeys := make(map[string]struct{}, len(meta.CredentialFields))
	for _, f := range meta.CredentialFields {
		credentialKeys[f.Key] = struct{}{}
	}

	return &BaseProvider{
		metadata:          meta,
		basePublisherOpts: opts,
		configKeys:        configKeys,
		credentialKeys:    credentialKeys,
	}, nil
}

// AllowConfigKeys marks config keys the provider reads or writes that are not
// declared as form fields in its metadata. Keys neither declared nor allowed
// are dropped before a destination is stored.
func (p *BaseProvider) AllowConfigKeys(keys ...string) {
	for _, k := range keys {
		p.configKeys[k] = struct{}{}
	}
}

// AllowCredentialKeys marks credential keys the provider reads or writes that
// are not declared as form fields in its metadata. Keys neither declared nor
// allowed are dropped before a destination is stored.
func (p *BaseProvider) AllowCredentialKeys(keys ...string) {
	for _, k := range keys {
		p.credentialKeys[k] = struct{}{}
	}
}

// ConfigKeys returns the sorted config keys the provider keeps on a destination.
func (p *BaseProvider) ConfigKeys() []string {
	return sortedKeys(p.configKeys)
}

// CredentialKeys returns the sorted credential keys the provider keeps on a destination.
func (p *BaseProvider) CredentialKeys() []string {
	return sortedKeys(p.credentialKeys)
}

func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// DropUndeclaredKeys removes config and credential keys the provider does not
// keep. It replaces a map instead of editing it, because the map may be
// shared with the stored destination.
func (p *BaseProvider) DropUndeclaredKeys(destination *models.Destination) {
	destination.Config = filterKeys(destination.Config, p.configKeys)
	destination.Credentials = filterKeys(destination.Credentials, p.credentialKeys)
}

func filterKeys(m models.MapStringString, keep map[string]struct{}) models.MapStringString {
	drop := false
	for k := range m {
		if _, ok := keep[k]; !ok {
			drop = true
			break
		}
	}
	if !drop {
		return m
	}
	out := make(models.MapStringString, len(m))
	for k, v := range m {
		if _, ok := keep[k]; ok {
			out[k] = v
		}
	}
	return out
}

// NewPublisher creates a BasePublisher with provider-configured options plus any additional options
func (p *BaseProvider) NewPublisher(additionalOpts ...BasePublisherOption) *BasePublisher {
	opts := append([]BasePublisherOption{}, p.basePublisherOpts...)
	opts = append(opts, additionalOpts...)
	return NewBasePublisher(opts...)
}

// Metadata returns the provider metadata
func (p *BaseProvider) Metadata() *metadata.ProviderMetadata {
	return p.metadata
}

// OmitConfigField removes the config field with the given key from the provider
// metadata. The key is still kept on destinations, so the provider's own
// validation still sees a stored value.
func (p *BaseProvider) OmitConfigField(key string) {
	fields := make([]metadata.FieldSchema, 0, len(p.metadata.ConfigFields))
	for _, f := range p.metadata.ConfigFields {
		if f.Key != key {
			fields = append(fields, f)
		}
	}
	p.metadata.ConfigFields = fields
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
