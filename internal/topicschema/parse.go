package topicschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math/big"
	"mime"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Parse limits. Definitions come from operator files and environment values,
// and OpenAPI documents possibly from a URL someone else controls, so every
// stage is bounded: a small document must not exhaust memory or the stack.
const (
	// parseMaxInputBytes caps a definitions or OpenAPI document.
	parseMaxInputBytes = 4 << 20
	// parseMaxDepth caps JSON and YAML nesting, which also bounds recursion.
	parseMaxDepth = 128
	// yamlMaxNodes and yamlMaxOutputBytes bound alias expansion: a few
	// hundred bytes of nested anchors otherwise expand to billions of nodes.
	yamlMaxNodes       = 1_000_000
	yamlMaxOutputBytes = 16 << 20
	// openapiMaxBundledBytes caps the payload schemas bundled from one
	// OpenAPI document. Each webhook gets its own copy of the components it
	// references, so the output can be much larger than the document.
	openapiMaxBundledBytes = 64 << 20
	// openapiMaxOverlaidMembers caps the members copied by $ref overlays in
	// one OpenAPI document. Each copies the referenced object, so many
	// references to one large object add up.
	openapiMaxOverlaidMembers = 1 << 20
)

var utf8BOM = []byte("\xef\xbb\xbf")

// ParseDefinitionsJSON parses topic definitions from a JSON object keyed by
// topic name. Unknown fields in a topic object and duplicate keys at any depth
// are errors, so typos fail at startup instead of being silently ignored.
// payload_schema is kept as compact JSON with its key order and number
// literals as written. Errors are a *ConfigError naming the topic and the
// JSON pointer of each problem.
func ParseDefinitionsJSON(data []byte) (Definitions, error) {
	if len(data) > parseMaxInputBytes {
		return nil, parseTooLarge("the topic schemas document")
	}
	root, err := parseDecodeJSON(bytes.TrimPrefix(data, utf8BOM))
	if err != nil {
		return nil, parseDefinitionsError(err)
	}
	return parseDefinitions(root)
}

// ParseDefinitionsYAML parses topic definitions from a YAML mapping keyed by
// topic name. The document is converted with YAMLNodeToJSON, then read with
// the rules of ParseDefinitionsJSON. A stream of several documents is an
// error.
func ParseDefinitionsYAML(data []byte) (Definitions, error) {
	if len(data) > parseMaxInputBytes {
		return nil, parseTooLarge("the topic schemas document")
	}
	raw, err := parseYAMLDocument(data)
	if err != nil {
		return nil, parseDefinitionsError(err)
	}
	root, err := parseDecodeJSON(raw)
	if err != nil {
		return nil, parseDefinitionsError(err)
	}
	return parseDefinitions(root)
}

// YAMLNodeToJSON converts a YAML node to compact JSON, keeping mapping keys in
// document order.
//
// Scalars are typed with the YAML 1.2 core schema: plain null, ~ and empty
// values, true/false (in lower, title or upper case), decimal, 0o octal and
// 0x hexadecimal integers, and decimal floats become JSON values. Everything
// else stays a string, including timestamps, YAML 1.1 forms such as yes, on
// or 1_000, quoted scalars and scalars tagged !!str, !!timestamp or with a
// custom tag. .inf and .nan have no JSON form and are errors.
//
// Mapping keys must be scalars and are used as written, so 200: becomes
// "200". Null keys, merge keys (<<) and duplicate keys are errors.
//
// Aliases are expanded in place, within a budget of yamlMaxNodes nodes,
// yamlMaxOutputBytes of output and parseMaxDepth levels of nesting, and an
// alias to a node that contains it is an error.
func YAMLNodeToJSON(n *yaml.Node) (json.RawMessage, error) {
	if n == nil {
		return nil, errors.New("yaml: nil node")
	}
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return json.RawMessage("null"), nil
		}
		n = n.Content[0]
	}
	c := yamlConverter{}
	if err := c.value(n, 0); err != nil {
		return nil, err
	}
	if len(c.buf) > yamlMaxOutputBytes {
		return nil, c.errorf(n, "the document expands to more than %d MiB of JSON; check for nested aliases", yamlMaxOutputBytes>>20)
	}
	return json.RawMessage(c.buf), nil
}

// ParseOpenAPI imports topic definitions from the webhooks of an OpenAPI
// 3.1.x document, in JSON or YAML. Each webhook whose POST operation has a
// JSON request body schema becomes one topic:
//   - the name is x-outpost-topic on the operation, else on the path item,
//     else the webhook key;
//   - payload_schema is the schema of the application/json media type (with
//     or without parameters), else of the first *+json one, bundled into a
//     standalone schema (see openapiBundler);
//   - description is the operation summary, else its description;
//   - mcp.enabled is the operation's x-mcp-enabled, and deprecated its
//     deprecated flag. x-mcp-filter on properties is kept as is.
//
// $ref is resolved at path item, operation, request body and media type
// level (for example to #/components/pathItems or #/components/requestBodies)
// and must be local. Webhooks without a POST operation or a JSON request body
// schema are skipped, since Outpost delivers event data as a JSON POST; one
// that sets x-mcp-enabled: true is an error instead, since the opt-in can't
// be honoured.
//
// Without options every webhook is returned, and NewCatalog skips those
// missing from TOPICS. With OpenAPITopics, webhooks whose topic is not listed
// are skipped here instead: they are not bundled, and are reported in the
// returned warnings along with any problem found in them, which no longer
// fails the import. A webhook's topic is what its x-outpost-topic and key
// resolve to or, when a reference can't be resolved, the name known so far.
//
// A document without a webhooks section is an error, as it is most likely not
// the intended one. Errors are a *ConfigError naming the topic and the JSON
// pointer in the document of each problem.
func ParseOpenAPI(data []byte, opts ...OpenAPIOption) (Definitions, []string, error) {
	var o openapiOptions
	for _, opt := range opts {
		opt(&o)
	}
	if len(data) > parseMaxInputBytes {
		return nil, nil, parseTooLarge("the OpenAPI document")
	}
	data = bytes.TrimPrefix(data, utf8BOM)
	raw := data
	// JSON is valid YAML, but the JSON decoder is faster and its errors
	// point at byte offsets. YAML flow mappings starting with "{" must be
	// valid JSON.
	if trimmed := bytes.TrimLeft(data, " \t\r\n"); len(trimmed) == 0 || trimmed[0] != '{' {
		var err error
		if raw, err = parseYAMLDocument(data); err != nil {
			return nil, nil, parseConfigError(err.Error())
		}
	}
	root, err := parseDecodeJSON(raw)
	if err != nil {
		return nil, nil, parseConfigError(err.Error())
	}
	if root.kind != parseObject {
		return nil, nil, parseConfigError("the OpenAPI document must be an object")
	}
	if err := openapiCheckVersion(root); err != nil {
		return nil, nil, parseConfigError(err.Error())
	}
	doc := &openapiDoc{root: root, schemas: map[string]*parseNode{}, opts: o}
	if schemas := root.get("components").get("schemas"); schemas != nil && schemas.kind == parseObject {
		for i := range schemas.kids {
			doc.schemas[schemas.kids[i].key] = &schemas.kids[i]
		}
	}

	hooks := root.get("webhooks")
	if hooks == nil {
		return nil, nil, parseConfigError((&parseError{path: []string{"webhooks"}, msg: "the document defines no webhooks to import"}).Error())
	}
	if hooks.kind != parseObject {
		return nil, nil, parseConfigError((&parseError{path: []string{"webhooks"}, msg: "must be an object"}).Error())
	}
	defs := make(Definitions, len(hooks.kids))
	sources := make(map[string]string, len(hooks.kids))
	var problems, warnings []string
	for i := range hooks.kids {
		hook := &hooks.kids[i]
		topic, def, ok, err := doc.webhook(hook)
		if errors.Is(err, errOpenAPIBundleTooLarge) || errors.Is(err, errOpenAPIOverlayTooLarge) {
			return nil, nil, parseConfigError(err.Error())
		}
		var pe *parseError
		if errors.As(err, &pe) && !o.wants(pe.topic) {
			warnings = append(warnings, o.skipped(pe.topic, pe))
			continue
		}
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		if !ok {
			continue
		}
		if !o.wants(topic) {
			warnings = append(warnings, o.skipped(topic, nil))
			continue
		}
		at := parsePointer([]string{"webhooks", hook.key})
		if prev, dup := sources[topic]; dup {
			problems = append(problems, (&parseError{topic: topic, path: []string{"webhooks", hook.key}, msg: "duplicate topic, also defined by " + prev}).Error())
			continue
		}
		sources[topic] = at
		defs[topic] = def
	}
	if len(problems) > 0 {
		return nil, nil, parseConfigError(problems...)
	}
	slices.Sort(warnings)
	return defs, slices.Compact(warnings), nil
}

