package topicschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	// maxPayloadSchemaBytes caps a payload schema, measured compact, because
	// events/list returns every payload schema to MCP clients.
	maxPayloadSchemaBytes = 64 << 10
	// maxArgumentsBytes caps subscription arguments before they are parsed,
	// as the API caps config.arguments.
	maxArgumentsBytes = 4 << 10
	// maxNumberScale bounds the length and the exponent of every number in
	// validated arguments. The validator turns numbers into exact rationals,
	// and 1e999999 alone takes milliseconds, so a value with a larger number
	// fails validation instead of being checked.
	maxNumberScale = 1000
)

// draft2020 is the only $schema accepted in payload schemas.
const draft2020 = "https://json-schema.org/draft/2020-12/schema"

// schemaURL is where schemas are added to the compiler. Relative references
// resolve against it, and loading them fails.
const schemaURL = "https://outpost.invalid/schema.json"

var (
	largeNumberMessage = fmt.Sprintf("must be a number of at most %d characters with an exponent between -%d and %d",
		maxNumberScale, maxNumberScale, maxNumberScale)
	// The inferred argument limits, worded as the validator's errors are.
	longArgumentListMessage   = "must have at most " + countOf(maxArgumentListItems, "item", "items")
	longArgumentStringMessage = "must be at most " + countOf(maxArgumentStringLength, "character", "characters")
)

// argumentChecks are what validateJSON checks before validating
// subscription arguments against their schema.
var argumentChecks = valueChecks{reject: rejectArgumentValue}

// Catalog is the immutable, compiled set of event definitions. It is safe
// for concurrent use: nothing in it changes after NewCatalog returns, and
// validating against a compiled schema doesn't modify it. Returned schemas
// and events share the catalog's memory and must not be modified.
type Catalog struct {
	// topics is the TOPICS list, empty when any topic is allowed.
	topics []string
	byName map[string]*entry
	// events are the events/list entries in name order.
	events []*Event
}

// entry is the compiled state of one event. input is the inputSchema without
// enums, which enums holds instead.
type entry struct {
	event      *Event
	args       []Argument
	input      *jsonschema.Schema
	inputNames map[string]struct{}
	enums      argumentEnums
}

// NewCatalog validates defs and compiles their schemas. When topics, the
// TOPICS list, is not empty, every event must be in it. The problems found
// are reported at once in a *ConfigError, up to 100 of them.
func NewCatalog(topics []string, defs Definitions) (*Catalog, error) {
	c := &Catalog{topics: topics, byName: make(map[string]*entry, len(defs))}
	b := catalogBuilder{c: c}
	for _, name := range slices.Sorted(maps.Keys(defs)) {
		b.addDefinition(name, defs[name])
	}
	if len(b.problems) > 0 {
		return nil, &ConfigError{Problems: limitMessages(b.problems)}
	}
	for _, name := range slices.Sorted(maps.Keys(c.byName)) {
		c.events = append(c.events, c.byName[name].event)
	}
	return c, nil
}

// didYouMean suggests the TOPICS entry name matches once whitespace is
// trimmed. TOPICS entries are used verbatim, so " a" and "a" differ.
func (c *Catalog) didYouMean(name string) string {
	trimmed := strings.TrimSpace(name)
	for _, t := range c.topics {
		if t != name && strings.TrimSpace(t) == trimmed {
			return fmt.Sprintf(" (did you mean %q?)", clip(t))
		}
	}
	return ""
}

// catalogBuilder collects the problems found while building a catalog.
type catalogBuilder struct {
	c        *Catalog
	problems []string
}

func (b *catalogBuilder) problem(topic, format string, args ...any) {
	b.problems = append(b.problems, fmt.Sprintf("topic %q: ", clip(topic))+fmt.Sprintf(format, args...))
}

func (b *catalogBuilder) addDefinition(name string, def Definition) {
	if strings.Contains(name, "*") {
		b.problem(name, "wildcard topics can't have event definitions")
		return
	}
	if len(b.c.topics) > 0 && !slices.Contains(b.c.topics, name) {
		b.problems = append(b.problems, fmt.Sprintf("topic %q is not in TOPICS%s", clip(name), b.c.didYouMean(name)))
		return
	}
	raw := bytes.TrimSpace(def.PayloadSchema)
	if len(raw) == 0 || string(raw) == "null" {
		b.problem(name, "payload_schema is required")
		return
	}
	if e := b.compile(name, def.Description, raw); e != nil {
		b.c.byName[name] = e
	}
}

