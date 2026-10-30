package mcpcheck

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpclient"
)

func (r *runner) discover() {
	res, _, err := r.c.Discover(r.ctx)
	switch probs := validateExtension(res); {
	case err != nil:
		r.fail("discover.extension", "server/discover: %v", err)
	case len(probs) > 0:
		r.fail("discover.extension", "%s", strings.Join(probs, "; "))
	default:
		r.pass("discover.extension", "")
	}
}

func (r *runner) list() bool {
	var all []mcpclient.Event
	var rawEvents []json.RawMessage
	cursor := ""
	seen := map[string]bool{}
	var dups []string
	pages := 0
	for {
		pages++
		resp, err := r.c.EventsListPage(r.ctx, cursor)
		if err != nil {
			r.fail("list.result", "events/list: %v", err)
			r.blocked = "events/list failed"
			return false
		}
		if resp.Error != nil || resp.Status != http.StatusOK {
			r.fail("list.result", "events/list: http %d %s", resp.Status, snippet(resp.Raw))
			r.blocked = "events/list failed"
			return false
		}
		obj, _ := parseObject(resp.Result)
		var evs []json.RawMessage
		_, isArr := obj.is("events", "array")
		if obj.str("resultType") != "complete" || !isArr {
			r.fail("list.result", "want resultType complete and an events array: %s", snippet(resp.Result))
			r.blocked = "events/list failed"
			return false
		}
		_ = json.Unmarshal(obj["events"], &evs)
		var page mcpclient.ListResult
		_ = json.Unmarshal(resp.Result, &page)
		for _, e := range page.Events {
			if seen[e.Name] {
				dups = append(dups, e.Name)
			}
			seen[e.Name] = true
		}
		all = append(all, page.Events...)
		rawEvents = append(rawEvents, evs...)
		if page.NextCursor == nil || *page.NextCursor == "" {
			break
		}
		if pages >= maxListPages {
			r.fail("list.pagination", "still paging after %d pages", pages)
			break
		}
		cursor = *page.NextCursor
	}
	r.pass("list.result", "%d events", len(all))
	var probs []string
	for _, raw := range rawEvents {
		probs = append(probs, validateDescriptor(raw)...)
	}
	if len(probs) > 0 {
		r.fail("list.descriptors", "%s", strings.Join(probs, "; "))
	} else {
		r.pass("list.descriptors", "")
	}
	if len(dups) > 0 {
		r.fail("list.pagination", "duplicate names across pages: %v", dups)
	} else {
		r.pass("list.pagination", "%d page(s)", pages)
	}
	return r.chooseEvent(all)
}

func (r *runner) chooseEvent(all []mcpclient.Event) bool {
	ev, err := ChooseEvent(all, r.o.Event)
	switch {
	case errors.Is(err, ErrNoWebhookEvent):
		r.skip("list.event", "%v", err)
		r.blocked = "no event offers webhook delivery"
		return false
	case err != nil:
		r.fail("list.event", "%v", err)
		r.blocked = "no usable event"
		return false
	}
	r.event = ev
	r.pass("list.event", "%s", ev.Name)
	return true
}