// OpenAPIOption configures ParseOpenAPI.
type OpenAPIOption func(*openapiOptions)

type openapiOptions struct {
	// topics lists the topics to import, and wanted holds them. A nil
	// wanted imports every topic.
	topics []string
	wanted map[string]bool
}

// OpenAPITopics imports only the webhooks whose topic is one of topics, the
// TOPICS list. An empty list skips every webhook.
func OpenAPITopics(topics []string) OpenAPIOption {
	return func(o *openapiOptions) {
		o.topics = slices.Clone(topics)
		o.wanted = make(map[string]bool, len(topics))
		for _, t := range topics {
			o.wanted[t] = true
		}
	}
}

// wants reports whether the webhooks of topic are imported.
func (o *openapiOptions) wants(topic string) bool {
	return o.wanted == nil || o.wanted[topic]
}

// skipped describes a webhook skipped because its topic is not wanted, with
// the problem that would fail its import, if one was found.
func (o *openapiOptions) skipped(topic string, problem *parseError) string {
	msg := fmt.Sprintf("imported topic %q is not in TOPICS and was skipped", topic)
	// TOPICS entries are used verbatim, so " a" and "a" differ.
	trimmed := strings.TrimSpace(topic)
	for _, t := range o.topics {
		if t != topic && strings.TrimSpace(t) == trimmed {
			msg += fmt.Sprintf(" (did you mean %q?)", t)
			break
		}
	}
	if problem != nil {
		p := *problem
		p.topic = ""
		msg += "; it would fail to import: " + p.Error()
	}
	return msg
}

// Merge returns the definitions of base and override, where an override entry
// replaces the base entry for the same topic whole: fields are never merged
// within a topic. Neither argument is modified.
func Merge(base, override Definitions) Definitions {
	out := make(Definitions, len(base)+len(override))
	maps.Copy(out, base)
	maps.Copy(out, override)
	return out
}

// parseError is one problem found while parsing, located by the JSON pointer
// tokens of the value in the document and, for YAML, the source line.
type parseError struct {
	topic string
	path  []string
	line  int
	msg   string
}

func (e *parseError) Error() string {
	var sb strings.Builder
	if e.topic != "" {
		fmt.Fprintf(&sb, "topic %q: ", e.topic)
	}
	switch {
	case len(e.path) > 0 && e.line > 0:
		fmt.Fprintf(&sb, "%s (line %d): ", parsePointer(e.path), e.line)
	case len(e.path) > 0:
		sb.WriteString(parsePointer(e.path) + ": ")
	case e.line > 0:
		fmt.Fprintf(&sb, "line %d: ", e.line)
	}
	sb.WriteString(e.msg)
	return sb.String()
}

func parseConfigError(problems ...string) error {
	sort.Strings(problems)
	return &ConfigError{Problems: problems}
}

func parseTooLarge(what string) error {
	return parseConfigError(fmt.Sprintf("%s is larger than the %d MiB limit", what, parseMaxInputBytes>>20))
}

// parseDefinitionsError reports a document error of a definitions file. Its
// first pointer token is the topic.
func parseDefinitionsError(err error) error {
	var pe *parseError
	if errors.As(err, &pe) && pe.topic == "" && len(pe.path) > 0 {
		pe.topic = pe.path[0]
	}
	return parseConfigError(err.Error())
}

// parsePointer renders JSON pointer tokens as a pointer string.
func parsePointer(tokens []string) string {
	var sb strings.Builder
	for _, t := range tokens {
		sb.WriteByte('/')
		sb.WriteString(escapePointerToken(t))
	}
	return sb.String()
}

// definitionFields and mcpFields are the fields accepted in a topic object
// and in its mcp object.
var (
	definitionFields = []string{"name", "description", "payload_schema", "validation", "mcp", "deprecated", "replaced_by"}
	mcpFields        = []string{"enabled"}
)

// parseDefinitions reads a definitions document. Every problem is reported.
func parseDefinitions(root *parseNode) (Definitions, error) {
	if root.kind != parseObject {
		return nil, parseConfigError("topic schemas must be an object keyed by topic name")
	}
	defs := make(Definitions, len(root.kids))
	var problems []string
	for i := range root.kids {
		m := &root.kids[i]
		def, errs := parseDefinition(m)
		problems = append(problems, errs...)
		defs[m.key] = def
	}
	if len(problems) > 0 {
		return nil, parseConfigError(problems...)
	}
	return defs, nil
}

// parseDefinition reads one topic object. null fields count as absent.
func parseDefinition(m *parseNode) (Definition, []string) {
	var def Definition
	var problems []string
	fail := func(msg string, path ...string) {
		problems = append(problems, (&parseError{topic: m.key, path: append([]string{m.key}, path...), msg: msg}).Error())
	}
	if m.kind != parseObject {
		fail("must be an object")
		return def, problems
	}
	str := func(f *parseNode, dst *string) {
		switch f.kind {
		case parseString:
			*dst = f.text
		case parseNull:
		default:
			fail("must be a string", f.key)
		}
	}
	boolean := func(f *parseNode, dst *bool, path ...string) {
		switch f.kind {
		case parseBool:
			*dst = f.text == "true"
		case parseNull:
		default:
			fail("must be a boolean", path...)
		}
	}
	for i := range m.kids {
		f := &m.kids[i]
		switch f.key {
		case "name":
			str(f, &def.Name)
		case "description":
			str(f, &def.Description)
		case "validation":
			var v string
			str(f, &v)
			def.Validation = ValidationMode(v)
		case "replaced_by":
			str(f, &def.ReplacedBy)
		case "deprecated":
			boolean(f, &def.Deprecated, f.key)
		case "payload_schema":
			if f.kind != parseNull {
				def.PayloadSchema = json.RawMessage(f.appendJSON(nil))
			}
		case "mcp":
			switch f.kind {
			case parseObject:
				for j := range f.kids {
					g := &f.kids[j]
					if g.key == "enabled" {
						boolean(g, &def.MCP.Enabled, f.key, g.key)
						continue
					}
					fail(parseUnknownField(g.key, mcpFields), f.key, g.key)
				}
			case parseNull:
			default:
				fail("must be an object", f.key)
			}
		default:
			fail(parseUnknownField(f.key, definitionFields), f.key)
		}
	}
	return def, problems
}

