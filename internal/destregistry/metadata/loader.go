package metadata

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed providers/*
var defaultFS embed.FS

type MetadataLoader struct {
	basePath string
}

func NewMetadataLoader(basePath string) *MetadataLoader {
	return &MetadataLoader{
		basePath: basePath,
	}
}

func (l *MetadataLoader) Load(providerType string) (*ProviderMetadata, error) {
	metadata := &ProviderMetadata{}

	if err := l.loadCore(providerType, metadata); err != nil {
		return nil, err
	}

	if err := l.loadValidation(providerType, metadata); err != nil {
		return nil, err
	}

	if err := l.loadUI(providerType, metadata); err != nil {
		return nil, err
	}

	if err := l.loadInstructions(providerType, metadata); err != nil {
		return nil, err
	}

	return metadata, nil
}

func (l *MetadataLoader) loadCore(providerType string, metadata *ProviderMetadata) error {
	if err := l.loadJSONFile(providerType, "core.json", metadata); err != nil {
		return fmt.Errorf("loading core metadata: %w", err)
	}
	return nil
}

func (l *MetadataLoader) loadValidation(providerType string, metadata *ProviderMetadata) error {
	validationBytes, err := l.loadFile(providerType, "validation.json")
	if err != nil {
		return fmt.Errorf("loading validation schema: %w", err)
	}

	// Parse and store the raw schema
	if err := json.Unmarshal(validationBytes, &metadata.ValidationSchema); err != nil {
		return fmt.Errorf("parsing validation schema: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("schema.json", metadata.ValidationSchema); err != nil {
		return fmt.Errorf("adding validation schema: %w", err)
	}

	metadata.Validation, err = compiler.Compile("schema.json")
	if err != nil {
		return fmt.Errorf("compiling validation schema: %w", err)
	}

	return nil
}

func (l *MetadataLoader) loadUI(providerType string, metadata *ProviderMetadata) error {
	if err := l.loadJSONFile(providerType, "ui.json", metadata); err != nil {
		return fmt.Errorf("loading UI metadata: %w", err)
	}
	return nil
}

func (l *MetadataLoader) loadInstructions(providerType string, metadata *ProviderMetadata) error {
	instructionsBytes, err := l.loadFile(providerType, "instructions.md")
	if err != nil {
		return fmt.Errorf("loading instructions: %w", err)
	}
	metadata.Instructions = string(instructionsBytes)
	return nil
}

// loadFile tries filesystem first, falls back to embedded
func (l *MetadataLoader) loadFile(providerType, filename string) ([]byte, error) {
	// Try filesystem first if basePath is set
	if l.basePath != "" {
		path := filepath.Join(l.basePath, providerType, filename)
		if bytes, err := os.ReadFile(path); err == nil {
			return bytes, nil
		}
		// Don't return error here, try embedded next
	}

	// Fall back to embedded
	bytes, err := defaultFS.ReadFile(filepath.Join("providers", providerType, filename))
	if err != nil {
		return nil, fmt.Errorf("file %s not found in filesystem or embedded", filename)
	}
	return bytes, nil
}

func (l *MetadataLoader) loadJSONFile(providerType, filename string, v interface{}) error {
	bytes, err := l.loadFile(providerType, filename)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(bytes, v); err != nil {
		return fmt.Errorf("parsing JSON from %s: %w", filename, err)
	}

	return nil
}
