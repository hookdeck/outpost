package mcpcatalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpclient"
	"gopkg.in/yaml.v3"
)

// Event is one entry of an events/list result, kept as generic JSON so fields
// the typed client doesn't know about are compared too.
type Event = map[string]any

const maxPages = 100

// Decode reads an event list from JSON or YAML: {"events": [...]}, a bare
// array, or a JSON-RPC response wrapping either. A list with a nextCursor is
// only a page and is rejected.
func Decode(data []byte) ([]Event, error) {
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		var y any
		if yerr := yaml.Unmarshal(data, &y); yerr != nil {
			return nil, fmt.Errorf("not JSON or YAML: %w", err)
		}
		b, err := json.Marshal(y)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			return nil, err
		}
	}
	if m, ok := doc.(map[string]any); ok {
		if r, ok := m["result"].(map[string]any); ok {
			m = r
		}
		if c, ok := m["nextCursor"].(string); ok && c != "" {
			return nil, errors.New("partial list (nextCursor is set): merge every page first")
		}
		doc = m["events"]
	}
	list, ok := doc.([]any)
	if !ok {
		return nil, errors.New(`want an events/list result: {"events": [...]}`)
	}
	events := make([]Event, len(list))
	for i, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("events[%d] is not an object", i)
		}
		events[i] = m
	}
	return events, nil
}

func LoadFile(path string) ([]Event, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	events, err := Decode(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return events, nil
}

// FromServer lists every page of events/list.
func FromServer(ctx context.Context, c *mcpclient.Client) ([]Event, error) {
	var out []Event
	cursor := ""
	for page := 0; page < maxPages; page++ {
		resp, err := c.EventsListPage(ctx, cursor)
		if err != nil {
			return nil, err
		}
		var res struct {
			Events     []Event `json:"events"`
			NextCursor *string `json:"nextCursor"`
		}
		if err := resp.Decode(&res); err != nil {
			return nil, fmt.Errorf("events/list: %w", err)
		}
		out = append(out, res.Events...)
		if res.NextCursor == nil || *res.NextCursor == "" {
			return out, nil
		}
		cursor = *res.NextCursor
	}
	return nil, fmt.Errorf("events/list: more than %d pages", maxPages)
}
