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

	"gopkg.in/yaml.v3"
)

const (
	// maxInputBytes caps a definitions document.
	maxInputBytes = 4 << 20
	// maxDepth caps nesting, which bounds recursion.
	maxDepth = 128
	// maxOutputBytes caps all payload schemas of a document together, after
	// alias expansion, so nested anchors can't expand to gigabytes.
	maxOutputBytes = 16 << 20
)

var utf8BOM = []byte("\xef\xbb\xbf")

// definitionFields are the fields accepted in an event definition.
var definitionFields = []string{"description", "payload_schema"}

// ParseDefinitions parses event definitions, a YAML or JSON mapping keyed by
// event name, from a file. Unknown fields and duplicate keys are errors.
// payload_schema is kept as compact JSON in document order. Errors are a
// *ConfigError naming the event and the JSON pointer of each problem.
func ParseDefinitions(data []byte) (Definitions, error) {
	if len(data) > maxInputBytes {
		return nil, configError(fmt.Sprintf("the event definitions document is larger than the %d MiB limit", maxInputBytes>>20))
	}
	dec := yaml.NewDecoder(bytes.NewReader(bytes.TrimPrefix(data, utf8BOM)))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, configError("the definitions document is empty")
		}
		return nil, configError("invalid YAML or JSON: " + strings.TrimPrefix(err.Error(), "yaml: "))
	}
	var next yaml.Node
	switch err := dec.Decode(&next); {
	case err == nil:
		return nil, configError("a stream with more than one document is not supported")
	case !errors.Is(err, io.EOF):
		return nil, configError("invalid YAML or JSON: " + strings.TrimPrefix(err.Error(), "yaml: "))
	}
	return ParseDefinitionsNode(&doc)
}

// ParseDefinitionsJSON parses event definitions from a JSON document, as
// ParseDefinitions does for YAML.
func ParseDefinitionsJSON(data []byte) (Definitions, error) {
	if len(data) > maxInputBytes {
		return nil, configError(fmt.Sprintf("the event definitions document is larger than the %d MiB limit", maxInputBytes>>20))
	}
	data = bytes.TrimPrefix(data, utf8BOM)
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, configError("the definitions document is empty")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	b := &jsonNodes{dec: dec, data: data}
	root, err := b.next(0)
	if err != nil {
		return nil, configError("invalid JSON: " + err.Error())
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, configError("invalid JSON: unexpected data after the top-level value")
	}
	return ParseDefinitionsNode(root)
}

// jsonNodes reads JSON as YAML nodes, keeping key order and duplicate keys
// so that the YAML path checks them.
type jsonNodes struct {
	dec  *json.Decoder
	data []byte
}

func (b *jsonNodes) line() int {
	return 1 + bytes.Count(b.data[:b.dec.InputOffset()], []byte("\n"))
}

func (b *jsonNodes) next(depth int) (*yaml.Node, error) {
	tok, err := b.dec.Token()
	if err != nil {
		return nil, err
	}
	n := &yaml.Node{Line: b.line()}
	switch t := tok.(type) {
	case json.Delim:
		if depth >= maxDepth {
			return nil, fmt.Errorf("nesting exceeds %d levels", maxDepth)
		}
		n.Kind = yaml.SequenceNode
		if t == '{' {
			n.Kind = yaml.MappingNode
		}
		for b.dec.More() {
			if n.Kind == yaml.MappingNode {
				k, err := b.next(depth + 1)
				if err != nil {
					return nil, err
				}
				n.Content = append(n.Content, k)
			}
			v, err := b.next(depth + 1)
			if err != nil {
				return nil, err
			}
			n.Content = append(n.Content, v)
		}
		if _, err := b.dec.Token(); err != nil {
			return nil, err
		}
	case string:
		n.Kind, n.Style, n.Value, n.Tag = yaml.ScalarNode, yaml.DoubleQuotedStyle, t, "!!str"
	case json.Number:
		n.Kind, n.Value = yaml.ScalarNode, t.String()
	case bool:
		n.Kind, n.Value = yaml.ScalarNode, strconv.FormatBool(t)
	case nil:
		n.Kind, n.Value, n.Tag = yaml.ScalarNode, "null", "!!null"
	}
	return n, nil
}

// ParseDefinitionsNode parses event definitions from a YAML node, as
// ParseDefinitions does. Scalars follow the YAML 1.2 core schema, so dates
// and other timestamps stay strings.
func ParseDefinitionsNode(n *yaml.Node) (Definitions, error) {
	n = unalias(n)
	if n != nil && n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = unalias(n.Content[0])
	}
	if n == nil || n.Kind != yaml.MappingNode {
		return nil, configError("event definitions must be a mapping keyed by event name")
	}
	defs := Definitions{}
	var problems []string
	keys := map[string]struct{}{}
	c := &converter{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		c.path = nil
		name, err := c.uniqueKey(keys, n.Content[i])
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		c.path = []string{name}
		def, err := c.definition(name, unalias(n.Content[i+1]))
		if err != nil {
			var ce *ConfigError
			if errors.As(err, &ce) {
				problems = append(problems, ce.Problems...)
			} else {
				problems = append(problems, err.Error())
			}
			if c.written > maxOutputBytes {
				break
			}
			continue
		}
		defs[name] = def
	}
	if len(problems) > 0 {
		return nil, configError(problems...)
	}
	return defs, nil
}

