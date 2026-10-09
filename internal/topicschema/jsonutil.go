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

// escapePointerToken escapes one JSON pointer reference token.
func escapePointerToken(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}
