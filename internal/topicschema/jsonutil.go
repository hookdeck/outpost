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

// localRefPath returns the dot path from path, as visitSchemas writes them,
// of the value a JSON pointer reference resolves to in root, a tree from
// decodeJSON, following resolveLocalRef. It reports false when ref doesn't
// resolve.
func localRefPath(root any, ref, path string) (string, bool) {
	fragment, err := url.PathUnescape(strings.TrimPrefix(ref, "#"))
	if err != nil || (fragment != "" && fragment[0] != '/') {
		return "", false
	}
	cur := root
	for _, token := range strings.Split(fragment, "/")[1:] {
		token = strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~")
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[token]
			if !ok {
				return "", false
			}
			cur, path = next, appendPathKey(path, token)
		case []any:
			i, err := strconv.Atoi(token)
			if err != nil || i < 0 || i >= len(node) {
				return "", false
			}
			cur, path = node[i], indexPath(path, i)
		default:
			return "", false
		}
	}
	return path, true
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

// escapePointerToken escapes one JSON pointer reference token.
func escapePointerToken(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1")
}
