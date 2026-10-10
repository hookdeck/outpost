package topicschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
)

// member is one key/value pair of a JSON object, in document order.
type member struct {
	Key   string
	Value json.RawMessage
}

// orderedObject is a JSON object that marshals its members in order.
type orderedObject []member

func (o orderedObject) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, m := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, err := marshalNoEscape(m.Key)
		if err != nil {
			return nil, err
		}
		buf.Write(key)
		buf.WriteByte(':')
		buf.Write(m.Value)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// get returns the value for key, or nil when absent.
func (o orderedObject) get(key string) json.RawMessage {
	for _, m := range o {
		if m.Key == key {
			return m.Value
		}
	}
	return nil
}

// objectMembers returns the members of a JSON object in document order.
// Duplicate keys are an error, since JSON Schema behaviour is undefined for
// them and encoding/json would silently keep the last one.
func objectMembers(raw json.RawMessage) (orderedObject, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, errors.New("expected a JSON object")
	}
	var out orderedObject
	seen := make(map[string]struct{})
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, errors.New("expected an object key")
		}
		if _, dup := seen[key]; dup {
			return nil, fmt.Errorf("duplicate key %q", key)
		}
		seen[key] = struct{}{}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return nil, err
		}
		out = append(out, member{Key: key, Value: value})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("unexpected data after JSON object")
	}
	return out, nil
}

// decodeJSON decodes raw into a generic tree, keeping numbers as json.Number
// so integers beyond 2^53 and exact decimals survive. Trailing data is an error.
func decodeJSON(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("unexpected data after JSON value")
	}
	return v, nil
}

// marshalNoEscape marshals v without HTML escaping, so <, > and & stay as
// written. Map keys are sorted, as encoding/json always does.
func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// canonicalJSON re-encodes raw with sorted object keys, no insignificant
// whitespace and no HTML escaping. Numbers keep their textual form.
func canonicalJSON(raw []byte) ([]byte, error) {
	v, err := decodeJSON(raw)
	if err != nil {
		return nil, err
	}
	return marshalNoEscape(v)
}

// resolveLocalRef resolves a same-document JSON Schema reference such as
// "#/$defs/order" against root, a tree from decodeJSON. Only fragment-only
// JSON pointer references are supported; anything else is an error.
func resolveLocalRef(root any, ref string) (any, error) {
	if !strings.HasPrefix(ref, "#") {
		return nil, fmt.Errorf("unsupported $ref %q: only local references are allowed", ref)
	}
	fragment, err := url.PathUnescape(ref[1:])
	if err != nil {
		return nil, fmt.Errorf("invalid $ref %q: %w", ref, err)
	}
	if fragment == "" {
		return root, nil
	}
	if !strings.HasPrefix(fragment, "/") {
		return nil, fmt.Errorf("unsupported $ref %q: anchors are not supported", ref)
	}
	cur := root
	for _, token := range strings.Split(fragment[1:], "/") {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[token]
			if !ok {
				return nil, fmt.Errorf("$ref %q does not resolve", ref)
			}
			cur = next
		case []any:
			i, err := strconv.Atoi(token)
			if err != nil || i < 0 || i >= len(node) {
				return nil, fmt.Errorf("$ref %q does not resolve", ref)
			}
			cur = node[i]
		default:
			return nil, fmt.Errorf("$ref %q does not resolve", ref)
		}
	}
	return cur, nil
}

// pointerRef reports whether ref is a fragment-only JSON pointer reference,
// "#" or "#/..." once percent-decoded, the only kind resolveLocalRef
// follows. JSON Schema reads any other fragment as an anchor.
func pointerRef(ref string) bool {
	fragment, ok := strings.CutPrefix(ref, "#")
	if !ok {
		return false
	}
	fragment, err := url.PathUnescape(fragment)
	return err == nil && (fragment == "" || fragment[0] == '/')
}

// canonicalNumber rewrites a JSON number literal as its significant digits
// and a decimal exponent, so literals of equal value, such as 30, 30.0,
// 3e1 and 300e-1, are equal ("3e1"). It reports false when the exponent is
// too large to handle.
func canonicalNumber(s string) (string, bool) {
	sign := ""
	if rest, ok := strings.CutPrefix(s, "-"); ok {
		sign, s = "-", rest
	}
	exp := 0
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		e, err := strconv.Atoi(s[i+1:])
		if err != nil || e > 1<<30 || e < -1<<30 {
			return "", false
		}
		exp, s = e, s[:i]
	}
	whole, frac, _ := strings.Cut(s, ".")
	digits := strings.TrimLeft(whole+frac, "0")
	if digits == "" {
		return "0", true
	}
	significant := strings.TrimRight(digits, "0")
	exp += len(digits) - len(significant) - len(frac)
	return sign + significant + "e" + strconv.Itoa(exp), true
}

// escapePointerToken escapes one JSON pointer reference token.
func escapePointerToken(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}