func configError(problems ...string) error {
	return &ConfigError{Problems: limitMessages(problems)}
}

func unalias(n *yaml.Node) *yaml.Node {
	if n != nil && n.Kind == yaml.AliasNode && n.Alias != nil {
		return n.Alias
	}
	return n
}

// parseError is one problem found while parsing, located by the JSON pointer
// of the value and, when known, the source line.
type parseError struct {
	path []string
	line int
	msg  string
}

func (e *parseError) Error() string {
	var sb strings.Builder
	if len(e.path) > 0 {
		fmt.Fprintf(&sb, "event %q: ", clip(e.path[0]))
		for _, t := range e.path {
			sb.WriteString("/" + escapePointerToken(clip(t)))
		}
		if e.line > 0 {
			fmt.Fprintf(&sb, " (line %d)", e.line)
		}
		sb.WriteString(": ")
	} else if e.line > 0 {
		fmt.Fprintf(&sb, "line %d: ", e.line)
	}
	sb.WriteString(e.msg)
	return sb.String()
}

// converter writes YAML nodes as JSON, tracking where it is for errors.
type converter struct {
	buf []byte
	// written is the size of the schemas already written, for the document
	// budget.
	written int
	path    []string
	// stack holds the collections being written, to detect an alias to a
	// node that contains it.
	stack []*yaml.Node
}

func (c *converter) errorf(n *yaml.Node, format string, args ...any) error {
	return &parseError{path: slices.Clone(c.path), line: n.Line, msg: fmt.Sprintf(format, args...)}
}

// definition reads one event definition. null fields count as absent.
func (c *converter) definition(name string, n *yaml.Node) (Definition, error) {
	var def Definition
	if n.Kind != yaml.MappingNode {
		return def, c.errorf(n, "must be an object")
	}
	var problems []string
	seen := map[string]struct{}{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		field, err := c.uniqueKey(seen, n.Content[i])
		if err != nil {
			problems = append(problems, err.Error())
			continue
		}
		v := unalias(n.Content[i+1])
		c.path = []string{name, field}
		switch field {
		case "description":
			kind, text, err := scalar(v)
			switch {
			case v.Kind == yaml.ScalarNode && err == nil && kind == kindString:
				def.Description = text
			case v.Kind == yaml.ScalarNode && err == nil && kind == kindNull:
			default:
				problems = append(problems, c.errorf(v, "must be a string").Error())
			}
		case "payload_schema":
			if v.Kind == yaml.ScalarNode && v.ShortTag() == "!!null" {
				break
			}
			c.buf = c.buf[:0]
			err := c.value(v, 0)
			c.written += len(c.buf)
			if err != nil {
				problems = append(problems, err.Error())
				break
			}
			def.PayloadSchema = json.RawMessage(bytes.Clone(c.buf))
		default:
			problems = append(problems, c.errorf(n.Content[i], "%s", unknownField(field)).Error())
		}
	}
	if len(problems) > 0 {
		return def, &ConfigError{Problems: problems}
	}
	return def, nil
}

// unknownField describes an unknown field, suggesting the known field it
// most likely meant: "payloadSchema" and "Payload-Schema" both suggest
// "payload_schema".
func unknownField(name string) string {
	norm := func(s string) string {
		return strings.NewReplacer("_", "", "-", "", " ", "").Replace(strings.ToLower(s))
	}
	for _, k := range definitionFields {
		if norm(k) == norm(name) {
			return fmt.Sprintf("unknown field %q (did you mean %q?)", clip(name), k)
		}
	}
	return fmt.Sprintf("unknown field %q", clip(name))
}

func (c *converter) value(n *yaml.Node, depth int) error {
	if c.written+len(c.buf) > maxOutputBytes {
		return c.errorf(n, "the schemas are more than %d MiB as JSON in total; check for nested aliases", maxOutputBytes>>20)
	}
	switch n.Kind {
	case yaml.ScalarNode:
		kind, text, err := scalar(n)
		if err != nil {
			return c.errorf(n, "%s", err)
		}
		switch kind {
		case kindString:
			c.appendString(text)
		case kindNull:
			c.buf = append(c.buf, "null"...)
		default:
			c.buf = append(c.buf, text...)
		}
		return nil
	case yaml.AliasNode:
		if n.Alias == nil {
			return c.errorf(n, "unknown anchor %q", clip(n.Value))
		}
		if slices.Contains(c.stack, n.Alias) {
			return c.errorf(n, "alias *%s refers to a node that contains it", clip(n.Value))
		}
		return c.value(n.Alias, depth)
	case yaml.MappingNode, yaml.SequenceNode:
		if depth >= maxDepth {
			return c.errorf(n, "nesting exceeds %d levels", maxDepth)
		}
		c.stack = append(c.stack, n)
		defer func() { c.stack = c.stack[:len(c.stack)-1] }()
		if n.Kind == yaml.SequenceNode {
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
			return nil
		}
		c.buf = append(c.buf, '{')
		keys := map[string]struct{}{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, err := c.uniqueKey(keys, n.Content[i])
			if err != nil {
				return err
			}
			if i > 0 {
				c.buf = append(c.buf, ',')
			}
			c.appendString(key)
			c.buf = append(c.buf, ':')
			c.path = append(c.path, key)
			if err := c.value(n.Content[i+1], depth+1); err != nil {
				return err
			}
			c.path = c.path[:len(c.path)-1]
		}
		c.buf = append(c.buf, '}')
		return nil
	}
	return c.errorf(n, "unsupported YAML node")
}

