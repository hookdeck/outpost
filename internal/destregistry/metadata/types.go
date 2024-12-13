// internal/destregistry/metadata/types.go
package metadata

import "github.com/santhosh-tekuri/jsonschema/v6"

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

	// From validation.json
	ValidationSchema map[string]interface{} `json:"validation"` // Raw JSON schema
	Validation       *jsonschema.Schema     `json:"-"`          // Compiled schema for internal use
}

type FieldSchema struct {
	Type        string `json:"type"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Key         string `json:"key"`
	Required    bool   `json:"required"`
	Sensitive   bool   `json:"sensitive,omitempty"` // Whether the field value should be obfuscated in API responses
}