// compile checks and compiles an event's payload schema, and precomputes what
// MCP needs. It returns nil after reporting a problem.
func (b *catalogBuilder) compile(name, description string, raw json.RawMessage) *entry {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		b.problem(name, "payload_schema is not valid JSON: %v", err)
		return nil
	}
	// Clone to drop the buffer's spare capacity: the catalog keeps this.
	compact := bytes.Clone(buf.Bytes())
	if compact[0] != '{' {
		b.problem(name, "payload_schema must be a JSON object")
		return nil
	}
	if len(compact) > maxPayloadSchemaBytes {
		b.problem(name, "payload_schema is %d bytes; the limit is %d", len(compact), maxPayloadSchemaBytes)
		return nil
	}
	tree, err := decodeJSON(compact)
	if err != nil {
		b.problem(name, "payload_schema is not valid JSON: %v", err)
		return nil
	}
	root := tree.(map[string]any)
	if !b.checkSchemaObjects(name, root) {
		return nil
	}
	isObject := root["type"] == "object"
	if !isObject {
		b.problem(name, `payload_schema must have "type": "object"`)
	}

	_, unsupported, err := compileSchema(tree, false)
	if err != nil {
		for _, p := range compileProblems(err, tree) {
			b.problem(name, "%s", p)
		}
	}
	// Go's RE2 engine rejects some ECMAScript patterns, such as lookaheads.
	for _, msg := range unsupported {
		b.problem(name, "payload_schema %s", msg)
	}
	if err != nil || len(unsupported) > 0 || !isObject {
		return nil
	}

	ev, args, err := buildEvent(name, description, compact, root)
	if err != nil {
		b.problem(name, "can't infer the MCP inputSchema: %v", err)
		return nil
	}
	// The validator compares a value with every enum value, numbers as
	// exact rationals, so it gets the inputSchema without enums, and
	// ValidateArguments checks them against sets.
	_, checked, err := inferArguments(compact, root, false)
	e := &entry{event: ev}
	var inputTree any
	if err == nil {
		inputTree, err = decodeJSON(checked)
	}
	if err == nil {
		var bad []string
		e.input, bad, err = compileSchema(inputTree, true)
		if err == nil && len(bad) > 0 {
			err = errors.New(bad[0])
		}
	}
	if err != nil {
		b.problem(name, "the inferred MCP inputSchema doesn't compile: %s", sanitizeCompileError(err))
		return nil
	}
	e.args, e.inputNames, e.enums = args, propertyNameSet(inputTree), newArgumentEnums(args)
	return e
}

