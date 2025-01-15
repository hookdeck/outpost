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
	// First load the embedded metadata
	embeddedMetadata := &ProviderMetadata{}
	if err := l.loadEmbeddedJSONFile(providerType, "metadata.json", embeddedMetadata); err != nil {
		return nil, fmt.Errorf("loading embedded metadata: %w", err)
	}

	// Try to load filesystem metadata for merging
	if l.basePath != "" {
		if fsMetadata, err := l.loadFilesystemMetadata(providerType); err == nil {
			// Merge filesystem metadata into embedded metadata (left merge)
			l.mergeMetadata(embeddedMetadata, fsMetadata)
		}
	}

	// Load instructions separately
	if err := l.loadInstructions(providerType, embeddedMetadata); err != nil {
		return nil, fmt.Errorf("loading instructions: %w", err)
	}

	return embeddedMetadata, nil
}

func (l *FSMetadataLoader) loadFilesystemMetadata(providerType string) (*ProviderMetadata, error) {
	metadata := &ProviderMetadata{}
	path := filepath.Join(l.basePath, providerType, "metadata.json")

	bytes, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(bytes, metadata); err != nil {
		return nil, fmt.Errorf("parsing filesystem metadata.json: %w", err)
	}

	return metadata, nil
}

func (l *FSMetadataLoader) mergeMetadata(base, override *ProviderMetadata) {
	// Only override non-empty values
	if override.Label != "" {
		base.Label = override.Label
	}
	if override.Description != "" {
		base.Description = override.Description
	}
	if override.Icon != "" {
		base.Icon = override.Icon
	}
	if override.RemoteSetupURL != "" {
		base.RemoteSetupURL = override.RemoteSetupURL
	}
	// Don't override Type, ConfigFields, or CredentialFields as those are core functionality
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

func (l *FSMetadataLoader) loadEmbeddedJSONFile(providerType, filename string, v interface{}) error {
	bytes, err := defaultFS.ReadFile(filepath.Join("providers", providerType, filename))
	if err != nil {
		return err
	}

	if err := json.Unmarshal(bytes, v); err != nil {
		return fmt.Errorf("parsing JSON from %s: %w", filename, err)
	}

	return nil
}
