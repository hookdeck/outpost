// internal/destregistry/metadata/types.go
package metadata

// MetadataLoader loads provider metadata
type MetadataLoader interface {
	Load(providerType string) (*ProviderMetadata, error)
}

type ProviderMetadata struct {
	// From core.json
	Type             string        `json:"type"`
	ConfigFields     []FieldSchema `json:"config_fields"`
	CredentialFields []FieldSchema `json:"credential_fields"`

	// From ui.json
	Label          string `json:"label"`
	Description    string `json:"description"`
	Icon           string `json:"icon"`
	RemoteSetupURL string `json:"remote_setup_url,omitempty"`

	// From instructions.md
	Instructions string `json:"instructions"`
}

type FieldSchema struct {
	Type        string  `json:"type"`
	Label       string  `json:"label"`
	Description string  `json:"description"`
	Key         string  `json:"key"`
	Required    bool    `json:"required"`
	Sensitive   bool    `json:"sensitive,omitempty"` // Whether the field value should be obfuscated in API responses
	Min         *int    `json:"min,omitempty"`       // Minimum value for numeric fields
	Max         *int    `json:"max,omitempty"`       // Maximum value for numeric fields
	Step        *int    `json:"step,omitempty"`      // Step value for numeric fields
	MinLength   *int    `json:"minlength,omitempty"` // Minimum length for text fields
	MaxLength   *int    `json:"maxlength,omitempty"` // Maximum length for text fields
	Pattern     *string `json:"pattern,omitempty"`   // Regular expression pattern for text fields
}
