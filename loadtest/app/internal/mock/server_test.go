package mock

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// post sends one delivery to the mock's handler through a mux that populates
// the path values the handler reads.
func post(t *testing.T, s *Server, group, tenant, dest string) {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle("POST /webhook/{group}/{tenant}/{dest}", s.Handler())

	r := httptest.NewRequest(http.MethodPost,
		"/webhook/"+group+"/"+tenant+"/"+dest, strings.NewReader("{}"))
	r.Header.Set("X-Outpost-Event-ID", "e1")
	mux.ServeHTTP(httptest.NewRecorder(), r)
}

// The receive path runs once per delivery, so a log line on it writes at the
// benchmark's full rate into a pipe that blocks when the collector throttles.
// A misrouted destination is the worst case: it keeps arriving for the whole
// run, so the warning has to be per route.
func TestUnregisteredRouteWarnsOncePerRoute(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	s := NewServer(nil)
	for range 100 {
		post(t, s, "ghost", "0", "0")
	}
	if got := strings.Count(buf.String(), "unregistered route"); got != 1 {
		t.Errorf("100 deliveries to one unregistered route logged %d times, want 1", got)
	}

	post(t, s, "ghost", "1", "0")
	if got := strings.Count(buf.String(), "unregistered route"); got != 2 {
		t.Errorf("a second unregistered route logged %d times total, want 2", got)
	}
}

// A registered route is the hot path proper: the whole point of removing the
// per-delivery line is that a normal run writes nothing per event.
func TestRegisteredDeliveriesAreSilent(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	got := 0
	s := NewServer(func(DeliveryRecord) { got++ })
	s.RegisterRoute("g", "0", "0", NewProfile(0, 0, 0))
	buf.Reset() // registration itself logs once, which is not per delivery

	for range 50 {
		post(t, s, "g", "0", "0")
	}
	if got != 50 {
		t.Fatalf("callback fired %d times, want 50 — deliveries were not counted", got)
	}
	if buf.Len() != 0 {
		t.Errorf("delivery path logged at debug:\n%s", buf.String())
	}
}
