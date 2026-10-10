package topicschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Parse limits. Definitions come from operator files and environment values;
// every stage is bounded so a small document can't exhaust memory or the
// stack.
const (
	// parseMaxInputBytes caps a definitions document.
	parseMaxInputBytes = 4 << 20
	// parseMaxDepth caps JSON and YAML nesting, which also bounds recursion.
	parseMaxDepth = 128
	// parseMaxNodes caps the values of a JSON document and the nodes of a
	// YAML document, which each take about a hundred bytes or more once
	// decoded. With yamlMaxOutputBytes, it also bounds alias expansion: a
	// few hundred bytes of nested anchors otherwise expand to billions of
	// nodes.
	parseMaxNodes      = 1_000_000
	yamlMaxOutputBytes = 16 << 20
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
// Aliases are expanded in place, within a budget of parseMaxNodes nodes,
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
		return nil, c.tooLarge(n)
	}
	return json.RawMessage(c.buf), nil
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
		fmt.Fprintf(&sb, "topic %q: ", clip(e.topic))
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
	return &ConfigError{Problems: limitMessages(problems)}
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

// parsePointer renders JSON pointer tokens as a pointer string for messages,
// with each token clipped.
func parsePointer(tokens []string) string {
	var sb strings.Builder
	for _, t := range tokens {
		sb.WriteByte('/')
		sb.WriteString(escapePointerToken(clip(t)))
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
			return fmt.Sprintf("unknown field %q (did you mean %q?)", clip(name), k)
		}
	}
	return fmt.Sprintf("unknown field %q", clip(name))
}

// parseKeySet detects duplicate object keys.
type parseKeySet struct {
	set map[string]struct{}
}

// add records key and reports false when it was already present.
func (s *parseKeySet) add(key string) bool {
	if _, dup := s.set[key]; dup {
		return false
	}
	if s.set == nil {
		s.set = map[string]struct{}{}
	}
	s.set[key] = struct{}{}
	return true
}

// parseYAMLDocument converts a single-document YAML stream to JSON.
func parseYAMLDocument(data []byte) (json.RawMessage, error) {
	// yaml.v3 builds the whole node tree before YAMLNodeToJSON can count
	// it, at a few hundred bytes a node.
	if yamlPotentialNodes(data) > parseMaxNodes {
		return nil, &parseError{msg: fmt.Sprintf("the YAML document may have more than %d nodes", parseMaxNodes)}
	}
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

// yamlPotentialNodes bounds the nodes yaml.v3 builds for data, without
// decoding it. A node starts after a line break, a colon, a comma, an
// opening bracket or brace, or a block sequence entry or explicit key
// indicator. In flow collections, an entry can make two: a key and an
// implicit null value, or a single-pair mapping and its key.
func yamlPotentialNodes(data []byte) int {
	n := 2 // the document and its root
	for i, b := range data {
		switch b {
		case '\n', ':':
			n++
		case ',', '[', '{':
			n += 2
		case '-', '?':
			if i+1 == len(data) || data[i+1] == ' ' || data[i+1] == '\t' || data[i+1] == '\r' || data[i+1] == '\n' {
				n++
			}
		}
	}
	return n
}

// yamlConverter writes a YAML node tree as JSON, counting nodes across alias
// expansions.
type yamlConverter struct {
	buf   []byte
	nodes int
	// aliased reports that an alias was expanded, which can make a small
	// document large.
	aliased bool
	// stack holds the mappings and sequences being written, to detect an
	// alias to a node that contains it.
	stack []*yaml.Node
	path  []string
}

func (c *yamlConverter) errorf(n *yaml.Node, format string, args ...any) error {
	return &parseError{path: slices.Clone(c.path), line: n.Line, msg: fmt.Sprintf(format, args...)}
}

// count charges one node against the budget.
func (c *yamlConverter) count(n *yaml.Node) error {
	c.nodes++
	if c.nodes > parseMaxNodes {
		if c.aliased {
			return c.errorf(n, "the document expands to more than %d nodes; check for nested aliases", parseMaxNodes)
		}
		return c.errorf(n, "the document has more than %d nodes", parseMaxNodes)
	}
	if len(c.buf) > yamlMaxOutputBytes {
		return c.tooLarge(n)
	}
	return nil
}

// tooLarge reports output past yamlMaxOutputBytes.
func (c *yamlConverter) tooLarge(n *yaml.Node) error {
	if c.aliased {
		return c.errorf(n, "the document expands to more than %d MiB of JSON; check for nested aliases", yamlMaxOutputBytes>>20)
	}
	return c.errorf(n, "the document is more than %d MiB as JSON", yamlMaxOutputBytes>>20)
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
			return c.errorf(n, "unknown anchor %q", clip(n.Value))
		}
		if slices.Contains(c.stack, target) {
			return c.errorf(n, "alias *%s refers to a node that contains it", clip(n.Value))
		}
		c.aliased = true
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
				return c.errorf(k, "duplicate key %q", clip(key))
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
				return 0, "", fmt.Errorf("%q is not a valid %s value", clip(n.Value), tag)
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
		return 0, "", fmt.Errorf("%q is infinite or NaN, which JSON can't represent; quote it to keep it as a string", clip(s))
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
	// values counts the values decoded, up to parseMaxNodes.
	values int
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
	if b.values++; b.values > parseMaxNodes {
		return parseNode{}, b.errorf("the JSON document has more than %d values", parseMaxNodes)
	}
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
			return n, b.errorf("duplicate key %q", clip(key))
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
