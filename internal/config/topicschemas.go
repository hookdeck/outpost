package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/hookdeck/outpost/internal/topicschema"
	"gopkg.in/yaml.v3"
)

// TopicSchemas holds TOPICS_SCHEMAS: topic definitions keyed by topic name,
// as raw JSON. Only LoadTopicCatalog parses it and reports its errors, so a
// service that never loads the catalog never fails on it.
//
// From the environment it is a JSON object. From YAML it is a mapping, kept
// as JSON with YAML 1.2 core types (dates and timestamps stay strings), or a
// string containing JSON. A mapping that can't be converted, for example
// because it uses merge keys, keeps the error for LoadTopicCatalog.
type TopicSchemas struct {
	raw json.RawMessage
	// err is why the YAML mapping couldn't be converted to JSON.
	err error
}

// NewTopicSchemas returns TopicSchemas holding the given JSON. Intended for
// tests and programmatic config construction.
func NewTopicSchemas(raw string) TopicSchemas {
	var t TopicSchemas
	_ = t.UnmarshalText([]byte(raw))
	return t
}

// IsSet reports whether a non-empty value was provided, including a YAML
// mapping that couldn't be converted.
func (t TopicSchemas) IsSet() bool {
	return len(t.raw) > 0 || t.err != nil
}

// Raw returns the JSON as provided. It must not be modified.
func (t TopicSchemas) Raw() json.RawMessage {
	return t.raw
}

func (t *TopicSchemas) UnmarshalText(b []byte) error {
	t.err = nil
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		t.raw = nil
		return nil
	}
	t.raw = json.RawMessage(bytes.Clone(b))
	return nil
}

func (t *TopicSchemas) UnmarshalYAML(node *yaml.Node) error {
	switch {
	case node.Tag == "!!null":
		// A bare `topics_schemas:` is unset, like an absent key.
		t.raw, t.err = nil, nil
		return nil
	case node.Kind == yaml.ScalarNode:
		var s string
		if err := node.Decode(&s); err != nil {
			return err
		}
		return t.UnmarshalText([]byte(s))
	default:
		// The error is kept, not returned: returning it would fail parsing
		// the config file for every service and command.
		t.raw, t.err = topicschema.YAMLNodeToJSON(node)
		if t.err != nil {
			t.raw, t.err = nil, fmt.Errorf("topics_schemas: %w", t.err)
		}
		return nil
	}
}

// topicSchemasError matches ErrInvalidTopicSchemas and the cause, while its
// message stays the cause's: a *topicschema.ConfigError already reads
// "invalid topic schemas: ...".
type topicSchemasError struct {
	err error
}

func (e *topicSchemasError) Error() string   { return e.err.Error() }
func (e *topicSchemasError) Unwrap() []error { return []error{ErrInvalidTopicSchemas, e.err} }

func invalidTopicSchemas(err error) error {
	var configErr *topicschema.ConfigError
	if !errors.As(err, &configErr) {
		err = fmt.Errorf("invalid topic schemas: %w", err)
	}
	return &topicSchemasError{err: err}
}

// validateTopicSchemas checks the shape of the topic schema settings. It does
// no I/O: schemas are read and compiled by LoadTopicCatalog.
func (c *Config) validateTopicSchemas() error {
	if c.TopicsSchemas.IsSet() && c.TopicsSchemasFile != "" {
		return fmt.Errorf("%w: TOPICS_SCHEMAS and TOPICS_SCHEMAS_FILE are mutually exclusive", ErrInvalidTopicSchemas)
	}
	return nil
}

// LoadTopicCatalog reads and compiles the topic schemas, then stores the
// catalog for TopicCatalog. It is the only topic schema step that does I/O:
// TOPICS_SCHEMAS_FILE is read through the OSInterface the config was parsed
// with. Errors match ErrInvalidTopicSchemas.
func (c *Config) LoadTopicCatalog() (*topicschema.Catalog, error) {
	defs, err := c.loadTopicDefinitions()
	if err != nil {
		return nil, invalidTopicSchemas(err)
	}
	catalog, err := topicschema.NewCatalog(c.Topics, defs)
	if err != nil {
		return nil, invalidTopicSchemas(err)
	}
	c.SetTopicCatalog(catalog)
	return catalog, nil
}

// SetTopicCatalog stores the catalog returned by TopicCatalog.
func (c *Config) SetTopicCatalog(catalog *topicschema.Catalog) {
	c.topicCatalog = catalog
}

// TopicCatalog returns the catalog stored by LoadTopicCatalog, or a catalog
// of schema-less TOPICS when none was loaded.
func (c *Config) TopicCatalog() *topicschema.Catalog {
	if c.topicCatalog != nil {
		return c.topicCatalog
	}
	return topicschema.EmptyCatalog(c.Topics)
}

// osOrDefault returns the OSInterface the config was parsed with, or the
// real OS for a config built in code.
func (c *Config) osOrDefault() OSInterface {
	if c.osInterface != nil {
		return c.osInterface
	}
	return defaultOS
}

// loadTopicDefinitions parses TOPICS_SCHEMAS or TOPICS_SCHEMAS_FILE. A file
// ending in .json is JSON; any other is YAML.
func (c *Config) loadTopicDefinitions() (topicschema.Definitions, error) {
	if c.TopicsSchemas.err != nil {
		return nil, c.TopicsSchemas.err
	}
	if c.TopicsSchemas.IsSet() {
		return topicschema.ParseDefinitionsJSON(c.TopicsSchemas.Raw())
	}
	if c.TopicsSchemasFile == "" {
		return nil, nil
	}
	data, err := c.osOrDefault().ReadFile(c.TopicsSchemasFile)
	if err != nil {
		return nil, fmt.Errorf("reading TOPICS_SCHEMAS_FILE: %w", err)
	}
	if strings.EqualFold(filepath.Ext(c.TopicsSchemasFile), ".json") {
		return topicschema.ParseDefinitionsJSON(data)
	}
	return topicschema.ParseDefinitionsYAML(data)
}
