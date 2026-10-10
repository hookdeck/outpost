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
	"sync"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

const (
	// maxPayloadSchemaBytes caps a payload schema, measured compact.
	maxPayloadSchemaBytes = 256 << 10
	// maxMCPPayloadSchemaBytes is lower because events/list returns every
	// MCP-enabled payload schema to MCP clients.
	maxMCPPayloadSchemaBytes = 64 << 10
	// defaultMaxValidationBytes is the WithMaxValidationBytes default.
	// Validating takes up to a few hundred times the data size in memory,
	// more with anyOf and oneOf (see ValidationFactor).
	defaultMaxValidationBytes = 256 << 10
	// maxValidationFactor caps ValidationFactor.
	maxValidationFactor = 32
	// maxArgumentsBytes caps subscription arguments before they are parsed.
	// Subscriptions filter on far less, and it keeps parsing and the checks
	// run before validation cheap.
	maxArgumentsBytes = 16 << 10
	// maxNumberScale bounds the length and the exponent of every number in a
	// validated value. The validator turns numbers into exact rationals, and
	// 1e999999 alone takes milliseconds, so a value with a larger number
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

// dataChecks and argumentChecks are what validateJSON checks before
// validating event data and subscription arguments against their schema.
var (
	dataChecks     = valueChecks{duplicateKeys: true, reject: rejectLargeNumber}
	argumentChecks = valueChecks{reject: rejectArgumentValue}
)

// Option configures NewCatalog.
type Option func(*catalogOptions)

type catalogOptions struct {
	imported           Definitions
	maxValidationBytes int
	warnings           []string
}

// WithImported adds definitions from an import source such as an OpenAPI
// document. Entries for topics missing from TOPICS are skipped with a warning
// instead of failing, and explicit definitions win per topic.
func WithImported(defs Definitions) Option {
	return func(o *catalogOptions) { o.imported = defs }
}

// WithMaxValidationBytes caps the size of event data validated at publish.
// Larger data fails validation in enforce mode and skips it in warn mode.
// The default is 256 KiB.
func WithMaxValidationBytes(n int) Option {
	return func(o *catalogOptions) { o.maxValidationBytes = n }
}

// Catalog is the immutable, compiled set of topics and their schemas. It is
// safe for concurrent use: nothing in it changes after NewCatalog returns,
// and validating against a compiled schema doesn't modify it. Returned
// schemas, arguments and events share the catalog's memory and must not be
// modified.
type Catalog struct {
	topics []Topic
	byName map[string]int
	// entries holds the compiled state of each topic, by topics index.
	entries            []entry
	warnings           []string
	hasSchemas         bool
	mcpTopics          []string
	mcpEvents          []*MCPEvent
	maxValidationBytes int
}

type entry struct {
	defined bool
	// validator is the compiled payload schema, set only for topics with
	// validation warn or enforce.
	validator *jsonschema.Schema
	// factor is ValidationFactor, set with validator.
	factor int
	// dataNames holds the property names the payload schema declares, which
	// error paths may show.
	dataNames map[string]struct{}
	// The rest is set only for MCP-enabled topics. input is the inputSchema
	// without enums, which enums holds instead.
	args       []Argument
	event      *MCPEvent
	input      *jsonschema.Schema
	inputNames map[string]struct{}
	enums      argumentEnums
}

// NewCatalog validates defs against topics, the TOPICS list, and compiles
// their schemas. Every problem found is reported at once in a *ConfigError.
func NewCatalog(topics []string, defs Definitions, opts ...Option) (*Catalog, error) {
	var o catalogOptions
	for _, opt := range opts {
		opt(&o)
	}
	c := EmptyCatalog(topics)
	if o.maxValidationBytes > 0 {
		c.maxValidationBytes = o.maxValidationBytes
	}
	c.warnings = slices.Sorted(slices.Values(o.warnings))
	if len(defs) == 0 && len(o.imported) == 0 {
		return c, nil
	}
	if len(c.topics) == 0 {
		// An empty TOPICS allows any topic, so there is nothing to check
		// the definitions against.
		return nil, &ConfigError{Problems: []string{"topic schemas require TOPICS to list the topics"}}
	}

	b := catalogBuilder{c: c}
	merged := make(Definitions, len(defs)+len(o.imported))
	for name, def := range o.imported {
		if _, ok := c.byName[name]; !ok {
			b.c.warnings = append(b.c.warnings, fmt.Sprintf("imported topic %q is not in TOPICS and was skipped%s", name, c.didYouMean(name)))
			continue
		}
		merged[name] = def
	}
	maps.Copy(merged, defs)
	for _, name := range slices.Sorted(maps.Keys(merged)) {
		b.addDefinition(name, merged[name])
	}
	b.checkReplacements()
	if len(b.problems) > 0 {
		slices.Sort(b.problems)
		return nil, &ConfigError{Problems: slices.Compact(b.problems)}
	}

	for i, t := range c.topics {
		e := &c.entries[i]
		c.hasSchemas = c.hasSchemas || e.defined
		if e.event != nil {
			c.mcpTopics = append(c.mcpTopics, t.Name)
			c.mcpEvents = append(c.mcpEvents, e.event)
		}
	}
	slices.Sort(c.warnings)
	return c, nil
}

// EmptyCatalog returns a catalog of schema-less topics.
func EmptyCatalog(topics []string) *Catalog {
	c := &Catalog{byName: make(map[string]int, len(topics)), maxValidationBytes: defaultMaxValidationBytes}
	for _, name := range topics {
		if _, dup := c.byName[name]; dup {
			continue
		}
		c.byName[name] = len(c.topics)
		c.topics = append(c.topics, Topic{Name: name, Validation: ValidationOff})
	}
	c.entries = make([]entry, len(c.topics))
	return c
}

// didYouMean suggests the TOPICS entry name matches once whitespace is
// trimmed. TOPICS entries are used verbatim, so " a" and "a" differ.
func (c *Catalog) didYouMean(name string) string {
	trimmed := strings.TrimSpace(name)
	for _, t := range c.topics {
		if t.Name != name && strings.TrimSpace(t.Name) == trimmed {
			return fmt.Sprintf(" (did you mean %q?)", t.Name)
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
	b.problems = append(b.problems, fmt.Sprintf("topic %q: ", topic)+fmt.Sprintf(format, args...))
}

func (b *catalogBuilder) addDefinition(name string, def Definition) {
	if strings.Contains(name, "*") {
		b.problem(name, "wildcard topics can't have schemas")
		return
	}
	i, ok := b.c.byName[name]
	if !ok {
		b.problems = append(b.problems, fmt.Sprintf("topic %q is not in TOPICS%s", name, b.c.didYouMean(name)))
		return
	}
	t := Topic{
		Name:        name,
		Description: def.Description,
		Validation:  ValidationOff,
		MCP:         def.MCP,
		Deprecated:  def.Deprecated,
		ReplacedBy:  def.ReplacedBy,
	}
	e := entry{defined: true}
	if def.Name != "" && def.Name != name {
		b.problem(name, "name %q must match the topic key", def.Name)
	}
	switch def.Validation {
	case "", ValidationOff:
	case ValidationWarn, ValidationEnforce:
		t.Validation = def.Validation
	default:
		b.problem(name, `validation %q must be "off", "warn" or "enforce"`, def.Validation)
	}
	if def.ReplacedBy != "" && !def.Deprecated {
		b.problem(name, "replaced_by requires deprecated: true")
	}

	raw := bytes.TrimSpace(def.PayloadSchema)
	if len(raw) == 0 || string(raw) == "null" {
		if t.Validation != ValidationOff {
			b.problem(name, "validation %q requires payload_schema", t.Validation)
		}
		if t.MCP.Enabled {
			b.problem(name, "mcp.enabled requires payload_schema")
		}
	} else {
		b.addSchema(&t, &e, raw)
	}
	b.c.topics[i] = t
	b.c.entries[i] = e
}

// addSchema checks and compiles a topic's payload schema, and precomputes
// what validation and MCP need.
func (b *catalogBuilder) addSchema(t *Topic, e *entry, raw json.RawMessage) {
	name := t.Name
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		b.problem(name, "payload_schema is not valid JSON: %v", err)
		return
	}
	// Clone to drop the buffer's spare capacity: the catalog keeps this.
	compact := bytes.Clone(buf.Bytes())
	if compact[0] != '{' {
		b.problem(name, "payload_schema must be a JSON object")
		return
	}
	limit, scope := maxPayloadSchemaBytes, ""
	if t.MCP.Enabled {
		limit, scope = maxMCPPayloadSchemaBytes, " for MCP-enabled topics"
	}
	if len(compact) > limit {
		b.problem(name, "payload_schema is %d bytes; the limit%s is %d", len(compact), scope, limit)
		return
	}
	if path, key, ok := duplicateKey(compact, "payload_schema", nil); ok {
		b.problem(name, "%s has a duplicate key %s", path, quoteJSONString(key))
		return
	}
	tree, err := decodeJSON(compact)
	if err != nil {
		b.problem(name, "payload_schema is not valid JSON: %v", err)
		return
	}
	root := tree.(map[string]any)
	if !b.checkSchemaObjects(name, root) {
		return
	}
	t.PayloadSchema = compact
	isObject := root["type"] == "object"
	if t.MCP.Enabled && !isObject {
		b.problem(name, `mcp.enabled requires a payload_schema with "type": "object"`)
	}

	schema, unsupported, err := compileSchema(tree)
	if err != nil {
		for _, p := range compileProblems(err, tree) {
			b.problem(name, "%s", p)
		}
	}
	// Go's RE2 engine rejects some ECMAScript patterns, such as lookaheads.
	// A topic that validates or is shown to MCP clients can't use them; one
	// that does neither keeps its schema for documentation.
	for _, msg := range unsupported {
		if t.Validation != ValidationOff || t.MCP.Enabled {
			b.problem(name, "payload_schema %s", msg)
		} else {
			b.c.warnings = append(b.c.warnings, fmt.Sprintf("topic %q: payload_schema %s; validation is off, so the schema is kept but can't be used to validate", name, msg))
		}
	}
	if err != nil || len(unsupported) > 0 {
		return
	}
	if t.Validation != ValidationOff {
		e.validator = schema
		e.factor = validationFactor(root)
		e.dataNames = propertyNameSet(root)
	}
	if !t.MCP.Enabled || !isObject {
		return
	}

	ev, args, err := buildMCPEvent(*t, compact, root)
	if err != nil {
		b.problem(name, "can't infer the MCP inputSchema: %v", err)
		return
	}
	// The validator compares a value with every enum value, numbers as
	// exact rationals, so it gets the inputSchema without enums, and
	// ValidateArguments checks them against sets.
	_, checked, err := inferArguments(compact, root, false)
	var inputTree any
	if err == nil {
		inputTree, err = decodeJSON(checked)
	}
	if err == nil {
		var bad []string
		e.input, bad, err = compileSchema(inputTree)
		if err == nil && len(bad) > 0 {
			err = errors.New(bad[0])
		}
	}
	if err != nil {
		b.problem(name, "the inferred MCP inputSchema doesn't compile: %s", sanitizeCompileError(err))
		return
	}
	e.event, e.args, e.inputNames, e.enums = ev, args, propertyNameSet(inputTree), newArgumentEnums(args)
}

// checkSchemaObjects checks what the metaschema doesn't: $schema is the
// 2020-12 dialect, references are local JSON pointers, $id and anchors are
// only declared at the root, and x-mcp-filter is a boolean.
//
// Inference and the breaking-change diff resolve references with
// resolveLocalRef, which follows JSON pointers from the root. The validator
// also resolves anchors, and resolves the references below a nested $id
// against it, so a schema using either would be validated against one
// thing and inferred and diffed against another.
func (b *catalogBuilder) checkSchemaObjects(name string, root map[string]any) bool {
	const rootPath = "payload_schema"
	before := len(b.problems)
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
					appendPathKey(path, kw), quoteJSONString(ref))
			case !pointerRef(ref):
				b.problem(name, `%s %s must be a JSON pointer such as "#/$defs/name"; anchors are not supported`,
					appendPathKey(path, kw), quoteJSONString(ref))
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

// checkReplacements checks replaced_by across topics, once every topic's
// MCP setting is known.
func (b *catalogBuilder) checkReplacements() {
	for i, t := range b.c.topics {
		if !b.c.entries[i].defined || t.ReplacedBy == "" {
			continue
		}
		j, ok := b.c.byName[t.ReplacedBy]
		switch {
		case t.ReplacedBy == t.Name:
			b.problem(t.Name, "replaced_by can't name the topic itself")
		case !ok:
			b.problem(t.Name, "replaced_by %q is not in TOPICS%s", t.ReplacedBy, b.c.didYouMean(t.ReplacedBy))
		case t.Deprecated && t.MCP.Enabled && !b.c.topics[j].MCP.Enabled:
			b.problem(t.Name, "replaced_by %q must be MCP-enabled because this topic is", t.ReplacedBy)
		}
	}
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
func compileSchema(doc any) (*jsonschema.Schema, []string, error) {
	var unsupported []string
	engine := func(pattern string) (jsonschema.Regexp, error) {
		re, err := regexp.Compile(pattern)
		if err != nil {
			msg := fmt.Sprintf("pattern %s is not supported by Go's RE2 syntax: %v", quoteJSONString(pattern), err)
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
	c.RegisterVocabulary(formatVocabulary())
	c.AssertVocabs()
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

// assertedFormats are the formats validation checks.
var assertedFormats = map[string]bool{"date": true, "date-time": true}

// formatVocabulary asserts the date and date-time formats, which ranged
// arguments rely on. JSON Schema 2020-12 makes format an annotation, and
// every other format stays one: OpenAPI documents use formats such as uri or
// email loosely, so asserting them would reject events publishers consider
// valid. The library can only assert all formats or none, hence a
// vocabulary instead of Compiler.AssertFormat.
var formatVocabulary = sync.OnceValue(func() *jsonschema.Vocabulary {
	formats := make(map[string]*jsonschema.Format, len(assertedFormats))
	for name := range assertedFormats {
		formats[name] = builtinFormat(name)
	}
	return &jsonschema.Vocabulary{
		URL: "urn:outpost:vocab:format-assertion",
		// Once a vocabulary is registered, the library checks schemas only
		// against the metaschemas of the active vocabularies, leaving out
		// meta-data, format-annotation and content. Adding them here keeps
		// the full 2020-12 metaschema check.
		Schema: annotationMetaschema(),
		Compile: func(_ *jsonschema.CompilerContext, obj map[string]any) (jsonschema.SchemaExt, error) {
			name, _ := obj["format"].(string)
			if f := formats[name]; f != nil {
				return formatAssertion{f}, nil
			}
			return nil, nil
		},
	}
})

// builtinFormat returns the library's validator for a format, which it
// exposes only on compiled schemas.
func builtinFormat(name string) *jsonschema.Format {
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	c.UseLoader(refuseLoader{})
	if err := c.AddResource(schemaURL, map[string]any{"format": name}); err != nil {
		panic(err)
	}
	f := c.MustCompile(schemaURL).Format
	if f == nil {
		panic("jsonschema: no built-in format " + name)
	}
	return f
}

// annotationMetaschema compiles the 2020-12 vocabulary metaschemas that a
// registered vocabulary drops from the metaschema check.
func annotationMetaschema() *jsonschema.Schema {
	const base = "https://json-schema.org/draft/2020-12/meta/"
	c := jsonschema.NewCompiler()
	c.UseLoader(refuseLoader{})
	doc := map[string]any{"allOf": []any{
		map[string]any{"$ref": base + "meta-data"},
		map[string]any{"$ref": base + "format-annotation"},
		map[string]any{"$ref": base + "content"},
	}}
	if err := c.AddResource("urn:outpost:metaschema", doc); err != nil {
		panic(err)
	}
	return c.MustCompile("urn:outpost:metaschema")
}

// formatAssertion is the compiled form of the format vocabulary.
type formatAssertion struct{ format *jsonschema.Format }

func (f formatAssertion) Validate(ctx *jsonschema.ValidatorContext, v any) {
	if err := f.format.Validate(v); err != nil {
		ctx.AddError(&kind.Format{Got: v, Want: f.format.Name, Err: err})
	}
}

// duplicateKey finds the first object in raw, valid JSON, that repeats a
// key, and returns its dot path from root and the key. As in instancePath,
// keys on the path show only when names is nil or holds them, else as "*".
func duplicateKey(raw []byte, root string, names map[string]struct{}) (string, string, bool) {
	type frame struct {
		path string
		// keys is nil for arrays.
		keys      map[string]struct{}
		expectKey bool
		key       string
		index     int
	}
	var stack []*frame
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", "", false
		}
		var top *frame
		if len(stack) > 0 {
			top = stack[len(stack)-1]
		}
		if key, ok := tok.(string); ok && top != nil && top.keys != nil && top.expectKey {
			if _, dup := top.keys[key]; dup {
				return top.path, key, true
			}
			top.keys[key] = struct{}{}
			top.key, top.expectKey = key, false
			continue
		}
		switch tok {
		case json.Delim('{'), json.Delim('['):
			path := root
			if top != nil && top.keys != nil {
				if _, named := names[top.key]; names == nil || named {
					path = appendPathKey(top.path, top.key)
				} else {
					path = top.path + ".*"
				}
			} else if top != nil {
				path = indexPath(top.path, top.index)
			}
			f := &frame{path: path}
			if tok == json.Delim('{') {
				f.keys, f.expectKey = map[string]struct{}{}, true
			}
			stack = append(stack, f)
			continue
		case json.Delim('}'), json.Delim(']'):
			stack = stack[:len(stack)-1]
			if len(stack) == 0 {
				return "", "", false
			}
			top = stack[len(stack)-1]
		}
		// A value ended.
		if top != nil {
			if top.keys != nil {
				top.expectKey = true
			} else {
				top.index++
			}
		}
	}
}

// Topics returns one entry per TOPICS entry, in TOPICS order. It is never
// nil, so it encodes as a JSON array.
func (c *Catalog) Topics() []Topic {
	if c == nil || len(c.topics) == 0 {
		return []Topic{}
	}
	return slices.Clone(c.topics)
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

// HasSchemas reports whether any topic has a definition.
func (c *Catalog) HasSchemas() bool { return c != nil && c.hasSchemas }

// MCPEnabled reports whether any topic is exposed to MCP.
func (c *Catalog) MCPEnabled() bool { return c != nil && len(c.mcpTopics) > 0 }

// MCPTopics returns the MCP-enabled topic names in TOPICS order.
func (c *Catalog) MCPTopics() []string {
	if c == nil {
		return nil
	}
	return slices.Clone(c.mcpTopics)
}

// Warnings returns non-fatal configuration problems found while building.
func (c *Catalog) Warnings() []string {
	if c == nil {
		return nil
	}
	return slices.Clone(c.warnings)
}

// ValidateData validates event data against the topic's payload schema. The
// mode is off, and nothing is checked, for "", "*", unknown topics and
// topics without a schema or with validation off.
func (c *Catalog) ValidateData(topic string, data []byte) ValidationResult {
	if c == nil || topic == "" || topic == "*" {
		return ValidationResult{Mode: ValidationOff}
	}
	i, ok := c.byName[topic]
	if !ok || c.entries[i].validator == nil {
		return ValidationResult{Mode: ValidationOff}
	}
	e, mode := &c.entries[i], c.topics[i].Validation
	if len(data) > c.maxValidationBytes {
		if mode == ValidationEnforce {
			return ValidationResult{Mode: mode, Checked: true, Errors: []string{DataTooLargeError}}
		}
		return ValidationResult{Mode: mode, SkippedTooLarge: true}
	}
	errs := validateJSON(e.validator, data, "data", e.dataNames, dataChecks)
	return ValidationResult{Mode: mode, Checked: true, Valid: len(errs) == 0, Errors: errs}
}

// ValidateArguments validates subscription arguments against the topic's
// inferred inputSchema. It returns nil when they are valid. Empty args mean
// no arguments. Topics that aren't MCP-enabled accept none.
func (c *Catalog) ValidateArguments(topic string, args []byte) []string {
	if c == nil {
		return []string{"arguments: topic is not MCP-enabled"}
	}
	i, ok := c.byName[topic]
	if !ok || c.entries[i].input == nil {
		return []string{"arguments: topic is not MCP-enabled"}
	}
	if len(args) > maxArgumentsBytes {
		return []string{"arguments: exceed the size limit"}
	}
	if len(bytes.TrimSpace(args)) == 0 {
		args = []byte("{}")
	}
	e := &c.entries[i]
	checks := argumentChecks
	checks.accepted = e.enums.errors
	return validateJSON(e.input, args, "arguments", e.inputNames, checks)
}

// valueChecks are checked on a parsed value before it is validated. A value
// failing one gets a single error and is never validated.
type valueChecks struct {
	// duplicateKeys rejects objects that repeat a key. The validator sees
	// the last of the repeated values, but the bytes are kept as published,
	// and some consumers read the first one.
	duplicateKeys bool
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
	// Counting finds duplicates cheaply; only data that has some is scanned
	// again to locate them.
	if checks.duplicateKeys && objectMemberCount(raw) != treeMemberCount(v) {
		path, _, ok := duplicateKey(raw, root, names)
		if !ok {
			path = root
		}
		return []string{path + ": has duplicate keys"}
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

// objectMemberCount counts the members of every object in raw, valid JSON:
// the colons outside strings.
func objectMemberCount(raw []byte) int {
	n, inString := 0, false
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '\\':
			// Only valid inside strings; skip the escaped byte.
			i++
		case '"':
			inString = !inString
		case ':':
			if !inString {
				n++
			}
		}
	}
	return n
}

// treeMemberCount counts the members of every object in v, a tree from
// jsonschema.UnmarshalJSON. It is less than objectMemberCount of the parsed
// JSON exactly when an object repeats a key, since only the last is kept.
func treeMemberCount(v any) int {
	n := 0
	switch x := v.(type) {
	case map[string]any:
		n = len(x)
		for _, child := range x {
			n += treeMemberCount(child)
		}
	case []any:
		for _, child := range x {
			n += treeMemberCount(child)
		}
	}
	return n
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
		out = append(out, fmt.Sprintf("... and %d more", more))
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

// Arguments returns the filterable properties of an MCP-enabled topic, in
// payload schema order.
func (c *Catalog) Arguments(topic string) []Argument {
	if c == nil {
		return nil
	}
	i, ok := c.byName[topic]
	if !ok {
		return nil
	}
	return slices.Clone(c.entries[i].args)
}

// MCPEvents returns the precomputed events/list entries in TOPICS order.
func (c *Catalog) MCPEvents() []*MCPEvent {
	if c == nil {
		return nil
	}
	return slices.Clone(c.mcpEvents)
}

// MCPEvent returns the precomputed events/list entry for name.
func (c *Catalog) MCPEvent(name string) (*MCPEvent, bool) {
	if c == nil {
		return nil, false
	}
	i, ok := c.byName[name]
	if !ok || c.entries[i].event == nil {
		return nil, false
	}
	return c.entries[i].event, true
}

// Snapshot returns the evolution contract for the catalog: the MCP setting
// and payload schema of every topic with a definition.
func (c *Catalog) Snapshot() Snapshot {
	s := Snapshot{Topics: map[string]SnapshotTopic{}}
	if c == nil {
		return s
	}
	for i, t := range c.topics {
		if c.entries[i].defined {
			s.Topics[t.Name] = SnapshotTopic{MCPEnabled: t.MCP.Enabled, PayloadSchema: t.PayloadSchema}
		}
	}
	return s
}

// WithWarnings adds warnings found before NewCatalog, such as those
// ParseOpenAPI returns, to the catalog's Warnings.
func WithWarnings(warnings []string) Option {
	return func(o *catalogOptions) { o.warnings = append(o.warnings, warnings...) }
}

// MaxValidationBytes returns the size of the largest event data ValidateData
// validates, set with WithMaxValidationBytes. It is 0 for a nil catalog,
// which validates nothing.
func (c *Catalog) MaxValidationBytes() int {
	if c == nil {
		return 0
	}
	return c.maxValidationBytes
}

// ValidationFactor estimates the memory validating the topic's data takes,
// relative to a schema without anyOf or oneOf: 1 plus the branch count of the
// largest anyOf or oneOf in the payload schema, at most 32. The validator
// keeps the errors of every failing branch, so memory grows with it. It is 0
// when ValidateData never parses the topic's data: for "", "*", unknown
// topics, and topics without a schema or with validation off.
func (c *Catalog) ValidationFactor(topic string) int {
	if c == nil {
		return 0
	}
	i, ok := c.byName[topic]
	if !ok {
		return 0
	}
	return c.entries[i].factor
}

// validationFactor computes ValidationFactor for a payload schema, a tree
// from decodeJSON. Every schema object counts, reached through a $ref or not.
func validationFactor(root any) int {
	branches := 0
	visitSchemaObjects(root, "", func(obj map[string]any, _ string) {
		for _, kw := range []string{"anyOf", "oneOf"} {
			if list, ok := obj[kw].([]any); ok {
				branches = max(branches, len(list))
			}
		}
	})
	return min(1+branches, maxValidationFactor)
}
