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

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const (
	// maxPayloadSchemaBytes caps a payload schema, measured compact, because
	// events/list returns every payload schema to MCP clients.
	maxPayloadSchemaBytes = 64 << 10
	// maxInputSchemaBytes caps an input schema, measured compact.
	maxInputSchemaBytes = 64 << 10
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

var largeNumberMessage = fmt.Sprintf("must be a number of at most %d characters with an exponent between -%d and %d",
	maxNumberScale, maxNumberScale, maxNumberScale)

// Catalog is the immutable, compiled set of event definitions. It is safe
// for concurrent use: nothing in it changes after NewCatalog returns, and
// validating against a compiled schema doesn't modify it. Returned schemas
// and events share the catalog's memory and must not be modified.
type Catalog struct {
	// topics is the TOPICS list, empty when any topic is allowed.
	topics []string
	byName map[string]*entry
	// events are the events/list entries in configured order.
	events []*Event
}

// entry is the compiled state of one event.
type entry struct {
	event *Event
	args  []Argument
	input *jsonschema.Schema
	// inputNames are the property names declared anywhere in the
	// inputSchema, and argNames the arguments it accepts.
	inputNames map[string]struct{}
	argNames   map[string]struct{}
}

// NewCatalog validates defs and compiles their schemas. When topics, the
// TOPICS list, is not empty, every event must be in it. The problems found
// are reported at once in a *ConfigError, up to 100 of them.
func NewCatalog(topics []string, defs Definitions) (*Catalog, error) {
	c := &Catalog{topics: topics, byName: make(map[string]*entry, len(defs))}
	b := catalogBuilder{c: c}
	first := make(map[string]int, len(defs))
	for i, def := range defs {
		if j, dup := first[def.Name]; dup {
			b.problems = append(b.problems, fmt.Sprintf("events[%d]: duplicate name %q, already defined at events[%d]", i, clip(def.Name), j))
			continue
		}
		first[def.Name] = i
		b.addDefinition(def)
	}
	if len(b.problems) > 0 {
		return nil, &ConfigError{Problems: limitMessages(b.problems)}
	}
	for _, def := range defs {
		c.events = append(c.events, c.byName[def.Name].event)
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

func (b *catalogBuilder) problem(name, format string, args ...any) {
	b.problems = append(b.problems, fmt.Sprintf("event %q: ", clip(name))+fmt.Sprintf(format, args...))
}

func (b *catalogBuilder) addDefinition(def Definition) {
	name := def.Name
	if strings.Contains(name, "*") {
		b.problem(name, "wildcard topics can't have event definitions")
		return
	}
	if len(b.c.topics) > 0 && !slices.Contains(b.c.topics, name) {
		b.problems = append(b.problems, fmt.Sprintf("event %q is not in TOPICS%s", clip(name), b.c.didYouMean(name)))
		return
	}
	input, payload := bytes.TrimSpace(def.InputSchema), bytes.TrimSpace(def.PayloadSchema)
	ok := true
	if len(input) == 0 || string(input) == "null" {
		b.problem(name, `inputSchema is required; use {"type": "object", "properties": {}} for an event without arguments`)
		ok = false
	}
	if len(payload) == 0 || string(payload) == "null" {
		b.problem(name, "payloadSchema is required")
		ok = false
	}
	if !ok {
		return
	}
	if e := b.compile(def, input, payload); e != nil {
		b.c.byName[name] = e
	}
}

// compactObject compacts raw, a schema named field, and reports a problem
// when it isn't a JSON object within limit bytes.
func (b *catalogBuilder) compactObject(name, field string, raw json.RawMessage, limit int) (json.RawMessage, map[string]any) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		b.problem(name, "%s is not valid JSON: %v", field, err)
		return nil, nil
	}
	// Clone to drop the buffer's spare capacity: the catalog keeps this.
	compact := bytes.Clone(buf.Bytes())
	if compact[0] != '{' {
		b.problem(name, "%s must be a JSON object", field)
		return nil, nil
	}
	if len(compact) > limit {
		b.problem(name, "%s is %d bytes; the limit is %d", field, len(compact), limit)
		return nil, nil
	}
	tree, err := decodeJSON(compact)
	if err != nil {
		b.problem(name, "%s is not valid JSON: %v", field, err)
		return nil, nil
	}
	return compact, tree.(map[string]any)
}

// compile checks and compiles an event's schemas, and precomputes what MCP
// needs. It returns nil after reporting a problem.
func (b *catalogBuilder) compile(def Definition, rawInput, rawPayload json.RawMessage) *entry {
	name := def.Name
	payload, root := b.compactObject(name, "payloadSchema", rawPayload, maxPayloadSchemaBytes)
	input, inRoot := b.compactObject(name, "inputSchema", rawInput, maxInputSchemaBytes)
	if root == nil || inRoot == nil {
		return nil
	}
	ok := b.checkSchemaObjects(name, "payloadSchema", root) && b.checkSchemaObjects(name, "inputSchema", inRoot)
	for _, f := range []struct {
		field string
		tree  map[string]any
	}{{"payloadSchema", root}, {"inputSchema", inRoot}} {
		if f.tree["type"] != "object" {
			b.problem(name, `%s must have "type": "object"`, f.field)
			ok = false
		}
	}
	if !ok {
		return nil
	}

	_, unsupported, perr := compileSchema(root, false)
	if perr != nil {
		for _, p := range compileProblems(perr, root, "payloadSchema") {
			b.problem(name, "%s", p)
		}
	}
	// Go's RE2 engine rejects some ECMAScript patterns, such as lookaheads.
	for _, msg := range unsupported {
		b.problem(name, "payloadSchema %s", msg)
	}
	compiled, inUnsupported, ierr := compileSchema(inRoot, true)
	if ierr != nil {
		for _, p := range compileProblems(ierr, inRoot, "inputSchema") {
			b.problem(name, "%s", p)
		}
	}
	for _, msg := range inUnsupported {
		b.problem(name, "inputSchema %s", msg)
	}
	if perr != nil || ierr != nil || len(unsupported) > 0 || len(inUnsupported) > 0 {
		return nil
	}

	args, problems := checkInputSchema(inRoot, root)
	for _, p := range problems {
		b.problem(name, "%s", p)
	}
	if len(problems) > 0 {
		return nil
	}
	ev, err := buildEvent(def, input, payload)
	if err != nil {
		b.problem(name, "can't build the events/list entry: %v", err)
		return nil
	}
	e := &entry{event: ev, args: args, input: compiled, inputNames: propertyNameSet(inRoot), argNames: map[string]struct{}{}}
	for _, a := range args {
		e.argNames[a.Name] = struct{}{}
	}
	return e
}

// checkSchemaObjects checks what the metaschema doesn't: $schema is the
// 2020-12 dialect, references are local JSON pointers to schemas, $id and
// and anchors are only declared at the root.
//
// Inference resolves references with resolveLocalRef, which follows JSON
// pointers from the root. The validator also resolves anchors, and resolves
// the references below a nested $id against it, so a schema using either
// would be validated against one thing and inferred against another. It also
// compiles whatever a reference points to as a schema, so a reference into an
// extension or a default would bring in an $id, anchors and references never
// checked here.
func (b *catalogBuilder) checkSchemaObjects(name, rootPath string, root map[string]any) bool {
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
				b.problem(name, "%s is only allowed at the %s root, since it changes how references below it resolve",
					appendPathKey(path, "$id"), rootPath)
			}
			for _, kw := range []string{"$anchor", "$dynamicAnchor"} {
				if _, ok := obj[kw]; ok {
					b.problem(name, `%s is only allowed at the %s root; reference subschemas with JSON pointers such as "#/$defs/name"`,
						appendPathKey(path, kw), rootPath)
				}
			}
		}
	})
	return len(b.problems) == before
}

