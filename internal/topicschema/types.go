// Package topicschema holds MCP event definitions and the inputSchemas
// inferred from them. A Catalog is built once at startup from the
// definitions, and is immutable afterwards, so it is safe for concurrent reads.
package topicschema

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

// Definition is one event's definition.
type Definition struct {
	Description string `json:"description,omitempty"`
	// PayloadSchema is a JSON Schema 2020-12 document for the event data.
	PayloadSchema json.RawMessage `json:"payload_schema,omitempty"`
}

// Definitions maps event name to its definition.
type Definitions map[string]Definition

// Event is one precomputed events/list entry.
type Event struct {
	Name        string
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
	// accepted: numbers, and strings with a date format. Filters compare
	// strings bytewise, so date-time strings get equality and lists only.
	Ranged bool
}

// ConfigError aggregates every problem found in event definition configuration,
// so a startup failure reports all of them at once.
type ConfigError struct {
	Problems []string
}

func (e *ConfigError) Error() string {
	if len(e.Problems) == 1 {
		return "invalid mcp events: " + e.Problems[0]
	}
	return "invalid mcp events:\n  - " + strings.Join(e.Problems, "\n  - ")
}

// Problems are bounded: at most maxReportedProblems are kept, and values they
// quote from configuration, such as references and keys, are clipped to
// maxQuotedBytes.
const (
	maxReportedProblems = 100
	maxQuotedBytes      = 200
)

// clip shortens s to maxQuotedBytes at a character boundary, marking the cut
// with "...".
func clip(s string) string {
	if len(s) <= maxQuotedBytes {
		return s
	}
	cut := maxQuotedBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

// limitMessages sorts msgs, drops duplicates and keeps the first
// maxReportedProblems, followed by "... and N more" when some were left out.
func limitMessages(msgs []string) []string {
	out := slices.Clone(msgs)
	slices.Sort(out)
	out = slices.Compact(out)
	if len(out) > maxReportedProblems {
		more := len(out) - maxReportedProblems
		out = append(out[:maxReportedProblems], moreMessage(more))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func moreMessage(n int) string {
	return fmt.Sprintf("... and %d more", n)
}
