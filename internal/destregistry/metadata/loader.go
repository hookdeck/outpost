package metadata

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed providers/*
var defaultFS embed.FS

type FSMetadataLoader struct {
	basePath string
}

func NewMetadataLoader(basePath string) MetadataLoader {
	return &FSMetadataLoader{
		basePath: basePath,
	}
}

func (l *FSMetadataLoader) Load(providerType string) (*ProviderMetadata, error) {
	metadata := &ProviderMetadata{}

	if err := l.loadCore(providerType, metadata); err != nil {
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

func (l *FSMetadataLoader) loadCore(providerType string, metadata *ProviderMetadata) error {
	if err := l.loadJSONFile(providerType, "core.json", metadata); err != nil {
		return fmt.Errorf("loading core metadata: %w", err)
	}
	return nil
}

func (l *FSMetadataLoader) loadUI(providerType string, metadata *ProviderMetadata) error {
	if err := l.loadJSONFile(providerType, "ui.json", metadata); err != nil {
		return fmt.Errorf("loading UI metadata: %w", err)
	}
	return nil
}

func (l *FSMetadataLoader) loadInstructions(providerType string, metadata *ProviderMetadata) error {
	instructionsBytes, err := l.loadFile(providerType, "instructions.md")
	if err != nil {
		return fmt.Errorf("loading instructions: %w", err)
	}
	metadata.Instructions = string(instructionsBytes)
	return nil
}

// loadFile tries filesystem first, falls back to embedded
func (l *FSMetadataLoader) loadFile(providerType, filename string) ([]byte, error) {
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

func (l *FSMetadataLoader) loadJSONFile(providerType, filename string, v interface{}) error {
	bytes, err := l.loadFile(providerType, filename)
	if err != nil {
		return err
	}

	if err := json.Unmarshal(bytes, v); err != nil {
		return fmt.Errorf("parsing JSON from %s: %w", filename, err)
	}

	return nil
}