// checkSchemaObjects checks what the metaschema doesn't: $schema is the
// 2020-12 dialect, references are local JSON pointers to schemas, $id and
// anchors are only declared at the root, and x-mcp-filter is a boolean.
//
// Inference resolves references with resolveLocalRef, which follows JSON
// pointers from the root. The validator also resolves anchors, and resolves
// the references below a nested $id against it, so a schema using either
// would be validated against one thing and inferred against another. It also
// compiles whatever a reference points to as a schema, so a reference into an
// extension or a default would bring in an $id, anchors and references never
// checked here.
func (b *catalogBuilder) checkSchemaObjects(name string, root map[string]any) bool {
	const rootPath = "payload_schema"
	before := len(b.problems)
	schemas := map[string]bool{}
	visitSchemas(root, rootPath, func(_ any, path string) { schemas[path] = true })
	visitSchemaObjects(root, rootPath, func(obj map[string]any, path string) {
		if v, ok := obj["$schema"]; ok && v != draft2020 {
			b.problem(name, "%s must be %q", appendPathKey(path, "$schema"), draft2020)
		}
		for _, kw := range []string{"$ref", "$dynamicRef"} {
			ref, ok := obj[kw].(string)
			switch {
			case !ok:
			case !strings.HasPrefix(ref, "#"):
				b.problem(name, "%s %s is not a local reference; only references starting with # are allowed",
					appendPathKey(path, kw), quoteJSONString(clip(ref)))
			case !pointerRef(ref):
				b.problem(name, `%s %s must be a JSON pointer such as "#/$defs/name"; anchors are not supported`,
					appendPathKey(path, kw), quoteJSONString(clip(ref)))
			default:
				// One that doesn't resolve fails to compile.
				if at, ok := localRefPath(root, ref, rootPath); ok && !schemas[at] {
					b.problem(name, `%s %s must point to a schema, such as "#/$defs/name", not into another value such as an extension or a default`,
						appendPathKey(path, kw), quoteJSONString(clip(ref)))
				}
			}
		}
		if path != rootPath {
			if _, ok := obj["$id"]; ok {
				b.problem(name, "%s is only allowed at the payload_schema root, since it changes how references below it resolve",
					appendPathKey(path, "$id"))
			}
			for _, kw := range []string{"$anchor", "$dynamicAnchor"} {
				if _, ok := obj[kw]; ok {
					b.problem(name, `%s is only allowed at the payload_schema root; reference subschemas with JSON pointers such as "#/$defs/name"`,
						appendPathKey(path, kw))
				}
			}
		}
		if v, ok := obj["x-mcp-filter"]; ok {
			if _, isBool := v.(bool); !isBool {
				b.problem(name, "%s must be a boolean", appendPathKey(path, "x-mcp-filter"))
			}
		}
	})
	return len(b.problems) == before
}

// compileProblems renders a compile error as problem lines. Metaschema
// violations are rendered like validation errors, with full paths, since a
// schema is configuration rather than event data.
func compileProblems(err error, tree any) []string {
	var sve *jsonschema.SchemaValidationError
	var verr *jsonschema.ValidationError
	if !errors.As(err, &sve) || !errors.As(sve.Err, &verr) {
		return []string{"payload_schema: " + sanitizeCompileError(err)}
	}
	// The library checks subschemas reached only through $ref separately;
	// URL then points at the subschema.
	root, instance := "payload_schema", tree
	if _, frag, ok := strings.Cut(sve.URL, "#"); ok && frag != "" {
		if node, err := resolveLocalRef(tree, "#"+frag); err == nil {
			var tokens []string
			for _, tok := range strings.Split(strings.TrimPrefix(frag, "/"), "/") {
				tokens = append(tokens, strings.ReplaceAll(strings.ReplaceAll(tok, "~1", "/"), "~0", "~"))
			}
			root, instance = instancePath(root, tokens, tree, nil), node
		}
	}
	lines := renderValidationErrors(verr, root, instance, nil)
	if len(lines) > 0 && strings.HasPrefix(lines[len(lines)-1], "...") {
		lines[len(lines)-1] = root + ": " + lines[len(lines)-1]
	}
	return lines
}

// sanitizeCompileError renders a compile error on one line, with locations
// relative to the schema.
func sanitizeCompileError(err error) string {
	msg := strings.ReplaceAll(err.Error(), schemaURL, "")
	return strings.Join(strings.Fields(msg), " ")
}

// compileSchema compiles doc, a tree from decodeJSON, as a JSON Schema
// 2020-12 document. It also describes the patterns Go's RE2 engine can't
// compile. Those compile to a pattern that matches nothing, so a schema with
// any must not be used to validate.
func compileSchema(doc any, assertFormat bool) (*jsonschema.Schema, []string, error) {
	var unsupported []string
	engine := func(pattern string) (jsonschema.Regexp, error) {
		re, err := regexp.Compile(pattern)
		if err != nil {
			msg := fmt.Sprintf("pattern %s is not supported by Go's RE2 syntax: %v", quoteJSONString(clip(pattern)), clip(err.Error()))
			if !slices.Contains(unsupported, msg) {
				unsupported = append(unsupported, msg)
			}
			return unsupportedRegexp(pattern), nil
		}
		return re, nil
	}
	c := jsonschema.NewCompiler()
	c.DefaultDraft(jsonschema.Draft2020)
	c.UseLoader(refuseLoader{})
	c.UseRegexpEngine(engine)
	if assertFormat {
		c.AssertFormat()
	}
	if err := c.AddResource(schemaURL, doc); err != nil {
		return nil, nil, err
	}
	schema, err := c.Compile(schemaURL)
	return schema, unsupported, err
}