// parseUnknownField describes an unknown field, suggesting the known field it
// most likely meant: "payloadSchema" and "Payload-Schema" both suggest
// "payload_schema".
func parseUnknownField(name string, known []string) string {
	norm := func(s string) string {
		return strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(s))
	}
	for _, k := range known {
		if norm(k) == norm(name) {
			return fmt.Sprintf("unknown field %q (did you mean %q?)", name, k)
		}
	}
	return fmt.Sprintf("unknown field %q", name)
}

// parseKeySet detects duplicate object keys: a slice scan for small objects,
// a map once an object grows. The map holds each key's position, so it also
// indexes the object's members.
type parseKeySet struct {
	list []string
	set  map[string]int
}

// add records key and reports false when it was already present.
func (s *parseKeySet) add(key string) bool {
	if s.set != nil {
		if _, dup := s.set[key]; dup {
			return false
		}
		s.set[key] = len(s.set)
		return true
	}
	if slices.Contains(s.list, key) {
		return false
	}
	s.list = append(s.list, key)
	if len(s.list) > 16 {
		s.set = make(map[string]int, 2*len(s.list))
		for i, k := range s.list {
			s.set[k] = i
		}
		s.list = nil
	}
	return true
}

// parseYAMLDocument converts a single-document YAML stream to JSON.
func parseYAMLDocument(data []byte) (json.RawMessage, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, &parseError{msg: "the YAML document is empty"}
		}
		return nil, &parseError{msg: "invalid YAML: " + strings.TrimPrefix(err.Error(), "yaml: ")}
	}
	var next yaml.Node
	switch err := dec.Decode(&next); {
	case err == nil:
		return nil, &parseError{msg: "a YAML stream with more than one document is not supported"}
	case !errors.Is(err, io.EOF):
		return nil, &parseError{msg: "invalid YAML: " + strings.TrimPrefix(err.Error(), "yaml: ")}
	}
	return YAMLNodeToJSON(&doc)
}

// yamlConverter writes a YAML node tree as JSON, counting nodes across alias
// expansions.
type yamlConverter struct {
	buf   []byte
	nodes int
	// stack holds the mappings and sequences being written, to detect an
	// alias to a node that contains it.
	stack []*yaml.Node
	path  []string
}

func (c *yamlConverter) errorf(n *yaml.Node, format string, args ...any) error {
	return &parseError{path: slices.Clone(c.path), line: n.Line, msg: fmt.Sprintf(format, args...)}
}

// count charges one node against the expansion budget.
func (c *yamlConverter) count(n *yaml.Node) error {
	c.nodes++
	if c.nodes > yamlMaxNodes {
		return c.errorf(n, "the document expands to more than %d nodes; check for nested aliases", yamlMaxNodes)
	}
	if len(c.buf) > yamlMaxOutputBytes {
		return c.errorf(n, "the document expands to more than %d MiB of JSON; check for nested aliases", yamlMaxOutputBytes>>20)
	}
	return nil
}

func (c *yamlConverter) value(n *yaml.Node, depth int) error {
	if err := c.count(n); err != nil {
		return err
	}
	switch n.Kind {
	case yaml.ScalarNode:
		kind, text, err := yamlScalar(n)
		if err != nil {
			return c.errorf(n, "%s", err)
		}
		switch kind {
		case parseString:
			c.buf = parseAppendJSONString(c.buf, text)
		case parseNull:
			c.buf = append(c.buf, "null"...)
		default:
			c.buf = append(c.buf, text...)
		}
		return nil
	case yaml.AliasNode:
		target := n.Alias
		if target == nil {
			return c.errorf(n, "unknown anchor %q", n.Value)
		}
		if slices.Contains(c.stack, target) {
			return c.errorf(n, "alias *%s refers to a node that contains it", n.Value)
		}
		return c.value(target, depth)
	case yaml.MappingNode:
		if depth >= parseMaxDepth {
			return c.errorf(n, "nesting exceeds %d levels", parseMaxDepth)
		}
		c.stack = append(c.stack, n)
		c.buf = append(c.buf, '{')
		var keys parseKeySet
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			key, err := c.key(k)
			if err != nil {
				return err
			}
			if !keys.add(key) {
				return c.errorf(k, "duplicate key %q", key)
			}
			if i > 0 {
				c.buf = append(c.buf, ',')
			}
			c.buf = parseAppendJSONString(c.buf, key)
			c.buf = append(c.buf, ':')
			c.path = append(c.path, key)
			if err := c.value(v, depth+1); err != nil {
				return err
			}
			c.path = c.path[:len(c.path)-1]
		}
		c.buf = append(c.buf, '}')
		c.stack = c.stack[:len(c.stack)-1]
		return nil
	case yaml.SequenceNode:
		if depth >= parseMaxDepth {
			return c.errorf(n, "nesting exceeds %d levels", parseMaxDepth)
		}
		c.stack = append(c.stack, n)
		c.buf = append(c.buf, '[')
		for i, v := range n.Content {
			if i > 0 {
				c.buf = append(c.buf, ',')
			}
			c.path = append(c.path, strconv.Itoa(i))
			if err := c.value(v, depth+1); err != nil {
				return err
			}
			c.path = c.path[:len(c.path)-1]
		}
		c.buf = append(c.buf, ']')
		c.stack = c.stack[:len(c.stack)-1]
		return nil
	}
	return c.errorf(n, "unsupported YAML node")
}

// key returns the JSON name of a mapping key: the text of a scalar as written.
func (c *yamlConverter) key(k *yaml.Node) (string, error) {
	if err := c.count(k); err != nil {
		return "", err
	}
	if k.Kind == yaml.AliasNode && k.Alias != nil {
		k = k.Alias
	}
	if k.Kind != yaml.ScalarNode {
		return "", c.errorf(k, "mapping keys must be strings")
	}
	switch k.ShortTag() {
	case "!!merge":
		return "", c.errorf(k, "merge keys (<<) are not supported; repeat the keys, or use allOf in schemas")
	case "!!null":
		return "", c.errorf(k, "mapping keys must be strings, not null")
	}
	return k.Value, nil
}

const yamlQuotedStyles = yaml.DoubleQuotedStyle | yaml.SingleQuotedStyle | yaml.LiteralStyle | yaml.FoldedStyle

