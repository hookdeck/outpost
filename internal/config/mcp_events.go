package config

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hookdeck/outpost/internal/topicschema"
	"gopkg.in/yaml.v3"
)

// MCPEvents holds mcp.events, the event definitions as a YAML mapping keyed by
// event name. It is parsed at validation, so a bad value doesn't fail loading
// the config file for every command.
type MCPEvents struct {
	node *yaml.Node
}

func (e MCPEvents) isSet() bool {
	return e.node != nil
}

func (e *MCPEvents) UnmarshalYAML(node *yaml.Node) error {
	e.node = nil
	if node.Tag != "!!null" {
		e.node = node
	}
	return nil
}

func (c *Config) validateMCPEvents() error {
	c.mcpCatalog = nil
	defs, err := c.loadMCPEventDefinitions()
	if err != nil {
		return invalidMCPEvents(err)
	}
	if len(defs) == 0 {
		return nil
	}
	catalog, err := topicschema.NewCatalog(c.Topics, defs)
	if err != nil {
		return invalidMCPEvents(err)
	}
	c.mcpCatalog = catalog
	return nil
}

// MCPCatalog returns the compiled event catalog, or nil when MCP is off or no
// events are configured.
func (c *Config) MCPCatalog() *topicschema.Catalog {
	return c.mcpCatalog
}

func (c *Config) loadMCPEventDefinitions() (topicschema.Definitions, error) {
	if c.MCP.EventsFile != "" {
		data, err := c.osOrDefault().ReadFile(c.MCP.EventsFile)
		if err != nil {
			return nil, fmt.Errorf("reading MCP_EVENTS_FILE: %w", err)
		}
		if strings.HasSuffix(strings.ToLower(c.MCP.EventsFile), ".json") {
			return topicschema.ParseDefinitionsJSON(data)
		}
		return topicschema.ParseDefinitions(data)
	}
	if c.MCP.Events.isSet() {
		return topicschema.ParseDefinitionsNode(c.MCP.Events.node)
	}
	return nil, nil
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

func invalidMCPEvents(err error) error {
	var configErr *topicschema.ConfigError
	if !errors.As(err, &configErr) {
		err = fmt.Errorf("invalid mcp events: %w", err)
	}
	return &mcpEventsError{err: err}
}
