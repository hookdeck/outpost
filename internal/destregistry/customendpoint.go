package destregistry

import (
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/models"
)

const ConfigKeyEndpoint = "endpoint"

func ValidateCustomEndpointAllowed(allowed bool, destination *models.Destination) error {
	if allowed || destination.Config[ConfigKeyEndpoint] == "" {
		return nil
	}
	return NewErrDestinationValidation([]ValidationErrorDetail{{
		Field: "config." + ConfigKeyEndpoint,
		Type:  "forbidden",
	}})
}

func (p *BaseProvider) OmitConfigField(key string) {
	fields := make([]metadata.FieldSchema, 0, len(p.metadata.ConfigFields))
	for _, f := range p.metadata.ConfigFields {
		if f.Key != key {
			fields = append(fields, f)
		}
	}
	p.metadata.ConfigFields = fields
}
