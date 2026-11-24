package partitionkey

import (
	"errors"
	"fmt"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/safejmespath"
	"github.com/jmespath/go-jmespath"
)

// Template is a partition key template, compiled once.
type Template struct {
	compiled *jmespath.JMESPath
}

// Validate checks a template on create and update: it must parse and be one
// of the accepted forms. An empty template is valid.
func Validate(template string) error {
	if template == "" {
		return nil
	}
	if err := safejmespath.Check(template); err != nil {
		return patternError()
	}
	return nil
}

// New compiles the template of a stored destination. A template that does not
// parse is not an error: it yields the fallback key, as it always has. A
// template that parses but is not an accepted form is an error and is never
// evaluated.
func New(template string) (*Template, error) {
	if template == "" {
		return &Template{}, nil
	}
	compiled, err := safejmespath.Compile(template)
	if errors.Is(err, safejmespath.ErrNotAccepted) {
		return nil, patternError()
	}
	if err != nil {
		return &Template{}, nil
	}
	return &Template{compiled: compiled}, nil
}

func patternError() error {
	return destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
		{Field: "config.partition_key_template", Type: "pattern"},
	})
}

// Evaluate extracts a partition key from payload.
// If there is no template or evaluation returns nil/empty, fallbackKey is returned.
func (t *Template) Evaluate(payload map[string]interface{}, fallbackKey string) (string, error) {
	if t == nil || t.compiled == nil {
		return fallbackKey, nil
	}

	result, err := t.compiled.Search(payload)
	if err != nil {
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
