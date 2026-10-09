package topicschema

// SKELETON — the real implementation replaces this file. It exists so that
// packages integrating the catalog compile against the final API.

// Option configures NewCatalog.
type Option func(*catalogOptions)

type catalogOptions struct {
	imported           Definitions
	maxValidationBytes int
}

// WithImported adds definitions from an import source such as an OpenAPI
// document. Entries for topics missing from TOPICS are skipped with a warning
// instead of failing, and explicit definitions win per topic.
func WithImported(defs Definitions) Option {
	return func(o *catalogOptions) { o.imported = defs }
}

// WithMaxValidationBytes caps the size of event data validated at publish.
func WithMaxValidationBytes(n int) Option {
	return func(o *catalogOptions) { o.maxValidationBytes = n }
}

// Catalog is the immutable, compiled set of topics and their schemas.
type Catalog struct {
	topics   []Topic
	byName   map[string]int
	warnings []string
}

// NewCatalog validates defs against topics and compiles their schemas.
func NewCatalog(topics []string, defs Definitions, opts ...Option) (*Catalog, error) {
	if len(defs) > 0 {
		return nil, &ConfigError{Problems: []string{"topic schemas are not implemented yet"}}
	}
	return EmptyCatalog(topics), nil
}

// EmptyCatalog returns a catalog of schema-less topics.
func EmptyCatalog(topics []string) *Catalog {
	c := &Catalog{byName: make(map[string]int, len(topics))}
	for _, name := range topics {
		if _, dup := c.byName[name]; dup {
			continue
		}
		c.byName[name] = len(c.topics)
		c.topics = append(c.topics, Topic{Name: name, Validation: ValidationOff})
	}
	return c
}

// Topics returns one entry per TOPICS entry, in TOPICS order.
func (c *Catalog) Topics() []Topic {
	if c == nil {
		return []Topic{}
	}
	out := make([]Topic, len(c.topics))
	copy(out, c.topics)
	return out
}

// Topic returns the named topic.
func (c *Catalog) Topic(name string) (Topic, bool) {
	if c == nil {
		return Topic{}, false
	}
	i, ok := c.byName[name]
	if !ok {
		return Topic{}, false
	}
	return c.topics[i], true
}

// HasSchemas reports whether any topic has a schema definition.
func (c *Catalog) HasSchemas() bool { return false }

// MCPEnabled reports whether any topic is exposed to MCP.
func (c *Catalog) MCPEnabled() bool { return false }

// MCPTopics returns the MCP-enabled topic names in TOPICS order.
func (c *Catalog) MCPTopics() []string { return nil }

// Warnings returns non-fatal configuration problems found while building.
func (c *Catalog) Warnings() []string {
	if c == nil {
		return nil
	}
	return c.warnings
}

// ValidateData validates event data against the topic's payload schema.
func (c *Catalog) ValidateData(topic string, data []byte) ValidationResult {
	return ValidationResult{Mode: ValidationOff}
}

// ValidateArguments validates subscription arguments against the topic's
// inferred inputSchema. It returns nil when they are valid.
func (c *Catalog) ValidateArguments(topic string, args []byte) []string { return nil }

// Arguments returns the topic's filterable properties.
func (c *Catalog) Arguments(topic string) []Argument { return nil }

// MCPEvents returns the precomputed events/list entries in TOPICS order.
func (c *Catalog) MCPEvents() []*MCPEvent { return nil }

// MCPEvent returns the precomputed events/list entry for name.
func (c *Catalog) MCPEvent(name string) (*MCPEvent, bool) { return nil, false }

// Snapshot returns the evolution contract for the catalog.
func (c *Catalog) Snapshot() Snapshot { return Snapshot{Topics: map[string]SnapshotTopic{}} }