// yamlScalar types a scalar node with the YAML 1.2 core schema. It returns the
// JSON kind and either the string value or a valid JSON literal.
func yamlScalar(n *yaml.Node) (parseKind, string, error) {
	if n.Style&yaml.TaggedStyle != 0 {
		tag := n.ShortTag()
		switch tag {
		case "!!null", "!!bool", "!!int", "!!float":
			kind, text, err := yamlCoreScalar(n.Value)
			if err != nil {
				return 0, "", err
			}
			ok := false
			switch tag {
			case "!!null":
				ok = kind == parseNull
			case "!!bool":
				ok = kind == parseBool
			case "!!int":
				_, ok = yamlCoreInt(n.Value)
			case "!!float":
				ok = kind == parseNumber
			}
			if !ok {
				return 0, "", fmt.Errorf("%q is not a valid %s value", n.Value, tag)
			}
			return kind, text, nil
		}
		// !!str, !!timestamp, !!binary and custom tags keep the text.
		return parseString, n.Value, nil
	}
	// yaml.v3 tags plain scalars with YAML 1.1-style resolution; only its
	// !!str verdict is kept, since the core schema never types those.
	if n.Style&yamlQuotedStyles != 0 || n.Tag == "!!str" {
		return parseString, n.Value, nil
	}
	return yamlCoreScalar(n.Value)
}

// yamlCoreScalar resolves a plain scalar with the YAML 1.2 core schema.
func yamlCoreScalar(s string) (parseKind, string, error) {
	switch s {
	case "", "~", "null", "Null", "NULL":
		return parseNull, "", nil
	case "true", "True", "TRUE":
		return parseBool, "true", nil
	case "false", "False", "FALSE":
		return parseBool, "false", nil
	case ".inf", ".Inf", ".INF", "+.inf", "+.Inf", "+.INF", "-.inf", "-.Inf", "-.INF", ".nan", ".NaN", ".NAN":
		return 0, "", fmt.Errorf("%q is infinite or NaN, which JSON can't represent; quote it to keep it as a string", s)
	}
	if text, ok := yamlCoreInt(s); ok {
		return parseNumber, text, nil
	}
	if text, ok := yamlCoreFloat(s); ok {
		return parseNumber, text, nil
	}
	return parseString, s, nil
}

// yamlCoreInt matches [-+]?[0-9]+, 0o[0-7]+ and 0x[0-9a-fA-F]+, and returns
// the value as a JSON integer literal of any size.
func yamlCoreInt(s string) (string, bool) {
	if len(s) > 2 && s[0] == '0' && (s[1] == 'o' || s[1] == 'x') {
		base, digits := 8, "01234567"
		if s[1] == 'x' {
			base, digits = 16, "0123456789abcdefABCDEF"
		}
		for i := 2; i < len(s); i++ {
			if !strings.ContainsRune(digits, rune(s[i])) {
				return "", false
			}
		}
		v, ok := new(big.Int).SetString(s[2:], base)
		if !ok {
			return "", false
		}
		return v.String(), true
	}
	sign, digits := "", s
	if digits != "" && (digits[0] == '-' || digits[0] == '+') {
		if digits[0] == '-' {
			sign = "-"
		}
		digits = digits[1:]
	}
	if digits == "" || !parseAllDigits(digits) {
		return "", false
	}
	if digits = strings.TrimLeft(digits, "0"); digits == "" {
		digits = "0"
	}
	return sign + digits, true
}

// yamlCoreFloat matches [-+]?(\.[0-9]+|[0-9]+(\.[0-9]*)?)([eE][-+]?[0-9]+)?
// and returns it as a JSON number literal with the same digits: "+.5" becomes
// "0.5", "1." becomes "1.0" and "007.5" becomes "7.5".
func yamlCoreFloat(s string) (string, bool) {
	i, neg := 0, false
	if i < len(s) && (s[i] == '-' || s[i] == '+') {
		neg = s[i] == '-'
		i++
	}
	start := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	whole := s[start:i]
	frac, dot := "", false
	if i < len(s) && s[i] == '.' {
		dot = true
		i++
		start = i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		frac = s[start:i]
	}
	if whole == "" && frac == "" {
		return "", false
	}
	exp := ""
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		start = i
		i++
		if i < len(s) && (s[i] == '-' || s[i] == '+') {
			i++
		}
		digits := i
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == digits {
			return "", false
		}
		exp = s[start:i]
	}
	if i != len(s) {
		return "", false
	}
	if whole = strings.TrimLeft(whole, "0"); whole == "" {
		whole = "0"
	}
	var sb strings.Builder
	if neg {
		sb.WriteByte('-')
	}
	sb.WriteString(whole)
	if dot {
		sb.WriteByte('.')
		if frac == "" {
			frac = "0"
		}
		sb.WriteString(frac)
	}
	sb.WriteString(exp)
	return sb.String(), true
}

func parseAllDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// parseKind is the JSON type of a parseNode.
type parseKind uint8

const (
	parseNull parseKind = iota
	parseBool
	parseNumber
	parseString
	parseObject
	parseArray
)

// parseNode is a decoded JSON value that keeps object members in document
// order and numbers as written.
type parseNode struct {
	kind parseKind
	// key is the member name when the node is an object member.
	key string
	// text is the string value, the number literal, or "true"/"false".
	text string
	// kids are the object members or array items.
	kids []parseNode
	// index maps member names to their position in kids. The decoder sets it
	// on objects of more than 16 members, so many lookups into a large
	// object, such as $refs into components, don't each scan it. Nodes built
	// otherwise have none and are scanned.
	index map[string]int
}

// get returns the member named key, or nil when n is not an object or has no
// such member. It is nil-safe so lookups can be chained.
func (n *parseNode) get(key string) *parseNode {
	if i := n.find(key); i >= 0 {
		return &n.kids[i]
	}
	return nil
}

// find returns the position in kids of the member named key, or -1 when n is
// not an object or has no such member.
func (n *parseNode) find(key string) int {
	if n == nil || n.kind != parseObject {
		return -1
	}
	if n.index != nil {
		if i, ok := n.index[key]; ok {
			return i
		}
		return -1
	}
	for i := range n.kids {
		if n.kids[i].key == key {
			return i
		}
	}
	return -1
}

// appendJSON appends n as compact JSON without HTML escaping.
func (n *parseNode) appendJSON(buf []byte) []byte {
	switch n.kind {
	case parseNull:
		return append(buf, "null"...)
	case parseBool, parseNumber:
		return append(buf, n.text...)
	case parseString:
		return parseAppendJSONString(buf, n.text)
	case parseObject:
		buf = append(buf, '{')
		for i := range n.kids {
			if i > 0 {
				buf = append(buf, ',')
			}
			buf = parseAppendJSONString(buf, n.kids[i].key)
			buf = append(buf, ':')
			buf = n.kids[i].appendJSON(buf)
		}
		return append(buf, '}')
	case parseArray:
		buf = append(buf, '[')
		for i := range n.kids {
			if i > 0 {
				buf = append(buf, ',')
			}
			buf = n.kids[i].appendJSON(buf)
		}
		return append(buf, ']')
	}
	return buf
}

// parseAppendJSONString appends s as a JSON string, escaped like
// encoding/json with HTML escaping off: invalid UTF-8 becomes U+FFFD, and
// U+2028 and U+2029 are escaped for JavaScript.
func parseAppendJSONString(buf []byte, s string) []byte {
	const hex = "0123456789abcdef"
	buf = append(buf, '"')
	start := 0
	for i := 0; i < len(s); {
		if b := s[i]; b < utf8.RuneSelf {
			if b >= 0x20 && b != '"' && b != '\\' {
				i++
				continue
			}
			buf = append(buf, s[start:i]...)
			switch b {
			case '"', '\\':
				buf = append(buf, '\\', b)
			case '\b':
				buf = append(buf, '\\', 'b')
			case '\f':
				buf = append(buf, '\\', 'f')
			case '\n':
				buf = append(buf, '\\', 'n')
			case '\r':
				buf = append(buf, '\\', 'r')
			case '\t':
				buf = append(buf, '\\', 't')
			default:
				buf = append(buf, '\\', 'u', '0', '0', hex[b>>4], hex[b&0xF])
			}
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			buf = append(buf, s[start:i]...)
			buf = utf8.AppendRune(buf, utf8.RuneError)
			i += size
			start = i
			continue
		}
		if r == 0x2028 || r == 0x2029 {
			buf = append(buf, s[start:i]...)
			buf = append(buf, '\\', 'u', '2', '0', '2', hex[r&0xF])
			i += size
			start = i
			continue
		}
		i += size
	}
	buf = append(buf, s[start:]...)
	return append(buf, '"')
}

