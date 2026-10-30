package mcpcatalog

import (
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
)

// Change is one difference between two catalogs. Path is empty when a whole
// event was added or removed.
type Change struct {
	Event string
	// Kind is "added" (only in b), "removed" (only in a) or "changed".
	Kind     string
	Path     string
	From, To any
}

// DefaultIgnore lists the fields Diff skips unless told otherwise: delivery
// differs between a server's list and an export of it.
var DefaultIgnore = []string{"delivery"}

// Diff compares two catalogs by event name, ignoring order and the named
// top-level fields. Output is sorted by event, then path.
func Diff(a, b []Event, ignore []string) ([]Change, error) {
	am, err := byName(a)
	if err != nil {
		return nil, err
	}
	bm, err := byName(b)
	if err != nil {
		return nil, err
	}
	skip := map[string]bool{}
	for _, f := range ignore {
		skip[f] = true
	}
	names := map[string]bool{}
	for n := range am {
		names[n] = true
	}
	for n := range bm {
		names[n] = true
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)
	var out []Change
	for _, n := range sorted {
		ea, inA := am[n]
		eb, inB := bm[n]
		switch {
		case !inB:
			out = append(out, Change{Event: n, Kind: "removed"})
		case !inA:
			out = append(out, Change{Event: n, Kind: "added"})
		default:
			walk(&out, n, "", stripped(ea, skip), stripped(eb, skip))
		}
	}
	return out, nil
}

func byName(events []Event) (map[string]Event, error) {
	m := map[string]Event{}
	for i, e := range events {
		name, err := eventName(e)
		if err != nil {
			return nil, fmt.Errorf("events[%d]: %w", i, err)
		}
		if _, dup := m[name]; dup {
			return nil, fmt.Errorf("duplicate event name %q", name)
		}
		m[name] = e
	}
	return m, nil
}

func stripped(e Event, skip map[string]bool) map[string]any {
	out := make(map[string]any, len(e))
	for k, v := range e {
		if !skip[k] {
			out[k] = v
		}
	}
	return out
}

var plainKey = regexp.MustCompile(`^[A-Za-z0-9_$-]+$`)

func join(path, key string) string {
	switch {
	case !plainKey.MatchString(key):
		return path + "[" + strconv.Quote(key) + "]"
	case path == "":
		return key
	}
	return path + "." + key
}

func walk(out *[]Change, event, path string, a, b any) {
	switch x := a.(type) {
	case map[string]any:
		if y, ok := b.(map[string]any); ok {
			keys := map[string]bool{}
			for k := range x {
				keys[k] = true
			}
			for k := range y {
				keys[k] = true
			}
			sorted := make([]string, 0, len(keys))
			for k := range keys {
				sorted = append(sorted, k)
			}
			sort.Strings(sorted)
			for _, k := range sorted {
				walk(out, event, join(path, k), x[k], y[k])
			}
			return
		}
	case []any:
		if y, ok := b.([]any); ok {
			for i := 0; i < max(len(x), len(y)); i++ {
				var ai, bi any
				if i < len(x) {
					ai = x[i]
				}
				if i < len(y) {
					bi = y[i]
				}
				walk(out, event, path+"["+strconv.Itoa(i)+"]", ai, bi)
			}
			return
		}
	}
	if !reflect.DeepEqual(a, b) {
		*out = append(*out, Change{Event: event, Kind: "changed", Path: path, From: a, To: b})
	}
}

func eventName(e Event) (string, error) {
	name, _ := e["name"].(string)
	if name == "" {
		return "", fmt.Errorf("event without a name")
	}
	return name, nil
}
