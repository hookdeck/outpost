package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hookdeck/outpost/internal/topicschema"
	"gopkg.in/yaml.v3"
)

// MCPEventsSchema holds experimental.mcp.events_schema: event definitions in the MCP
// events/list format, as a YAML mapping, or the path of a JSON or YAML file
// with them. It is parsed at validation, so a bad value doesn't fail loading
// the config file for every command.
type MCPEventsSchema struct {
	node    *yaml.Node
	path    string
	inline  string
	fromEnv bool
}

func (e MCPEventsSchema) isSet() bool {
	return e.node != nil || e.path != "" || e.inline != ""
}

// key names the setting as the user wrote it.
func (e MCPEventsSchema) key() string {
	if e.fromEnv {
		return "EXPERIMENTAL_MCP_EVENTS_SCHEMA"
	}
	return "experimental.mcp.events_schema"
}

func (e *MCPEventsSchema) UnmarshalYAML(node *yaml.Node) error {
	*e = MCPEventsSchema{}
	switch {
	case node.Tag == "!!null":
	case node.Kind == yaml.MappingNode:
		e.node = node
	case node.Kind == yaml.ScalarNode && node.Tag == "!!str":
		e.path = strings.TrimSpace(node.Value)
	default:
		return errors.New("experimental.mcp.events_schema must be a mapping or a file path")
	}
	return nil
}

func (e *MCPEventsSchema) UnmarshalText(text []byte) error {
	value := strings.TrimSpace(string(text))
	*e = MCPEventsSchema{fromEnv: true}
	if strings.HasPrefix(value, "{") {
		e.inline = value
	} else {
		e.path = value
	}
	return nil
}

func (c *Config) validateMCPEvents() error {
	c.mcpCatalog = nil
	defs, source, err := c.loadMCPEventDefinitions()
	if err != nil {
		return invalidMCPEvents(source, err)
	}
	if len(defs) == 0 {
		return nil
	}
	catalog, err := topicschema.NewCatalog(c.Topics, defs)
	if err != nil {
		return invalidMCPEvents(source, err)
	}
	c.mcpCatalog = catalog
	return nil
}

// MCPCatalog returns the compiled event catalog, or nil when MCP is off or no
// events are configured.
func (c *Config) MCPCatalog() *topicschema.Catalog {
	return c.mcpCatalog
}

// loadMCPEventDefinitions returns the configured definitions and the source
// to name in errors: the setting, and the file when there is one.
func (c *Config) loadMCPEventDefinitions() (topicschema.Definitions, string, error) {
	e := c.Experimental.MCP.EventsSchema
	if !e.isSet() {
		return nil, "", nil
	}
	if e.node != nil {
		defs, err := topicschema.ParseDefinitionsNode(e.node)
		return defs, e.key(), err
	}
	if e.inline != "" {
		defs, err := topicschema.ParseDefinitionsJSON([]byte(e.inline))
		return defs, e.key(), err
	}
	lower := strings.ToLower(e.path)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return nil, e.key(), errors.New("URLs are not supported; set a file path or inline definitions")
	}
	source := fmt.Sprintf("%s %q", e.key(), e.path)
	data, err := c.osOrDefault().ReadFile(e.path)
	if err != nil {
		return nil, e.key(), fmt.Errorf("reading %q: %w", e.path, err)
	}
	if strings.HasSuffix(lower, ".json") {
		defs, err := topicschema.ParseDefinitionsJSON(data)
		return defs, source, err
	}
	defs, err := topicschema.ParseDefinitions(data)
	return defs, source, err
}

func (c *Config) osOrDefault() OSInterface {
	if c.osInterface != nil {
		return c.osInterface
	}
	return defaultOS
}

type mcpEventsError struct {
	err error
}

func (e *mcpEventsError) Error() string   { return e.err.Error() }
func (e *mcpEventsError) Unwrap() []error { return []error{ErrInvalidMCPConfig, e.err} }

// invalidMCPEvents prefixes err with the setting it came from.
func invalidMCPEvents(source string, err error) error {
	return &mcpEventsError{err: fmt.Errorf("%s: %w", source, err)}
}