// refuseLoader fails every external resource: schemas may only reference
// themselves, and compiling one must never read files or the network. The
// library still resolves the JSON Schema metaschemas, which it embeds.
type refuseLoader struct{}

func (refuseLoader) Load(string) (any, error) {
	return nil, errors.New("external references are not allowed")
}

// unsupportedRegexp stands in for a pattern RE2 can't compile, so compiling
// goes on and reports every other problem. It matches nothing.
type unsupportedRegexp string

func (r unsupportedRegexp) String() string          { return string(r) }
func (r unsupportedRegexp) MatchString(string) bool { return false }

// Events returns the events/list entries in name order.
func (c *Catalog) Events() []*Event {
	if c == nil {
		return nil
	}
	return slices.Clone(c.events)
}

// Event returns the precomputed events/list entry for name.
func (c *Catalog) Event(name string) (*Event, bool) {
	if c == nil {
		return nil, false
	}
	e, ok := c.byName[name]
	if !ok {
		return nil, false
	}
	return e.event, true
}

// ValidateArguments validates subscription arguments against the event's
// inferred inputSchema. It returns nil when they are valid. Empty args mean
// no arguments.
func (c *Catalog) ValidateArguments(name string, args []byte) []string {
	var e *entry
	if c != nil {
		e = c.byName[name]
	}
	if e == nil {
		return []string{"arguments: event is not defined"}
	}
	if len(args) > maxArgumentsBytes {
		return []string{"arguments: exceed the size limit"}
	}
	if len(bytes.TrimSpace(args)) == 0 {
		args = []byte("{}")
	}
	checks := argumentChecks
	checks.accepted = e.enums.errors
	return validateJSON(e.input, args, "arguments", e.inputNames, checks)
}

// valueChecks are checked on a parsed value before it is validated. A value
// failing one gets a single error and is never validated.
type valueChecks struct {
	// reject returns the message for a value too costly to validate, or "".
	reject func(any) string
	// accepted, when set, returns the errors of a value the schema accepts,
	// rendered from root, or nil.
	accepted func(v any, root string) []string
}

// validateJSON parses raw and validates it against schema. It returns the
// rendered errors, or nil when raw is valid.
func validateJSON(schema *jsonschema.Schema, raw []byte, root string, names map[string]struct{}, checks valueChecks) []string {
	v, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		// Parse errors quote the offending input.
		return []string{root + ": must be valid JSON"}
	}
	if path, msg := findRejectedValue(v, checks.reject); msg != "" {
		return []string{instancePath(root, path, v, names) + ": " + msg}
	}
	err = schema.Validate(v)
	if err == nil {
		if checks.accepted != nil {
			return checks.accepted(v, root)
		}
		return nil
	}
	var verr *jsonschema.ValidationError
	if !errors.As(err, &verr) {
		return []string{root + ": does not satisfy the schema"}
	}
	return renderValidationErrors(verr, root, v, names)
}

// findRejectedValue returns the location of a value in v, a tree from
// jsonschema.UnmarshalJSON, that reject refuses, and its message, or "" when
// there is none. Values are checked before their children.
func findRejectedValue(v any, reject func(any) string) ([]string, string) {
	// Map order is random, but sorting every object's keys would cost every
	// valid value. Only a hit is looked up again in key order, so the same
	// input always reports the same location.
	if _, msg := rejectedValueRev(v, reject, false); msg == "" {
		return nil, ""
	}
	rev, msg := rejectedValueRev(v, reject, true)
	slices.Reverse(rev)
	return rev, msg
}

