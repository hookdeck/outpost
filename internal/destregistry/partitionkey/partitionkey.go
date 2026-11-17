package partitionkey

import (
	"errors"
	"fmt"

	"github.com/hookdeck/outpost/internal/destregistry/safejmespath"
)

// Evaluate extracts a partition key from payload using a JMESPath template.
// If the template is empty or evaluation returns nil/empty, fallbackKey is returned.
func Evaluate(template string, payload map[string]interface{}, fallbackKey string) (string, error) {
	if template == "" {
		return fallbackKey, nil
	}

	result, err := safejmespath.Search(template, payload)
	if err != nil {
		// Only a panic is a TemplateError: callers fail the attempt on it and
		// fall back to the event ID on any other error.
		if errors.Is(err, safejmespath.ErrPanic) {
			return "", &safejmespath.TemplateError{Field: "partition_key_template", Err: err}
		}
		return "", fmt.Errorf("error evaluating partition key template: %w", err)
	}

	if result == nil {
		return fallbackKey, nil
	}

	switch v := result.(type) {
	case string:
		if v == "" {
			return fallbackKey, nil
		}
		return v, nil
	case float64:
		return fmt.Sprintf("%g", v), nil
	case int:
		return fmt.Sprintf("%d", v), nil
	case bool:
		return fmt.Sprintf("%t", v), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}
