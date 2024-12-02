// cmd/tools/genprovider/main.go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type CoreMetadata struct {
	Type             string        `json:"type"`
	ConfigFields     []FieldSchema `json:"config_fields"`
	CredentialFields []FieldSchema `json:"credential_fields"`
}

type UIMetadata struct {
	Label          string `json:"label"`
	Description    string `json:"description"`
	Icon           string `json:"icon"`
	RemoteSetupURL string `json:"remote_setup_url,omitempty"`
}

type FieldSchema struct {
	Type        string `json:"type"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Key         string `json:"key"`
	Required    bool   `json:"required"`
}

type ValidationSchema struct {
	Schema     string                 `json:"$schema"`
	Type       string                 `json:"type"`
	Properties map[string]interface{} `json:"properties"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("Usage: genprovider <provider-name>")
		os.Exit(1)
	}

	provider := os.Args[1]
	baseDir := "internal/destregistry/metadata/providers"
	providerDir := filepath.Join(baseDir, provider)

	// Create provider directory
	if err := os.MkdirAll(providerDir, 0755); err != nil {
		fmt.Printf("Error creating provider directory: %v\n", err)
		os.Exit(1)
	}

	// Generate core.json
	core := CoreMetadata{
		Type:             provider,
		ConfigFields:     []FieldSchema{},
		CredentialFields: []FieldSchema{},
	}
	writeJSON(filepath.Join(providerDir, "core.json"), core)

	// Generate ui.json
	ui := UIMetadata{
		Label:       strings.ToTitle(provider),
		Description: fmt.Sprintf("Send events to %s", provider),
		Icon:        "",
	}
	writeJSON(filepath.Join(providerDir, "ui.json"), ui)

	// Generate validation.json
	validation := ValidationSchema{
		Schema: "http://json-schema.org/draft-07/schema#",
		Type:   "object",
		Properties: map[string]interface{}{
			"config": map[string]interface{}{
				"type": "object",
			},
			"credentials": map[string]interface{}{
				"type": "object",
			},
		},
	}
	writeJSON(filepath.Join(providerDir, "validation.json"), validation)

	// Generate instructions.md
	instructions := fmt.Sprintf("# %s Setup Instructions\n\nBasic setup instructions for %s destination.",
		strings.ToTitle(provider), provider)
	writeFile(filepath.Join(providerDir, "instructions.md"), instructions)

	fmt.Printf("Created provider files for %s in %s\n", provider, providerDir)
}

func writeJSON(path string, v interface{}) {
	data, err := json.MarshalIndent(v, "", "    ")
	if err != nil {
		fmt.Printf("Error marshaling JSON for %s: %v\n", path, err)
		return
	}
	writeFile(path, string(data))
}

func writeFile(path string, content string) {
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		fmt.Printf("Error writing file %s: %v\n", path, err)
	}
}
