package mcpevents

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strconv"
	"unicode/utf8"
)

// maxCanonicalDepth bounds recursion for values that did not come through the
// strict decoder (which has its own, lower limit).
const maxCanonicalDepth = 512

var errCanonicalDepth = errors.New("mcpevents: value nested too deeply")

// CanonicalJSON serializes v as JSON with object keys sorted at every level,
// byte-identical to the MCP Events guide's canonicalJson (JSON.stringify per
// value):
//   - keys sort by UTF-16 code units, as JavaScript's Array.prototype.sort;
//   - strings escape only `"`, `\` and controls below U+0020 (U+2028/U+2029
//     and HTML characters stay raw); invalid UTF-8 becomes U+FFFD;
//   - numbers format as ECMAScript Number::toString (-0 is "0"); NaN and
//     infinities are an error, since JSON input can't produce them.
//
// Supported values: nil, bool, string, float64/float32, integers (converted to
// float64, as JavaScript would), json.Number, json.RawMessage, map[string]any
// and []any. Anything else round-trips through encoding/json first.
func CanonicalJSON(v any) ([]byte, error) {
	return appendCanonical(nil, v, 0)
}

// CanonicalizeJSON parses raw strictly (duplicate keys rejected) and returns
// its canonical form.
func CanonicalizeJSON(raw []byte) ([]byte, error) {
	v, err := decodeStrict(raw, maxCanonicalDepth)
	if err != nil {
		return nil, err
	}
	return appendCanonical(nil, v, 0)
}

func appendCanonical(b []byte, v any, depth int) ([]byte, error) {
	if depth > maxCanonicalDepth {
		return nil, errCanonicalDepth
	}
	switch v := v.(type) {
	case nil:
		return append(b, "null"...), nil
	case bool:
		if v {
			return append(b, "true"...), nil
		}
		return append(b, "false"...), nil
	case string:
		return appendJSString(b, v), nil
	case float64:
		return appendESNumber(b, v)
	case float32:
		return appendESNumber(b, float64(v))
	case int:
		return appendESNumber(b, float64(v))
	case int64:
		return appendESNumber(b, float64(v))
	case int32:
		return appendESNumber(b, float64(v))
	case uint64:
		return appendESNumber(b, float64(v))
	case uint32:
		return appendESNumber(b, float64(v))
	case uint:
		return appendESNumber(b, float64(v))
	case json.Number:
		f, err := parseJSONNumber(string(v))
		if err != nil {
			return nil, err
		}
		return appendESNumber(b, f)
	case json.RawMessage:
		parsed, err := decodeStrict(v, maxCanonicalDepth-depth)
		if err != nil {
			return nil, err
		}
		return appendCanonical(b, parsed, depth)
	case []any:
		b = append(b, '[')
		for i, e := range v {
			if i > 0 {
				b = append(b, ',')
			}
			var err error
			if b, err = appendCanonical(b, e, depth+1); err != nil {
				return nil, err
			}
		}
		return append(b, ']'), nil
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		slices.SortFunc(keys, compareUTF16)
		b = append(b, '{')
		for i, k := range keys {
			if i > 0 {
				b = append(b, ',')
				// Distinct keys only collide once invalid UTF-8 is
				// replaced, which JSON input can't produce.
				if compareUTF16(keys[i-1], k) == 0 {
					return nil, fmt.Errorf("mcpevents: duplicate object key after UTF-8 replacement")
				}
			}
			b = appendJSString(b, k)
			b = append(b, ':')
			var err error
			if b, err = appendCanonical(b, v[k], depth+1); err != nil {
				return nil, err
			}
		}
		return append(b, '}'), nil
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("mcpevents: canonical json: %w", err)
		}
		parsed, err := decodeStrict(raw, maxCanonicalDepth-depth)
		if err != nil {
			return nil, err
		}
		return appendCanonical(b, parsed, depth)
	}
}

// appendESNumber formats f as ECMAScript Number::toString does for finite
// values: shortest round-trip digits, fixed notation for 1e-6 <= |f| < 1e21,
// exponential (e+21, e-7) otherwise.
func appendESNumber(b []byte, f float64) ([]byte, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return nil, fmt.Errorf("mcpevents: unsupported number %v", f)
	}
	if f == 0 {
		return append(b, '0'), nil // also -0
	}
	format := byte('f')
	if abs := math.Abs(f); abs < 1e-6 || abs >= 1e21 {
		format = 'e'
	}
	b = strconv.AppendFloat(b, f, format, -1, 64)
	if format == 'e' {
		// strconv writes e-07; ECMAScript writes e-7.
		n := len(b)
		if n >= 4 && b[n-4] == 'e' && b[n-3] == '-' && b[n-2] == '0' {
			b[n-2] = b[n-1]
			b = b[:n-1]
		}
	}
	return b, nil
}

const hexDigits = "0123456789abcdef"