// rejectedValueRev returns the location of a rejected value with its tokens
// in reverse, so only the path to a hit allocates. sorted visits object keys
// in order.
func rejectedValueRev(v any, reject func(any) string, sorted bool) ([]string, string) {
	if msg := reject(v); msg != "" {
		return nil, msg
	}
	switch n := v.(type) {
	case map[string]any:
		if sorted {
			for _, key := range slices.Sorted(maps.Keys(n)) {
				if rev, msg := rejectedValueRev(n[key], reject, sorted); msg != "" {
					return append(rev, key), msg
				}
			}
			return nil, ""
		}
		for key, child := range n {
			if rev, msg := rejectedValueRev(child, reject, sorted); msg != "" {
				return append(rev, key), msg
			}
		}
	case []any:
		for i, child := range n {
			if rev, msg := rejectedValueRev(child, reject, sorted); msg != "" {
				return append(rev, strconv.Itoa(i)), msg
			}
		}
	}
	return nil, ""
}

// rejectLargeNumber rejects numbers too large to validate.
func rejectLargeNumber(v any) string {
	if n, ok := v.(json.Number); ok && numberTooLarge(n) {
		return largeNumberMessage
	}
	return ""
}

// rejectArgumentValue also rejects lists and strings longer than any
// inferred argument accepts. The validator checks every item of a list
// against an enum even past maxItems, comparing numbers as exact rationals,
// so a long list could otherwise take seconds to reject.
func rejectArgumentValue(v any) string {
	switch x := v.(type) {
	case []any:
		if len(x) > maxArgumentListItems {
			return longArgumentListMessage
		}
	case string:
		// maxLength counts characters, not bytes.
		if len(x) > maxArgumentStringLength && utf8.RuneCountInString(x) > maxArgumentStringLength {
			return longArgumentStringMessage
		}
	}
	return rejectLargeNumber(v)
}

// argumentEnums maps each argument with an enum to the enumKey of its
// values. Checking values against these sets costs the same whatever the
// enum's size.
type argumentEnums map[string]map[string]struct{}

func newArgumentEnums(args []Argument) argumentEnums {
	var enums argumentEnums
	for _, arg := range args {
		if arg.Enum == nil {
			continue
		}
		set := make(map[string]struct{}, len(arg.Enum))
		for _, raw := range arg.Enum {
			v, err := decodeJSON(raw)
			if err != nil {
				continue
			}
			// Values without a key, such as objects, can't equal a
			// scalar argument value.
			if key := enumKey(v); key != "" {
				set[key] = struct{}{}
			}
		}
		if enums == nil {
			enums = argumentEnums{}
		}
		enums[arg.Name] = set
	}
	return enums
}

// errors checks the arguments in v, which the inputSchema accepts, against
// their enums: a value, or each item of a list. Range operator operands
// compare rather than match, and have none. Errors are rendered from root
// like validation errors.
func (a argumentEnums) errors(v any, root string) []string {
	args, _ := v.(map[string]any)
	var out []string
	more := 0
	check := func(set map[string]struct{}, path string, value any) {
		if _, ok := set[enumKey(value)]; ok {
			return
		}
		if len(out) == maxReportedErrors {
			more++
			return
		}
		out = append(out, path+": "+enumMessage)
	}
	for _, name := range slices.Sorted(maps.Keys(args)) {
		set, ok := a[name]
		if !ok {
			continue
		}
		path := appendPathKey(root, name)
		switch x := args[name].(type) {
		case []any:
			for i, item := range x {
				check(set, indexPath(path, i), item)
			}
		case map[string]any:
			// Range operators.
		default:
			check(set, path, x)
		}
	}
	slices.Sort(out)
	if more > 0 {
		out = append(out, moreMessage(more))
	}
	return out
}

// enumKey returns a form of a scalar decoded JSON value that equals
// another's exactly when JSON Schema considers the values equal, so 1, 1.0
// and 10e-1 share one. It is "" for other values, and for numbers too large
// for any accepted argument value to equal.
func enumKey(v any) string {
	switch x := v.(type) {
	case string:
		return "s" + x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case json.Number:
		if n, ok := canonicalNumber(string(x)); ok {
			return "n" + n
		}
	}
	return ""
}

func numberTooLarge(n json.Number) bool {
	s := string(n)
	if len(s) > maxNumberScale {
		return true
	}
	i := strings.IndexAny(s, "eE")
	if i < 0 {
		return false
	}
	exp, err := strconv.Atoi(s[i+1:])
	return err != nil || exp > maxNumberScale || exp < -maxNumberScale
}