// parseDecodeJSON decodes a JSON document into a parseNode tree with the
// streaming decoder. Duplicate keys at any depth, nesting beyond
// parseMaxDepth and trailing data are errors.
func parseDecodeJSON(data []byte) (*parseNode, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	b := parseTreeBuilder{dec: dec}
	tok, err := dec.Token()
	if errors.Is(err, io.EOF) {
		return nil, &parseError{msg: "the JSON document is empty"}
	}
	if err != nil {
		return nil, b.syntaxError(err)
	}
	root, err := b.value(tok, 0)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, &parseError{msg: "unexpected data after the JSON document"}
	}
	return &root, nil
}

type parseTreeBuilder struct {
	dec  *json.Decoder
	path []string
}

func (b *parseTreeBuilder) errorf(format string, args ...any) error {
	return &parseError{path: slices.Clone(b.path), msg: fmt.Sprintf(format, args...)}
}

func (b *parseTreeBuilder) syntaxError(err error) error {
	var se *json.SyntaxError
	switch {
	case errors.As(err, &se):
		return b.errorf("invalid JSON at byte %d: %s", se.Offset, se)
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return b.errorf("invalid JSON: unexpected end of input")
	}
	return b.errorf("invalid JSON: %s", err)
}

func (b *parseTreeBuilder) value(tok json.Token, depth int) (parseNode, error) {
	switch t := tok.(type) {
	case json.Delim:
		if depth >= parseMaxDepth {
			return parseNode{}, b.errorf("nesting exceeds %d levels", parseMaxDepth)
		}
		if t == '{' {
			return b.object(depth)
		}
		return b.array(depth)
	case string:
		return parseNode{kind: parseString, text: t}, nil
	case json.Number:
		return parseNode{kind: parseNumber, text: string(t)}, nil
	case bool:
		if t {
			return parseNode{kind: parseBool, text: "true"}, nil
		}
		return parseNode{kind: parseBool, text: "false"}, nil
	case nil:
		return parseNode{kind: parseNull}, nil
	}
	return parseNode{}, b.errorf("unexpected JSON token %v", tok)
}

func (b *parseTreeBuilder) object(depth int) (parseNode, error) {
	n := parseNode{kind: parseObject}
	var keys parseKeySet
	for b.dec.More() {
		tok, err := b.dec.Token()
		if err != nil {
			return n, b.syntaxError(err)
		}
		key, ok := tok.(string)
		if !ok {
			return n, b.errorf("expected an object key")
		}
		if !keys.add(key) {
			return n, b.errorf("duplicate key %q", key)
		}
		b.path = append(b.path, key)
		if tok, err = b.dec.Token(); err != nil {
			return n, b.syntaxError(err)
		}
		child, err := b.value(tok, depth+1)
		if err != nil {
			return n, err
		}
		b.path = b.path[:len(b.path)-1]
		child.key = key
		n.kids = append(n.kids, child)
	}
	if _, err := b.dec.Token(); err != nil {
		return n, b.syntaxError(err)
	}
	n.index = keys.set
	return n, nil
}

func (b *parseTreeBuilder) array(depth int) (parseNode, error) {
	n := parseNode{kind: parseArray}
	for i := 0; b.dec.More(); i++ {
		b.path = append(b.path, strconv.Itoa(i))
		tok, err := b.dec.Token()
		if err != nil {
			return n, b.syntaxError(err)
		}
		child, err := b.value(tok, depth+1)
		if err != nil {
			return n, err
		}
		b.path = b.path[:len(b.path)-1]
		n.kids = append(n.kids, child)
	}
	if _, err := b.dec.Token(); err != nil {
		return n, b.syntaxError(err)
	}
	return n, nil
}

// errOpenAPIBundleTooLarge aborts an import whose bundled payload schemas
// exceed openapiMaxBundledBytes.
var errOpenAPIBundleTooLarge = fmt.Errorf("the payload schemas bundled from the OpenAPI document exceed %d MiB; reduce the components each webhook references", openapiMaxBundledBytes>>20)

// errOpenAPIOverlayTooLarge aborts an import whose $ref overlays copy more
// than openapiMaxOverlaidMembers members.
var errOpenAPIOverlayTooLarge = fmt.Errorf("the $ref overlays of the OpenAPI document copy more than %d members; drop the members next to $refs to large objects", openapiMaxOverlaidMembers)

// openapiCheckVersion accepts OpenAPI 3.1.x documents only: 3.0 schemas are
// not JSON Schema 2020-12 (nullable, exclusiveMinimum as a boolean, ...).
func openapiCheckVersion(root *parseNode) error {
	v := root.get("openapi")
	if v == nil {
		if root.get("swagger") != nil {
			return &parseError{path: []string{"swagger"}, msg: "Swagger 2.0 documents are not supported; only OpenAPI 3.1.x documents can be imported"}
		}
		return &parseError{path: []string{"openapi"}, msg: "missing; only OpenAPI 3.1.x documents can be imported"}
	}
	if v.kind != parseString {
		return &parseError{path: []string{"openapi"}, msg: `must be a version string such as "3.1.0"`}
	}
	if patch, ok := strings.CutPrefix(v.text, "3.1."); !ok || patch == "" || !parseAllDigits(patch) {
		return &parseError{path: []string{"openapi"}, msg: fmt.Sprintf("version %q is not supported; only OpenAPI 3.1.x documents can be imported", v.text)}
	}
	return nil
}

// openapiDoc is a decoded OpenAPI document.
type openapiDoc struct {
	root *parseNode
	// schemas indexes #/components/schemas by name.
	schemas map[string]*parseNode
	// bundled counts the bytes of payload schemas produced so far.
	bundled int
	// overlaid counts the members copied by overlay so far.
	overlaid int
	// opts selects the topics to import.
	opts openapiOptions
}

