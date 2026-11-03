package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpreceiver"
)

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// compact renders v as one-line JSON with string values over limit
// characters clipped.
func compact(v any, limit int) string {
	b, err := json.Marshal(clipStrings(v, limit))
	if err != nil {
		return fmt.Sprint(v)
	}
	return string(b)
}

func clipStrings(v any, limit int) any {
	switch x := v.(type) {
	case string:
		if len([]rune(x)) > limit {
			return string([]rune(x)[:limit]) + "…"
		}
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = clipStrings(e, limit)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = clipStrings(e, limit)
		}
		return out
	}
	return v
}

// compactRaw is compact for a raw JSON document.
func compactRaw(raw []byte, limit int) string {
	var v any
	if json.Unmarshal(raw, &v) != nil {
		return clip(strings.TrimSpace(string(raw)), limit)
	}
	return compact(v, limit)
}

func stamp() string { return time.Now().Format("15:04:05") }

func stderrf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "%s %s\n", stamp(), fmt.Sprintf(format, args...))
}

// describe renders one request the receiver got, as one line plus the body
// of events.
func describe(q mcpreceiver.Request, secrets []string) string {
	id := q.Header.Get("webhook-id")
	sub := q.Header.Get("X-MCP-Subscription-Id")
	sig := "signature unchecked"
	if len(secrets) > 0 {
		sig = "signature BAD"
		for _, ok := range q.VerifiedWith(secrets...) {
			if ok {
				sig = "signature ok"
			}
		}
	}
	env := q.Envelope()
	switch q.Kind() {
	case "verification":
		answer := "echoed challenge"
		if sig == "signature BAD" {
			answer = "401"
		}
		return fmt.Sprintf("verification  webhook-id=%s  sub=%s  %s  -> %s", id, sub, sig, answer)
	case "event":
		return fmt.Sprintf("event %s  eventId=%s  webhook-id=%s  sub=%s  %s\n%s", env.Name, env.EventID, id, sub, sig, compactRaw(env.Data, 80))
	case "gap":
		return fmt.Sprintf("gap  cursor=%s  webhook-id=%s  sub=%s  %s", env.Cursor, id, sub, sig)
	case "terminated":
		return fmt.Sprintf("terminated  error=%s  webhook-id=%s  sub=%s  %s", env.Error, id, sub, sig)
	case "control":
		return fmt.Sprintf("control %s  webhook-id=%s  sub=%s  %s", env.Type, id, sub, sig)
	}
	return fmt.Sprintf("invalid body  %s", clip(string(q.Body), 80))
}
