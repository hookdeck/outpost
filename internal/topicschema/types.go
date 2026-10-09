// Package topicschema holds topic schema configuration: each topic's
// description, payload JSON Schema, publish-time validation mode and MCP
// opt-in. A Catalog is built once at startup from TOPICS and the schema
// sources, and is immutable afterwards, so it is safe for concurrent reads.
package topicschema

import (
	"encoding/json"
	"strings"
)

// ValidationMode controls publish-time validation of event data against the
// topic's payload schema.
type ValidationMode string

const (
	// ValidationOff skips validation. It is the default.
	ValidationOff ValidationMode = "off"
	// ValidationWarn accepts invalid events and marks them schema_valid: false.
	ValidationWarn ValidationMode = "warn"
	// ValidationEnforce rejects invalid events with 422.
	ValidationEnforce ValidationMode = "enforce"
)

// MCPSettings is a topic's MCP opt-in.
type MCPSettings struct {
	Enabled bool `json:"enabled"`
}

// Definition is one topic's schema configuration as the operator wrote it.
type Definition struct {
	// Name is optional. When set it must equal the topic key.
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	// PayloadSchema is a JSON Schema 2020-12 document for the event data.
	PayloadSchema json.RawMessage `json:"payload_schema,omitempty"`
	// Validation is empty or one of the ValidationMode values. Empty means off.
	Validation ValidationMode `json:"validation,omitempty"`
	MCP        MCPSettings    `json:"mcp"`
	Deprecated bool           `json:"deprecated,omitempty"`
	ReplacedBy string         `json:"replaced_by,omitempty"`
}

// Definitions maps topic name to its definition.
type Definitions map[string]Definition

// Topic is the resolved, public view of one topic, as returned by
// GET /topics in API v2.
type Topic struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// PayloadSchema is the schema as configured, x-mcp-filter included.
	PayloadSchema json.RawMessage `json:"payload_schema,omitempty"`
	Validation    ValidationMode  `json:"validation"`
	MCP           MCPSettings     `json:"mcp"`
	Deprecated    bool            `json:"deprecated,omitempty"`
	ReplacedBy    string          `json:"replaced_by,omitempty"`
}

// ValidationResult is the outcome of validating event data against a topic's
// payload schema.
type ValidationResult struct {
	// Mode is the effective mode: off when the topic has no schema.
	Mode ValidationMode
	// Checked reports whether a schema check ran (mode warn or enforce).
	Checked bool
	// Valid is meaningful only when Checked.
	Valid bool
	// SkippedTooLarge reports that a warn-mode check was skipped because the
	// data exceeds the validation size limit. Enforce mode rejects instead.
	SkippedTooLarge bool
	// Errors lists at most maxReportedErrors problems plus a trailing
	// "... and N more" entry. Entries never contain instance values.
	Errors []string
}

// MCPEvent is one precomputed events/list entry.
type MCPEvent struct {
	Name string
	// Description carries a "Deprecated: use <replaced_by>. " prefix when the
	// topic is deprecated.
	Description string
	InputSchema json.RawMessage
	// PayloadSchema is the topic payload schema minus x-mcp-filter.
	PayloadSchema json.RawMessage
	// JSON is the full entry: name, description, delivery, inputSchema and
	// payloadSchema.
	JSON json.RawMessage
}

// Argument describes one filterable payload property: one optional
// subscription argument in the inferred inputSchema.
type Argument struct {
	Name string
	// Types is a sorted, non-empty subset of string, number, integer, boolean.
	Types []string
	// Enum lists the allowed values, or is nil when unconstrained.
	Enum []json.RawMessage
	// Format is "date-time", "date" or empty. Only set for strings.
	Format string
	// Ranged reports whether an operator object ($gt, $gte, $lt, $lte) is
	// accepted: numbers, and strings with a date or date-time format.
	Ranged bool
}

// Snapshot is the persisted contract used for schema evolution checks.
type Snapshot struct {
	Topics map[string]SnapshotTopic `json:"topics"`
}

// SnapshotTopic is one topic's entry in a Snapshot.
type SnapshotTopic struct {
	MCPEnabled bool `json:"mcp_enabled"`
	// PayloadSchema is the schema as configured, x-mcp-filter included.
	PayloadSchema json.RawMessage `json:"payload_schema,omitempty"`
}

// Change is one breaking change between two snapshots of a topic.
type Change struct {
	Topic string `json:"topic"`
	// Path is a JSON pointer into the payload schema.
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}

// Change kinds.
const (
	ChangePropertyRemoved     = "property_removed"
	ChangeTypeNarrowed        = "type_narrowed"
	ChangeEnumNarrowed        = "enum_narrowed"
	ChangeConstChanged        = "const_changed"
	ChangeConstraintTightened = "constraint_tightened"
	ChangeFilterHidden        = "filter_hidden"
	ChangeCompositeChanged    = "composite_changed"
)

// ConfigError aggregates every problem found in topic schema configuration,
// so a startup failure reports all of them at once.
type ConfigError struct {
	Problems []string
}

func (e *ConfigError) Error() string {
	if len(e.Problems) == 1 {
		return "invalid topic schemas: " + e.Problems[0]
	}
	return "invalid topic schemas:\n  - " + strings.Join(e.Problems, "\n  - ")
}