// webhook imports one webhook. ok is false when it is skipped. A webhook
// whose topic is not wanted is checked but not bundled, and returned without
// a payload schema.
func (d *openapiDoc) webhook(hook *parseNode) (topic string, def Definition, ok bool, err error) {
	topic = hook.key
	// x-outpost-topic next to a path item $ref names the topic even when the
	// reference doesn't resolve.
	if t := hook.get("x-outpost-topic"); t != nil && t.kind == parseString && t.text != "" {
		topic = t.text
	}
	item, path, err := d.follow(hook, []string{"webhooks", hook.key}, topic)
	if err != nil {
		return "", def, false, err
	}
	if item.kind != parseObject {
		return "", def, false, &parseError{topic: topic, path: path, msg: "must be a path item object"}
	}
	if topic, err = openapiTopicName(item, path, topic); err != nil {
		return "", def, false, err
	}
	post := item.get("post")
	if post == nil {
		return "", def, false, nil
	}
	op, opPath, err := d.follow(post, openapiPath(path, "post"), topic)
	if err != nil {
		return "", def, false, err
	}
	if op.kind != parseObject {
		return "", def, false, &parseError{topic: topic, path: opPath, msg: "must be an operation object"}
	}
	if topic, err = openapiTopicName(op, opPath, topic); err != nil {
		return "", def, false, err
	}

	summary, err := openapiString(op, "summary", opPath, topic)
	if err != nil {
		return "", def, false, err
	}
	description, err := openapiString(op, "description", opPath, topic)
	if err != nil {
		return "", def, false, err
	}
	def.Description = summary
	if def.Description == "" {
		def.Description = description
	}
	if def.MCP.Enabled, err = openapiBool(op, "x-mcp-enabled", opPath, topic); err != nil {
		return "", def, false, err
	}
	if def.Deprecated, err = openapiBool(op, "deprecated", opPath, topic); err != nil {
		return "", def, false, err
	}

	schema, schemaPath, err := d.requestSchema(op, opPath, topic)
	if err != nil {
		return "", def, false, err
	}
	if schema == nil {
		if def.MCP.Enabled {
			return "", def, false, &parseError{topic: topic, path: openapiPath(opPath, "x-mcp-enabled"), msg: "needs a request body with a JSON schema (application/json or *+json)"}
		}
		return "", def, false, nil
	}
	if !d.opts.wants(topic) {
		return topic, def, true, nil
	}
	if def.PayloadSchema, err = d.bundle(topic, schema, schemaPath); err != nil {
		return "", def, false, err
	}
	return topic, def, true, nil
}

// requestSchema returns the JSON request body schema of an operation, or nil
// when it has none.
func (d *openapiDoc) requestSchema(op *parseNode, opPath []string, topic string) (*parseNode, []string, error) {
	body := op.get("requestBody")
	if body == nil {
		return nil, nil, nil
	}
	body, path, err := d.follow(body, openapiPath(opPath, "requestBody"), topic)
	if err != nil {
		return nil, nil, err
	}
	content := body.get("content")
	if content == nil {
		return nil, nil, nil
	}
	if content.kind != parseObject {
		return nil, nil, &parseError{topic: topic, path: openapiPath(path, "content"), msg: "must be an object"}
	}
	media := openapiJSONMediaType(content)
	if media == nil {
		return nil, nil, nil
	}
	media, path, err = d.follow(media, openapiPath(path, "content", media.key), topic)
	if err != nil {
		return nil, nil, err
	}
	schema := media.get("schema")
	if schema == nil {
		return nil, nil, nil
	}
	return schema, openapiPath(path, "schema"), nil
}

// openapiJSONMediaType picks application/json, with or without parameters,
// else the first *+json media type in document order.
func openapiJSONMediaType(content *parseNode) *parseNode {
	var fallback *parseNode
	for i := range content.kids {
		m := &content.kids[i]
		mt, _, err := mime.ParseMediaType(m.key)
		if err != nil {
			continue
		}
		if mt == "application/json" {
			return m
		}
		if fallback == nil && strings.HasSuffix(mt, "+json") {
			fallback = m
		}
	}
	return fallback
}

// follow resolves a $ref at path item, operation, request body or media type
// level, to a local object. Fields next to $ref override the referenced
// object's, as summary and description do on a Reference Object. path
// becomes the location of the referenced object.
func (d *openapiDoc) follow(n *parseNode, path []string, topic string) (*parseNode, []string, error) {
	var seen []string
	for n.kind == parseObject {
		ref := n.get("$ref")
		if ref == nil {
			break
		}
		refPath := openapiPath(path, "$ref")
		if ref.kind != parseString {
			return nil, nil, &parseError{topic: topic, path: refPath, msg: "must be a string"}
		}
		if !strings.HasPrefix(ref.text, "#") {
			return nil, nil, &parseError{topic: topic, path: refPath, msg: fmt.Sprintf("external $ref %q is not supported; move the referenced object into the document", ref.text)}
		}
		if slices.Contains(seen, ref.text) {
			return nil, nil, &parseError{topic: topic, path: refPath, msg: fmt.Sprintf("circular $ref %q", ref.text)}
		}
		if len(seen) >= maxRefDepth {
			return nil, nil, &parseError{topic: topic, path: refPath, msg: fmt.Sprintf("$ref %q: more than %d chained references", ref.text, maxRefDepth)}
		}
		seen = append(seen, ref.text)
		tokens, pointer, err := openapiParseRef(ref.text)
		if err != nil || !pointer {
			return nil, nil, &parseError{topic: topic, path: refPath, msg: fmt.Sprintf("invalid $ref %q", ref.text)}
		}
		target := openapiResolve(d.root, tokens)
		if target == nil {
			return nil, nil, &parseError{topic: topic, path: refPath, msg: fmt.Sprintf("$ref %q does not resolve", ref.text)}
		}
		if n, err = d.overlay(target, n); err != nil {
			return nil, nil, err
		}
		path = tokens
	}
	return n, path, nil
}

// overlay returns target with the members of ref other than $ref set on top
// of it. target is returned as is when ref has no other member.
func (d *openapiDoc) overlay(target, ref *parseNode) (*parseNode, error) {
	if len(ref.kids) == 1 || target.kind != parseObject {
		return target, nil
	}
	if d.overlaid += len(target.kids) + len(ref.kids); d.overlaid > openapiMaxOverlaidMembers {
		return nil, errOpenAPIOverlayTooLarge
	}
	merged := &parseNode{kind: parseObject, key: target.key, kids: slices.Clone(target.kids)}
	for _, m := range ref.kids {
		if m.key == "$ref" {
			continue
		}
		// Members of ref are unique, so only target's can be replaced, and
		// target is a document node, indexed when large.
		if i := target.find(m.key); i >= 0 {
			merged.kids[i] = m
		} else {
			merged.kids = append(merged.kids, m)
		}
	}
	return merged, nil
}

func openapiTopicName(n *parseNode, path []string, current string) (string, error) {
	t := n.get("x-outpost-topic")
	if t == nil {
		return current, nil
	}
	if t.kind != parseString || t.text == "" {
		return "", &parseError{topic: current, path: openapiPath(path, "x-outpost-topic"), msg: "must be a non-empty string"}
	}
	return t.text, nil
}

func openapiString(n *parseNode, key string, path []string, topic string) (string, error) {
	v := n.get(key)
	switch {
	case v == nil || v.kind == parseNull:
		return "", nil
	case v.kind == parseString:
		return v.text, nil
	}
	return "", &parseError{topic: topic, path: openapiPath(path, key), msg: "must be a string"}
}

func openapiBool(n *parseNode, key string, path []string, topic string) (bool, error) {
	v := n.get(key)
	switch {
	case v == nil || v.kind == parseNull:
		return false, nil
	case v.kind == parseBool:
		return v.text == "true", nil
	}
	return false, &parseError{topic: topic, path: openapiPath(path, key), msg: "must be a boolean"}
}

// openapiPath returns a new path: path followed by tokens. path is never
// written to, so callers can keep it.
func openapiPath(path []string, tokens ...string) []string {
	return append(slices.Clip(path), tokens...)
}

