package destregistry

import (
	"context"
	"fmt"
	"strings"

	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

// BaseProvider provides common functionality for all destination providers
type BaseProvider struct {
	metadata *metadata.ProviderMetadata
}

// NewBaseProvider creates a new base provider with loaded metadata
func NewBaseProvider(providerType string) (*BaseProvider, error) {
	loader := metadata.NewMetadataLoader("")
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

// Validate performs schema validation using the provider's metadata
func (p *BaseProvider) Validate(ctx context.Context, destination *models.Destination) error {
	if destination.Type != p.metadata.Type {
		return NewErrDestinationValidation([]ValidationErrorDetail{{
			Field: "type",
			Type:  "invalid_type",
		}})
	}

	// Convert the config and credentials to map[string]interface{} for JSON schema validation
	validationData := map[string]interface{}{
		"config":      map[string]interface{}{},
		"credentials": map[string]interface{}{},
	}

	// Copy config values
	for k, v := range destination.Config {
		validationData["config"].(map[string]interface{})[k] = v
	}

	// Copy credentials values
	for k, v := range destination.Credentials {
		validationData["credentials"].(map[string]interface{})[k] = v
	}

	// Validate using JSON schema
	if err := p.metadata.Validation.Validate(validationData); err != nil {
		if validationErr, ok := err.(*jsonschema.ValidationError); ok {
			errors := formatJSONSchemaErrors(validationErr)
			if len(errors) == 0 {
				return NewErrDestinationValidation([]ValidationErrorDetail{{
					Field: "root",
					Type:  "unknown",
				}})
			}
			return NewErrDestinationValidation(errors)
		}
	}

	return nil
}

func formatPropertyPath(pathParts []string) string {
	var parts []string
	for _, part := range pathParts {
		if part != "" {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return "root"
	}
	return strings.Join(parts, ".")
}

func formatJSONSchemaErrors(validationErr *jsonschema.ValidationError) []ValidationErrorDetail {
	var errors []ValidationErrorDetail

	var processError func(*jsonschema.ValidationError)
	processError = func(verr *jsonschema.ValidationError) {
		if verr.InstanceLocation != nil {
			propertyPath := formatPropertyPath(verr.InstanceLocation)
			if errorKind, ok := verr.ErrorKind.(interface{ KeywordPath() []string }); ok {
				keywordPath := errorKind.KeywordPath()
				errorType := keywordPath[len(keywordPath)-1]

				// Handle required field errors specially
				if errorType == "required" {
					if required, ok := verr.ErrorKind.(*kind.Required); ok {
						for _, missingField := range required.Missing {
							fullPath := propertyPath
							if fullPath != "root" {
								fullPath = fullPath + "." + missingField
							}
							errors = append(errors, ValidationErrorDetail{
								Field: fullPath,
								Type:  "required",
							})
						}
						return
					}
				}

				errors = append(errors, ValidationErrorDetail{
					Field: propertyPath,
					Type:  errorType,
				})
			}
		}

		for _, cause := range verr.Causes {
			processError(cause)
		}
	}

	processError(validationErr)
	return errors
}
