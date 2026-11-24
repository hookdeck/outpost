package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// HeaderTemplates is a header name -> value template map. From the environment
// it is parsed as comma-separated "name=template" pairs, where "\," is a
// literal comma. From YAML it is a plain map, or the same string form.
type HeaderTemplates map[string]string

func (h *HeaderTemplates) UnmarshalText(b []byte) error {
	parsed, err := parseHeaderTemplates(string(b))
	if err != nil {
		return err
	}
	*h = parsed
	return nil
}

func (h *HeaderTemplates) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		var s string
		if err := node.Decode(&s); err != nil {
			return err
		}
		return h.UnmarshalText([]byte(s))
	default:
		var m map[string]string
		if err := node.Decode(&m); err != nil {
			return err
		}
		*h = m
		return nil
	}
}

func parseHeaderTemplates(value string) (HeaderTemplates, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	result := HeaderTemplates{}
	for _, pair := range splitUnescaped(value, ',') {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		name, tmpl, found := strings.Cut(pair, "=")
		name = strings.TrimSpace(name)
		if !found || name == "" {
			return nil, fmt.Errorf("%q should be in \"name=template\" format", pair)
		}
		result[name] = strings.ReplaceAll(strings.TrimSpace(tmpl), `\,`, ",")
	}
	return result, nil
}

// splitUnescaped splits on sep, ignoring occurrences preceded by a backslash.
// The backslash is kept for the caller to unescape.
func splitUnescaped(value string, sep byte) []string {
	var parts []string
	start := 0
	for i := 0; i < len(value); i++ {
		if value[i] == sep && (i == 0 || value[i-1] != '\\') {
			parts = append(parts, value[start:i])
			start = i + 1
		}
	}
	return append(parts, value[start:])
}