// openapiParseRef splits a fragment-only reference into JSON pointer tokens,
// percent-decoding the fragment first. pointer is false for anchors such as
// "#node".
func openapiParseRef(ref string) (tokens []string, pointer bool, err error) {
	fragment, err := url.PathUnescape(strings.TrimPrefix(ref, "#"))
	if err != nil {
		return nil, false, err
	}
	if fragment == "" {
		return nil, true, nil
	}
	if fragment[0] != '/' {
		return nil, false, nil
	}
	tokens = strings.Split(fragment[1:], "/")
	for i, t := range tokens {
		tokens[i] = strings.ReplaceAll(strings.ReplaceAll(t, "~1", "/"), "~0", "~")
	}
	return tokens, true, nil
}

// openapiFragment renders JSON pointer tokens as a fragment-only reference.
func openapiFragment(tokens []string) string {
	var sb strings.Builder
	sb.WriteByte('#')
	for _, t := range tokens {
		sb.WriteByte('/')
		sb.WriteString(url.PathEscape(escapePointerToken(t)))
	}
	return sb.String()
}

// openapiResolve walks JSON pointer tokens from n, or returns nil.
func openapiResolve(n *parseNode, tokens []string) *parseNode {
	for _, t := range tokens {
		switch n.kind {
		case parseObject:
			if n = n.get(t); n == nil {
				return nil
			}
		case parseArray:
			i, err := strconv.Atoi(t)
			if err != nil || !parseAllDigits(t) || (len(t) > 1 && t[0] == '0') || i >= len(n.kids) {
				return nil
			}
			n = &n.kids[i]
		default:
			return nil
		}
	}
	return n
}

// openapiAnnotations are the keywords allowed next to the $ref of a payload
// schema that is inlined as the root: they describe, and don't constrain.
var openapiAnnotations = map[string]bool{
	"$comment":    true,
	"default":     true,
	"deprecated":  true,
	"description": true,
	"example":     true,
	"examples":    true,
	"readOnly":    true,
	"summary":     true,
	"title":       true,
	"writeOnly":   true,
}

// openapiSchemaKeywords lists the keywords whose values hold subschemas.
var openapiSchemaKeywords = map[string]openapiKeyword{
	"additionalItems":       openapiSubschemas,
	"additionalProperties":  openapiSubschemas,
	"allOf":                 openapiSubschemas,
	"anyOf":                 openapiSubschemas,
	"contains":              openapiSubschemas,
	"contentSchema":         openapiSubschemas,
	"else":                  openapiSubschemas,
	"if":                    openapiSubschemas,
	"items":                 openapiSubschemas,
	"not":                   openapiSubschemas,
	"oneOf":                 openapiSubschemas,
	"prefixItems":           openapiSubschemas,
	"propertyNames":         openapiSubschemas,
	"then":                  openapiSubschemas,
	"unevaluatedItems":      openapiSubschemas,
	"unevaluatedProperties": openapiSubschemas,
	"$defs":                 openapiSchemaMap,
	"definitions":           openapiSchemaMap,
	"dependencies":          openapiSchemaMap,
	"dependentSchemas":      openapiSchemaMap,
	"patternProperties":     openapiSchemaMap,
	"properties":            openapiSchemaMap,
}

type openapiKeyword uint8

const (
	// openapiSubschemas holds a schema or an array of schemas.
	openapiSubschemas openapiKeyword = iota + 1
	// openapiSchemaMap holds an object whose values are schemas.
	openapiSchemaMap
)

// openapiBundler turns one OpenAPI schema into a standalone payload schema.
//
// A reference to #/components/schemas/<name> becomes #/$defs/<name>, and the
// component is copied into the root $defs, transitively: a component
// referenced several times, or recursively, is copied once. A component never
// takes a $defs key the schema already defines; it gets a "_2", "_3", ...
// suffix instead. References into the payload schema itself, including to the
// component it was taken from, become "#..." pointers relative to its root.
//
// Other fragment references, such as "#/$defs/x" to the schema's own $defs,
// are kept as written and resolve against the payload schema, which is what
// JSON Schema authors expect. Anchor references such as "#node" are kept too,
// for NewCatalog to reject: inference and the breaking-change diff only
// follow JSON pointers. References to other parts of the document
// (#/paths/..., #/components/parameters/...) and external references are
// errors.
//
// Only schema keywords are walked, so a "$ref" inside enum, const, default,
// examples or an extension is data and left alone. $id is only allowed at the
// root: a nested one would change the base rewritten references resolve
// against.
type openapiBundler struct {
	doc   *openapiDoc
	topic string
	// root is the location in the document of the payload schema root.
	root []string
	// names maps a component to its $defs key, and taken holds the keys in
	// use.
	names map[string]string
	taken map[string]bool
	// queue lists the components to copy, in discovery order.
	queue []string
}

// openapiDialectBase is the default JSON Schema dialect of OpenAPI 3.1
// documents. A payload schema root declaring it has the declaration dropped.
const openapiDialectBase = "https://spec.openapis.org/oas/3.1/dialect/base"

// bundle returns the standalone payload schema for the schema at path.
func (d *openapiDoc) bundle(topic string, schema *parseNode, path []string) (json.RawMessage, error) {
	root, path, err := d.schemaRoot(schema, path)
	if err != nil {
		return nil, err
	}
	if root.kind != parseObject {
		// Boolean schemas hold no references.
		return json.RawMessage(root.appendJSON(nil)), nil
	}
	b := &openapiBundler{
		doc:   d,
		topic: topic,
		root:  slices.Clone(path),
		names: map[string]string{},
		taken: map[string]bool{},
	}
	defs := root.get("$defs")
	if defs != nil && defs.kind != parseObject {
		defs = nil
	}
	if defs != nil {
		for i := range defs.kids {
			b.taken[defs.kids[i].key] = true
		}
	}

	// Members are written separately so $defs, which grows with every
	// component reached, can be completed last and still keep its place.
	segments := make([][]byte, 0, len(root.kids)+1)
	defsAt := -1
	for i := range root.kids {
		m := &root.kids[i]
		if m == defs {
			defsAt = len(segments)
			segments = append(segments, nil)
			continue
		}
		if m.key == "$schema" && m.kind == parseString && m.text == openapiDialectBase {
			// The OAS 3.1 dialect is JSON Schema 2020-12 plus OpenAPI's own
			// annotation keywords; payload schemas are plain 2020-12.
			continue
		}
		seg, err := b.member(nil, m, openapiPath(path), true)
		if err != nil {
			return nil, err
		}
		segments = append(segments, seg)
	}
	var body []byte
	if defs != nil {
		for i := range defs.kids {
			k := &defs.kids[i]
			if i > 0 {
				body = append(body, ',')
			}
			body = parseAppendJSONString(body, k.key)
			body = append(body, ':')
			if body, err = b.schema(body, k, openapiPath(path, "$defs", k.key)); err != nil {
				return nil, err
			}
		}
	}
	for i := 0; i < len(b.queue); i++ {
		name := b.queue[i]
		if len(body) > 0 {
			body = append(body, ',')
		}
		body = parseAppendJSONString(body, b.names[name])
		body = append(body, ':')
		if body, err = b.schema(body, d.schemas[name], []string{"components", "schemas", name}); err != nil {
			return nil, err
		}
		if d.bundled+len(body) > openapiMaxBundledBytes {
			return nil, errOpenAPIBundleTooLarge
		}
	}
	if defs != nil || len(b.queue) > 0 {
		seg := append([]byte(`"$defs":{`), body...)
		seg = append(seg, '}')
		if defsAt >= 0 {
			segments[defsAt] = seg
		} else {
			segments = append(segments, seg)
		}
	}

	out := []byte{'{'}
	for i, seg := range segments {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, seg...)
	}
	out = append(out, '}')
	if d.bundled += len(out); d.bundled > openapiMaxBundledBytes {
		return nil, errOpenAPIBundleTooLarge
	}
	return json.RawMessage(out), nil
}

