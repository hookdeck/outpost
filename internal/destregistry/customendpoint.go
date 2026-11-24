package destregistry

import (
	"github.com/hookdeck/outpost/internal/models"
)

// ConfigKeyEndpoint is the config key that points a destination at a server other than the cloud service.
const ConfigKeyEndpoint = "endpoint"

// ValidateCustomEndpointAllowed rejects a destination that sets config.endpoint unless allowed is true.
func ValidateCustomEndpointAllowed(allowed bool, destination *models.Destination) error {
	if allowed || destination.Config[ConfigKeyEndpoint] == "" {
		return nil
	}
	return NewErrDestinationValidation([]ValidationErrorDetail{{
		Field: "config." + ConfigKeyEndpoint,
		Type:  "forbidden",
	}})
}