func (c *converter) appendString(s string) {
	b, _ := marshalNoEscape(s)
	c.buf = append(c.buf, b...)
}

// uniqueKey returns the name of a mapping key and records it in seen, or
// fails when it was already there.
func (c *converter) uniqueKey(seen map[string]struct{}, k *yaml.Node) (string, error) {
	key, err := c.key(k)
	if err != nil {
		return "", err
	}
	if _, dup := seen[key]; dup {
		return "", c.errorf(k, "duplicate key %q", clip(key))
	}
	seen[key] = struct{}{}
	return key, nil
}

// key returns the JSON name of a mapping key: the text of a scalar as written.
func (c *converter) key(k *yaml.Node) (string, error) {
	k = unalias(k)
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

// scalarKind is the JSON type of a scalar.
type scalarKind uint8

const (
	kindNull scalarKind = iota
	kindBool
	kindNumber
	kindString
)

const quotedStyles = yaml.DoubleQuotedStyle | yaml.SingleQuotedStyle | yaml.LiteralStyle | yaml.FoldedStyle

// scalar types a scalar node with the YAML 1.2 core schema. It returns the
// JSON kind and either the string value or a valid JSON literal.
func scalar(n *yaml.Node) (scalarKind, string, error) {
	if n.Style&yaml.TaggedStyle != 0 {
		tag := n.ShortTag()
		switch tag {
		case "!!null", "!!bool", "!!int", "!!float":
			kind, text, err := coreScalar(n.Value)
			if err != nil {
				return 0, "", err
			}
			ok := false
			switch tag {
			case "!!null":
				ok = kind == kindNull
			case "!!bool":
				ok = kind == kindBool
			case "!!int":
				_, ok = coreInt(n.Value)
			case "!!float":
				ok = kind == kindNumber
			}
			if !ok {
				return 0, "", fmt.Errorf("%q is not a valid %s value", clip(n.Value), tag)
			}
			return kind, text, nil
		}
		// !!str, !!timestamp, !!binary and custom tags keep the text.
		return kindString, n.Value, nil
	}
	// yaml.v3 tags plain scalars with YAML 1.1-style resolution; only its
	// !!str verdict is kept, since the core schema never types those.
	if n.Style&quotedStyles != 0 || n.Tag == "!!str" {
		return kindString, n.Value, nil
	}
	return coreScalar(n.Value)
}

// coreScalar resolves a plain scalar with the YAML 1.2 core schema.
func coreScalar(s string) (scalarKind, string, error) {
	switch s {
	case "", "~", "null", "Null", "NULL":
		return kindNull, "", nil
	case "true", "True", "TRUE":
		return kindBool, "true", nil
	case "false", "False", "FALSE":
		return kindBool, "false", nil
	case ".inf", ".Inf", ".INF", "+.inf", "+.Inf", "+.INF", "-.inf", "-.Inf", "-.INF", ".nan", ".NaN", ".NAN":
		return 0, "", fmt.Errorf("%q is infinite or NaN, which JSON can't represent; quote it to keep it as a string", clip(s))
	}
	if text, ok := coreInt(s); ok {
		return kindNumber, text, nil
	}
	if text, ok := coreFloat(s); ok {
		return kindNumber, text, nil
	}
	return kindString, s, nil
}

// coreInt matches [-+]?[0-9]+, 0o[0-7]+ and 0x[0-9a-fA-F]+, and returns
// the value as a JSON integer literal of any size.
func coreInt(s string) (string, bool) {
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
	if digits == "" || !allDigits(digits) {
		return "", false
	}
	if digits = strings.TrimLeft(digits, "0"); digits == "" {
		digits = "0"
	}
	return sign + digits, true
}

// coreFloat matches [-+]?(\.[0-9]+|[0-9]+(\.[0-9]*)?)([eE][-+]?[0-9]+)?
// and returns it as a JSON number literal with the same digits: "+.5" becomes
// "0.5", "1." becomes "1.0" and "007.5" becomes "7.5".
func coreFloat(s string) (string, bool) {
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

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
