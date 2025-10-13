package idgen

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"text/template"

	"github.com/google/uuid"
)

var (
	eventGenerator *IDGenerator
)

func init() {
	// Initialize with default UUID v4 generator
	eventGenerator, _ = NewIDGenerator("{{uuidv4}}")
}

// IDGenerator generates IDs based on a template
type IDGenerator struct {
	template *template.Template
}

// NewIDGenerator creates a new ID generator with the given template string
func NewIDGenerator(templateStr string) (*IDGenerator, error) {
	if templateStr == "" {
		templateStr = "{{uuidv4}}"
	}

	// Create template with custom functions
	tmpl := template.New("id").Funcs(template.FuncMap{
		"uuidv4": func() string {
			return uuid.New().String()
		},
		"uuidv7": func() string {
			id, err := uuid.NewV7()
			if err != nil {
				// Fallback to v4 if v7 generation fails
				return uuid.New().String()
			}
			return id.String()
		},
		"nanoid": func() string {
			return generateNanoid(21) // default size of 21
		},
	})

	// Parse template
	parsed, err := tmpl.Parse(templateStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ID template: %w", err)
	}

	return &IDGenerator{template: parsed}, nil
}

// Generate generates a new ID using the template
func (g *IDGenerator) Generate() (string, error) {
	var buf bytes.Buffer
	if err := g.template.Execute(&buf, nil); err != nil {
		return "", fmt.Errorf("failed to generate ID: %w", err)
	}
	return buf.String(), nil
}

// generateNanoid generates a nanoid-like ID
// This is a simplified implementation inspired by nanoid
// Uses URL-safe base64 alphabet
func generateNanoid(size int) string {
	// URL-safe alphabet (A-Za-z0-9_-)
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

	bytes := make([]byte, size)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to UUID if random generation fails
		return uuid.New().String()
	}

	// Map random bytes to alphabet
	result := make([]byte, size)
	for i := 0; i < size; i++ {
		result[i] = alphabet[int(bytes[i])%len(alphabet)]
	}

	return string(result)
}

// Helper to encode bytes to base64 URL-safe string
func encodeBase64URL(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

// IDTemplateConfig contains ID generation templates for different entity types
type IDTemplateConfig struct {
	Event string
}

// Configure configures all ID generators based on the provided config
// This should be called once at application startup before any concurrent usage
func Configure(cfg IDTemplateConfig) error {
	// Configure event generator if template is provided
	if cfg.Event != "" {
		gen, err := NewIDGenerator(cfg.Event)
		if err != nil {
			return fmt.Errorf("failed to configure event ID generator: %w", err)
		}
		eventGenerator = gen
	}

	return nil
}

// Event generates an event ID using the configured generator.
// Defaults to UUID v4 if not configured via Configure().
func Event() string {
	id, err := eventGenerator.Generate()
	if err != nil {
		// Fallback to UUID v4 on error
		return uuid.New().String()
	}

	return id
}
