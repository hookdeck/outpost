// Package mcpcatalog builds sample payloads from the schemas of MCP Events
// event catalogs.
package mcpcatalog

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

const maxDepth = 16

// Sample builds the i-th example value for a JSON Schema, deterministically:
// the same (schema, i, now) gives the same value and different i give
// different values where the schema allows. It uses const, enum, examples,
// default, oneOf/anyOf, allOf, then the type. Objects get every declared
// property. pattern is not honoured; give such properties examples.
func Sample(schema map[string]any, i int, now time.Time) any {
	g := &sampler{root: schema, i: i, now: now}
	return g.value(schema, "", 0, nil)
}

type sampler struct {
	root map[string]any
	i    int
	now  time.Time
}

func (g *sampler) value(s map[string]any, prop string, depth int, seen []string) any {
	if depth > maxDepth {
		return nil
	}
	if r, ok := s["$ref"].(string); ok {
		for _, p := range seen {
			if p == r {
				return nil
			}
		}
		target, ok := g.resolve(r)
		if !ok {
			return nil
		}
		return g.value(target, prop, depth+1, append(seen, r))
	}
	if v, ok := s["const"]; ok {
		return v
	}
	if e, ok := s["enum"].([]any); ok && len(e) > 0 {
		return e[g.i%len(e)]
	}
	if e, ok := s["examples"].([]any); ok && len(e) > 0 {
		// An object's own examples seed the first value; later ones are
		// built from its properties so they differ.
		if _, props := s["properties"].(map[string]any); !props || g.i == 0 {
			return e[g.i%len(e)]
		}
	}
	if v, ok := s["default"]; ok {
		return v
	}
	for _, k := range []string{"oneOf", "anyOf"} {
		if alts, ok := s[k].([]any); ok && len(alts) > 0 {
			if alt, ok := alts[g.i%len(alts)].(map[string]any); ok {
				return g.value(alt, prop, depth+1, seen)
			}
		}
	}
	if parts, ok := s["allOf"].([]any); ok && len(parts) > 0 {
		merged := map[string]any{}
		for k, v := range s {
			if k != "allOf" {
				merged[k] = v
			}
		}
		for _, p := range parts {
			if pm, ok := p.(map[string]any); ok {
				if r, ok := pm["$ref"].(string); ok {
					if t, ok := g.resolve(r); ok {
						pm = t
					}
				}
				mergeSchema(merged, pm)
			}
		}
		return g.value(merged, prop, depth+1, seen)
	}
	switch typeOf(s) {
	case "object":
		return g.object(s, prop, depth, seen)
	case "array":
		return g.array(s, prop, depth, seen)
	case "string":
		return g.str(s, prop)
	case "integer":
		return g.number(s, prop, false)
	case "number":
		return g.number(s, prop, true)
	case "boolean":
		return g.i%2 == 0
	}
	return nil
}

func mergeSchema(dst, src map[string]any) {
	for k, v := range src {
		if k == "properties" {
			props, _ := dst[k].(map[string]any)
			merged := map[string]any{}
			for pk, pv := range props {
				merged[pk] = pv
			}
			for pk, pv := range v.(map[string]any) {
				merged[pk] = pv
			}
			dst[k] = merged
			continue
		}
		if _, ok := dst[k]; !ok {
			dst[k] = v
		}
	}
}

func typeOf(s map[string]any) string {
	switch t := s["type"].(type) {
	case string:
		return t
	case []any:
		for _, e := range t {
			if str, _ := e.(string); str != "null" && str != "" {
				return str
			}
		}
	}
	if _, ok := s["properties"]; ok {
		return "object"
	}
	if _, ok := s["items"]; ok {
		return "array"
	}
	return ""
}

func (g *sampler) resolve(ref string) (map[string]any, bool) {
	if ref == "#" {
		return g.root, true
	}
	if !strings.HasPrefix(ref, "#/") {
		return nil, false
	}
	var cur any = g.root
	for _, seg := range strings.Split(ref[2:], "/") {
		seg = strings.NewReplacer("~1", "/", "~0", "~").Replace(seg)
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur = m[seg]
	}
	m, ok := cur.(map[string]any)
	return m, ok
}

func (g *sampler) object(s map[string]any, prop string, depth int, seen []string) any {
	props, _ := s["properties"].(map[string]any)
	if len(props) == 0 {
		if e, ok := s["examples"].([]any); ok && len(e) > 0 {
			return e[0]
		}
		return map[string]any{}
	}
	names := make([]string, 0, len(props))
	for k := range props {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make(map[string]any, len(props))
	for _, k := range names {
		ps, _ := props[k].(map[string]any)
		if ps == nil {
			continue
		}
		if v := g.value(ps, k, depth+1, seen); v != nil || nullable(ps) {
			out[k] = v
		}
	}
	return out
}

func nullable(s map[string]any) bool {
	t, ok := s["type"].([]any)
	if !ok {
		return s["type"] == "null"
	}
	for _, e := range t {
		if e == "null" {
			return true
		}
	}
	return false
}

func (g *sampler) array(s map[string]any, prop string, depth int, seen []string) any {
	n := 1 + g.i%3
	if v, ok := num(s["minItems"]); ok && n < int(v) {
		n = int(v)
	}
	if v, ok := num(s["maxItems"]); ok && n > int(v) {
		n = int(v)
	}
	items, _ := s["items"].(map[string]any)
	out := make([]any, 0, n)
	if items == nil {
		return out
	}
	for k := 0; k < n; k++ {
		sub := &sampler{root: g.root, i: g.i + k, now: g.now}
		out = append(out, sub.value(items, prop, depth+1, seen))
	}
	return out
}

func (g *sampler) str(s map[string]any, prop string) string {
	n := g.i + 1
	var v string
	switch s["format"] {
	case "date-time":
		return g.now.UTC().Add(-time.Duration(g.i) * time.Minute).Truncate(time.Second).Format(time.RFC3339)
	case "date":
		return g.now.UTC().AddDate(0, 0, -g.i).Format("2006-01-02")
	case "email":
		v = fmt.Sprintf("user%d@example.com", n)
	case "uri", "url":
		v = fmt.Sprintf("https://example.com/%s/%d", prop, n)
	case "uuid":
		h := sha256.Sum256([]byte(fmt.Sprintf("%s/%d", prop, g.i)))
		x := hex.EncodeToString(h[:16])
		v = x[0:8] + "-" + x[8:12] + "-4" + x[13:16] + "-8" + x[17:20] + "-" + x[20:32]
	default:
		v = fmt.Sprintf("%s-%d", prop, n)
		if prop == "" {
			v = fmt.Sprintf("value-%d", n)
		}
	}
	if m, ok := num(s["maxLength"]); ok && len(v) > int(m) {
		v = v[:int(m)]
	}
	if m, ok := num(s["minLength"]); ok && len(v) < int(m) {
		v += strings.Repeat("x", int(m)-len(v))
	}
	return v
}

func (g *sampler) number(s map[string]any, prop string, float bool) any {
	lo, ok := num(s["minimum"])
	if !ok {
		lo = 1
	}
	v := lo + float64(g.i*7+len(prop))
	if float {
		v += 0.5
	}
	if hi, ok := num(s["maximum"]); ok && v > hi {
		v = hi
	}
	if float {
		return v
	}
	return int64(v)
}

func num(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}
