// internal/destregistry/metadata/types.go
package metadata

import "github.com/santhosh-tekuri/jsonschema/v6"

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

	// From other files
	Instructions string             // from instructions.md
	Validation   *jsonschema.Schema // from validation.json
}

type FieldSchema struct {
	Type        string `json:"type"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Key         string `json:"key"`
	Required    bool   `json:"required"`
}