// compileProblems renders a compile error as problem lines. Metaschema
// violations are rendered like validation errors, with full paths, since a
// schema is configuration rather than event data.
func compileProblems(err error, tree any, rootName string) []string {
	var sve *jsonschema.SchemaValidationError
	var verr *jsonschema.ValidationError
	if !errors.As(err, &sve) || !errors.As(sve.Err, &verr) {
		return []string{rootName + ": " + sanitizeCompileError(err)}
	}
	// The library checks subschemas reached only through $ref separately;
	// URL then points at the subschema.
	root, instance := rootName, tree
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

// Events returns the events/list entries in configured order.
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
// inputSchema, and refuses any argument it doesn't list. It returns nil when
// they are valid. Empty args mean no arguments.
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
	checks := valueChecks{reject: rejectLargeNumber, accepted: e.unlisted}
	return validateJSON(e.input, args, "arguments", e.inputNames, checks)
}

// unlisted reports the keys of v, arguments the inputSchema accepts, that it
// doesn't list under properties. Keys are never echoed, so an unlisted one
// shows as "*".
func (e *entry) unlisted(v any, root string) []string {
	args, _ := v.(map[string]any)
	var out []string
	for _, key := range slices.Sorted(maps.Keys(args)) {
		if _, ok := e.argNames[key]; !ok {
			out = append(out, instancePath(root, []string{key}, v, e.inputNames)+": is not an argument of this event")
		}
	}
	return slices.Compact(out)
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
