package mcpcheck

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"time"

	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpcatalog"
	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpclient"
)

var (
	// ErrNoArgument means an event offers nothing to filter on.
	ErrNoArgument = errors.New("event has no scalar argument: pick one with --event or pass --arguments")
	// ErrNoWebhookEvent means no listed event offers webhook delivery.
	ErrNoWebhookEvent = errors.New("no listed event offers webhook delivery")
)

func offersWebhook(e mcpclient.Event) bool { return slices.Contains(e.Delivery, "webhook") }

// ChooseEvent returns the event called name, which must offer webhook
// delivery. Without a name it is the first webhook event with a filter
// argument, else the first webhook event.
func ChooseEvent(events []mcpclient.Event, name string) (mcpclient.Event, error) {
	if name != "" {
		i := slices.IndexFunc(events, func(e mcpclient.Event) bool { return e.Name == name })
		switch {
		case i < 0:
			return mcpclient.Event{}, fmt.Errorf("event %q is not listed", name)
		case !offersWebhook(events[i]):
			return mcpclient.Event{}, fmt.Errorf("event %q does not offer webhook delivery", name)
		}
		return events[i], nil
	}
	var first *mcpclient.Event
	for i, e := range events {
		if !offersWebhook(e) {
			continue
		}
		if HasFilterArgument(e) {
			return e, nil
		}
		if first == nil {
			first = &events[i]
		}
	}
	if first == nil {
		return mcpclient.Event{}, ErrNoWebhookEvent
	}
	return *first, nil
}

// FilterCaseResult is a matching and a non-matching case for one event:
// subscription arguments and the payloads published for them.
type FilterCaseResult struct {
	Field     string
	Match     map[string]any
	Other     map[string]any
	Data      map[string]any
	OtherData map[string]any
}

type scalarArg struct {
	enum  bool
	name  string
	match any
	other any
}

// scalarArgs lists the arguments of ev that take at least two scalar
// values, sorted by name.
func scalarArgs(ev mcpclient.Event) []scalarArg {
	props, _ := ev.InputSchema["properties"].(map[string]any)
	names := make([]string, 0, len(props))
	for n := range props {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []scalarArg
	for _, n := range names {
		p, _ := props[n].(map[string]any)
		if a, ok := argFromSchema(n, p); ok {
			out = append(out, a)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].enum && !out[j].enum })
	return out
}

func argFromSchema(name string, p map[string]any) (scalarArg, bool) {
	if enum, ok := p["enum"].([]any); ok {
		var vals []any
		for _, v := range enum {
			if isScalar(v) {
				vals = append(vals, v)
			}
		}
		if len(vals) >= 2 {
			return scalarArg{true, name, vals[0], vals[1]}, true
		}
		return scalarArg{}, false
	}
	if _, ok := p["const"]; ok {
		return scalarArg{}, false
	}
	switch p["type"] {
	case "string":
		return scalarArg{false, name, "mcp-check", "mcp-check-other"}, true
	case "number", "integer":
		return scalarArg{false, name, float64(1), float64(2)}, true
	case "boolean":
		return scalarArg{false, name, true, false}, true
	}
	return scalarArg{}, false
}

func isScalar(v any) bool {
	switch v.(type) {
	case string, bool, float64, int, int64, json.Number:
		return true
	}
	return false
}

// HasFilterArgument reports whether ev has an argument FilterCase can use.
func HasFilterArgument(ev mcpclient.Event) bool { return len(scalarArgs(ev)) > 0 }

// FilterCase derives the cases for ev. With args, Match is args and the
// first argument by name is the one varied; otherwise the first scalar
// argument is used, enums before other types, then by name. The payloads
// are samples of payloadSchema carrying the argument fields; data and
// otherData replace them when set. When the result can't be completed it
// is returned as far as it goes, with the error.
func FilterCase(ev mcpclient.Event, args, data, otherData map[string]any) (res FilterCaseResult, err error) {
	defer func() {
		if data != nil {
			res.Data = data
		}
		if otherData != nil {
			res.OtherData = otherData
		}
	}()
	var other any
	if len(args) > 0 {
		keys := make([]string, 0, len(args))
		for k := range args {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		res.Field = keys[0]
		res.Match = maps.Clone(args)
		if other, err = otherValue(ev, res.Field, args[res.Field]); err != nil {
			res.Data = payload(ev, res.Match)
			return res, err
		}
	} else {
		sa := scalarArgs(ev)
		if len(sa) == 0 {
			return res, ErrNoArgument
		}
		res.Field = sa[0].name
		res.Match = map[string]any{res.Field: sa[0].match}
		other = sa[0].other
	}
	res.Other = maps.Clone(res.Match)
	res.Other[res.Field] = other
	res.Data = payload(ev, res.Match)
	res.OtherData = payload(ev, res.Other)
	return res, nil
}

func otherValue(ev mcpclient.Event, field string, cur any) (any, error) {
	props, _ := ev.InputSchema["properties"].(map[string]any)
	p, _ := props[field].(map[string]any)
	if enum, ok := p["enum"].([]any); ok {
		for _, v := range enum {
			if isScalar(v) && fmt.Sprint(v) != fmt.Sprint(cur) {
				return v, nil
			}
		}
	}
	switch v := cur.(type) {
	case string:
		return v + "-other", nil
	case bool:
		return !v, nil
	case float64:
		return v + 1, nil
	case int:
		return v + 1, nil
	case int64:
		return v + 1, nil
	}
	return nil, fmt.Errorf("can't derive a non-matching value for argument %q: pass --other-data", field)
}

func payload(ev mcpclient.Event, args map[string]any) map[string]any {
	out := map[string]any{}
	if len(ev.PayloadSchema) > 0 {
		if m, ok := mcpcatalog.Sample(ev.PayloadSchema, 0, time.Now()).(map[string]any); ok {
			out = maps.Clone(m)
		}
	}
	maps.Copy(out, args)
	return out
}
