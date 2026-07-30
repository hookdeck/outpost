package config

import (
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	Port       int
	OutpostURL string
	APIKey     string
	MockURL    string
	// ExportDir is where run artifacts land. Rendering a figure from an
	// immutable export rather than a live query is what keeps a published
	// number reproducible after Prometheus retention rolls.
	ExportDir string
}

func Load() (*Config, error) {
	port := 9090
	if v := os.Getenv("PORT"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("invalid PORT: %w", err)
		}
		port = p
	}

	outpostURL := os.Getenv("OUTPOST_URL")
	if outpostURL == "" {
		return nil, fmt.Errorf("OUTPOST_URL is required")
	}

	apiKey := os.Getenv("OUTPOST_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OUTPOST_API_KEY is required")
	}

	mockURL := os.Getenv("MOCK_URL")
	if mockURL == "" {
		return nil, fmt.Errorf("MOCK_URL is required")
	}

	exportDir := os.Getenv("EXPORT_DIR")
	if exportDir == "" {
		exportDir = "/data/runs"
	}

	return &Config{
		Port:       port,
		OutpostURL: outpostURL,
		APIKey:     apiKey,
		MockURL:    mockURL,
		ExportDir:  exportDir,
	}, nil
}