// schemaRoot follows a payload schema that is only a reference to a
// component, possibly with annotations next to it, to the component itself,
// so that {$ref: '#/components/schemas/Order'} yields Order's root "type" and
// "properties", which inference reads. The annotations replace the
// component's.
func (d *openapiDoc) schemaRoot(n *parseNode, path []string) (*parseNode, []string, error) {
	var seen []string
	for len(seen) < maxRefDepth && n.kind == parseObject {
		ref := n.get("$ref")
		if ref == nil || ref.kind != parseString || slices.Contains(seen, ref.text) {
			break
		}
		for _, m := range n.kids {
			if m.key != "$ref" && !openapiAnnotations[m.key] && !strings.HasPrefix(m.key, "x-") {
				return n, path, nil
			}
		}
		tokens, pointer, err := openapiParseRef(ref.text)
		if err != nil || !pointer || len(tokens) < 3 || tokens[0] != "components" || tokens[1] != "schemas" {
			break
		}
		target := openapiResolve(d.root, tokens)
		if target == nil {
			break
		}
		seen = append(seen, ref.text)
		if n, err = d.overlay(target, n); err != nil {
			return nil, nil, err
		}
		path = tokens
	}
	return n, path, nil
}

func (b *openapiBundler) errorf(path []string, format string, args ...any) error {
	return &parseError{topic: b.topic, path: slices.Clone(path), msg: fmt.Sprintf(format, args...)}
}

// schema appends the schema n, rewriting the references it holds.
func (b *openapiBundler) schema(buf []byte, n *parseNode, path []string) ([]byte, error) {
	if n.kind != parseObject {
		return n.appendJSON(buf), nil
	}
	buf = append(buf, '{')
	for i := range n.kids {
		if i > 0 {
			buf = append(buf, ',')
		}
		var err error
		if buf, err = b.member(buf, &n.kids[i], path, false); err != nil {
			return nil, err
		}
	}
	return append(buf, '}'), nil
}

// member appends one schema object member, "key":value. root reports whether
// the object is the payload schema root.
func (b *openapiBundler) member(buf []byte, m *parseNode, path []string, root bool) ([]byte, error) {
	buf = parseAppendJSONString(buf, m.key)
	buf = append(buf, ':')
	path = append(path, m.key)
	var err error
	switch m.key {
	case "$ref", "$dynamicRef":
		if m.kind == parseString {
			ref, err := b.ref(m.text, path)
			if err != nil {
				return nil, err
			}
			return parseAppendJSONString(buf, ref), nil
		}
	case "$id":
		if !root {
			return nil, b.errorf(path, "$id is only supported at the payload schema root, since it changes how references below it resolve")
		}
	case "discriminator":
		return b.discriminator(buf, m, path)
	}
	switch openapiSchemaKeywords[m.key] {
	case openapiSubschemas:
		if m.kind != parseArray {
			return b.schema(buf, m, path)
		}
		buf = append(buf, '[')
		for i := range m.kids {
			if i > 0 {
				buf = append(buf, ',')
			}
			if buf, err = b.schema(buf, &m.kids[i], append(path, strconv.Itoa(i))); err != nil {
				return nil, err
			}
		}
		return append(buf, ']'), nil
	case openapiSchemaMap:
		if m.kind != parseObject {
			break
		}
		buf = append(buf, '{')
		for i := range m.kids {
			k := &m.kids[i]
			if i > 0 {
				buf = append(buf, ',')
			}
			buf = parseAppendJSONString(buf, k.key)
			buf = append(buf, ':')
			if buf, err = b.schema(buf, k, append(path, k.key)); err != nil {
				return nil, err
			}
		}
		return append(buf, '}'), nil
	}
	return m.appendJSON(buf), nil
}

// discriminator appends an OpenAPI discriminator, rewriting the component
// references in its mapping. Mapping values that are plain schema names are
// kept.
func (b *openapiBundler) discriminator(buf []byte, m *parseNode, path []string) ([]byte, error) {
	if m.get("mapping") == nil || m.get("mapping").kind != parseObject {
		return m.appendJSON(buf), nil
	}
	buf = append(buf, '{')
	for i := range m.kids {
		k := &m.kids[i]
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = parseAppendJSONString(buf, k.key)
		buf = append(buf, ':')
		if k.key != "mapping" {
			buf = k.appendJSON(buf)
			continue
		}
		buf = append(buf, '{')
		for j := range k.kids {
			v := &k.kids[j]
			if j > 0 {
				buf = append(buf, ',')
			}
			buf = parseAppendJSONString(buf, v.key)
			buf = append(buf, ':')
			if v.kind != parseString || !strings.HasPrefix(v.text, "#/components/schemas/") {
				buf = v.appendJSON(buf)
				continue
			}
			ref, err := b.ref(v.text, openapiPath(path, "mapping", v.key))
			if err != nil {
				return nil, err
			}
			buf = parseAppendJSONString(buf, ref)
		}
		buf = append(buf, '}')
	}
	return append(buf, '}'), nil
}

// ref rewrites one reference found at path. See openapiBundler.
func (b *openapiBundler) ref(ref string, path []string) (string, error) {
	if !strings.HasPrefix(ref, "#") {
		return "", b.errorf(path, "external $ref %q is not supported; move the schema under #/components/schemas", ref)
	}
	tokens, pointer, err := openapiParseRef(ref)
	if err != nil {
		return "", b.errorf(path, "invalid $ref %q", ref)
	}
	if !pointer {
		return ref, nil
	}
	if len(tokens) >= len(b.root) && slices.Equal(tokens[:len(b.root)], b.root) {
		return openapiFragment(tokens[len(b.root):]), nil
	}
	if len(tokens) >= 3 && tokens[0] == "components" && tokens[1] == "schemas" {
		component, ok := b.doc.schemas[tokens[2]]
		if !ok || openapiResolve(component, tokens[3:]) == nil {
			return "", b.errorf(path, "$ref %q does not resolve", ref)
		}
		return openapiFragment(append([]string{"$defs", b.defName(tokens[2])}, tokens[3:]...)), nil
	}
	if len(tokens) > 0 && b.doc.root.get(tokens[0]) != nil {
		return "", b.errorf(path, "unsupported $ref %q: only #/components/schemas references can be bundled into a payload schema", ref)
	}
	return ref, nil
}

// defName returns the $defs key of a component, queueing it for copying the
// first time.
func (b *openapiBundler) defName(component string) string {
	if name, ok := b.names[component]; ok {
		return name
	}
	name := component
	for i := 2; b.taken[name]; i++ {
		name = component + "_" + strconv.Itoa(i)
	}
	b.names[component] = name
	b.taken[name] = true
	b.queue = append(b.queue, component)
	return name
}