// appendJSString writes s as JSON.stringify does: only `"`, `\` and C0
// controls are escaped (short forms where JSON has them, \u00xx otherwise).
// Invalid UTF-8 bytes are written as U+FFFD, one per byte.
func appendJSString(b []byte, s string) []byte {
	b = append(b, '"')
	start := 0
	for i := 0; i < len(s); {
		c := s[i]
		if c < utf8.RuneSelf {
			if c >= 0x20 && c != '"' && c != '\\' {
				i++
				continue
			}
			b = append(b, s[start:i]...)
			switch c {
			case '"', '\\':
				b = append(b, '\\', c)
			case '\b':
				b = append(b, '\\', 'b')
			case '\f':
				b = append(b, '\\', 'f')
			case '\n':
				b = append(b, '\\', 'n')
			case '\r':
				b = append(b, '\\', 'r')
			case '\t':
				b = append(b, '\\', 't')
			default:
				b = append(b, '\\', 'u', '0', '0', hexDigits[c>>4], hexDigits[c&0xF])
			}
			i++
			start = i
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b = append(b, s[start:i]...)
			b = append(b, "�"...)
			i++
			start = i
			continue
		}
		i += size
	}
	b = append(b, s[start:]...)
	return append(b, '"')
}

// compareUTF16 orders strings by UTF-16 code units, as JavaScript's default
// sort does. It differs from byte (code point) order only between
// supplementary characters (surrogate pairs, 0xD800-0xDBFF lead unit) and
// BMP characters in U+E000..U+FFFF.
func compareUTF16(a, b string) int {
	for len(a) > 0 && len(b) > 0 {
		ra, sa := utf8.DecodeRuneInString(a)
		rb, sb := utf8.DecodeRuneInString(b)
		if ra != rb {
			ua, ub := firstUTF16Unit(ra), firstUTF16Unit(rb)
			if ua != ub {
				return int(ua) - int(ub)
			}
			// Same lead surrogate: the trail units order like the code points.
			if ra < rb {
				return -1
			}
			return 1
		}
		a, b = a[sa:], b[sb:]
	}
	return len(a) - len(b)
}

func firstUTF16Unit(r rune) rune {
	if r >= 0x10000 {
		return 0xD800 + ((r - 0x10000) >> 10)
	}
	return r
}

// parseJSONNumber converts a JSON number to float64 as JSON.parse does:
// overflow is an error (JavaScript would produce Infinity, which JSON can't
// carry back out), underflow rounds to zero.
func parseJSONNumber(s string) (float64, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		var numErr *strconv.NumError
		if errors.As(err, &numErr) && errors.Is(numErr.Err, strconv.ErrRange) && math.IsInf(f, 0) {
			return 0, fmt.Errorf("mcpevents: number %s is out of range", truncate(s, 32))
		}
		return 0, fmt.Errorf("mcpevents: invalid number")
	}
	return f, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	// Back off to a rune boundary.
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

// decodeStrict decodes one JSON value into nil, bool, string, json.Number,
// map[string]any and []any. Unlike encoding/json it rejects duplicate object
// keys (a JSON parser that kept the first occurrence would see a different
// value than one that kept the last), nesting deeper than maxDepth, and
// trailing data.
func decodeStrict(raw []byte, maxDepth int) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, err := decodeValue(dec, 0, maxDepth)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, errors.New("mcpevents: unexpected data after JSON value")
	}
	return v, nil
}

func decodeValue(dec *json.Decoder, depth, maxDepth int) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("mcpevents: invalid JSON: %w", err)
	}
	delim, ok := tok.(json.Delim)
	if !ok {
		return tok, nil
	}
	if depth >= maxDepth {
		return nil, errCanonicalDepth
	}
	switch delim {
	case '[':
		arr := []any{}
		for dec.More() {
			v, err := decodeValue(dec, depth+1, maxDepth)
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		if _, err := dec.Token(); err != nil {
			return nil, fmt.Errorf("mcpevents: invalid JSON: %w", err)
		}
		return arr, nil
	case '{':
		obj := map[string]any{}
		for dec.More() {
			tok, err := dec.Token()
			if err != nil {
				return nil, fmt.Errorf("mcpevents: invalid JSON: %w", err)
			}
			key, ok := tok.(string)
			if !ok {
				return nil, errors.New("mcpevents: invalid JSON object key")
			}
			if _, dup := obj[key]; dup {
				return nil, errDuplicateKey
			}
			v, err := decodeValue(dec, depth+1, maxDepth)
			if err != nil {
				return nil, err
			}
			obj[key] = v
		}
		if _, err := dec.Token(); err != nil {
			return nil, fmt.Errorf("mcpevents: invalid JSON: %w", err)
		}
		return obj, nil
	default:
		return nil, errors.New("mcpevents: invalid JSON")
	}
}

var errDuplicateKey = errors.New("mcpevents: duplicate object key")

// numbersToFloat replaces every json.Number in v (as produced by decodeStrict)
// with its float64 value, in place.
func numbersToFloat(v any) (any, error) {
	switch v := v.(type) {
	case json.Number:
		return parseJSONNumber(string(v))
	case []any:
		for i, e := range v {
			f, err := numbersToFloat(e)
			if err != nil {
				return nil, err
			}
			v[i] = f
		}
		return v, nil
	case map[string]any:
		for k, e := range v {
			f, err := numbersToFloat(e)
			if err != nil {
				return nil, err
			}
			v[k] = f
		}
		return v, nil
	default:
		return v, nil
	}
}
