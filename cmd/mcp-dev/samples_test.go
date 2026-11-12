package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSamplesHandler(t *testing.T) {
	srv := httptest.NewServer(samplesHandler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/samples/events.json")
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	assert.Equal(t, sampleEventsJSON(), body)
}

func TestServeKeepsMCPEndpointOutsideSamples(t *testing.T) {
	// serve mounts the sample for GET only; everything else reaches the MCP handler.
	mux := http.NewServeMux()
	mux.Handle("GET /samples/events.json", samplesHandler())
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("mcp")) }))
	srv := httptest.NewServer(mux)
	defer srv.Close()
	resp, err := http.Post(srv.URL+"/", "application/json", strings.NewReader("{}"))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode)
	resp, err = http.Post(srv.URL+"/samples/events.json", "application/json", strings.NewReader("{}"))
	require.NoError(t, err)
	resp.Body.Close()
	assert.Equal(t, 200, resp.StatusCode, "POST is not diverted")
}

func TestSamplesCommandWritesFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out")
	require.NoError(t, runQuiet(t, "samples", "--out", dir))
	b, err := os.ReadFile(filepath.Join(dir, "events.json"))
	require.NoError(t, err)
	assert.Equal(t, sampleEventsJSON(), b)
}

type publishedEvent struct {
	Tenant, Topic string
	Data          map[string]any
	Auth, Extra   string
}

type fakeAPI struct {
	*httptest.Server
	mu        sync.Mutex
	published []publishedEvent
	matched   []string
	failTopic string
	created   []map[string]any
	deleted   []string
	tenants   []string
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.Method == "POST" && r.URL.Path == "/publish":
			var b struct {
				Tenant string         `json:"tenant_id"`
				Topic  string         `json:"topic"`
				Data   map[string]any `json:"data"`
			}
			_ = json.NewDecoder(r.Body).Decode(&b)
			if b.Topic == f.failTopic {
				http.Error(w, `{"message":"rejected"}`, 422)
				return
			}
			f.published = append(f.published, publishedEvent{b.Tenant, b.Topic, b.Data, r.Header.Get("Authorization"), r.Header.Get("X-Extra")})
			w.WriteHeader(202)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "evt_" + b.Topic, "duplicate": false, "destination_ids": f.matched})
		case r.Method == "PUT" && strings.HasPrefix(r.URL.Path, "/tenants/"):
			f.tenants = append(f.tenants, r.URL.Path)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": strings.TrimPrefix(r.URL.Path, "/tenants/")})
		case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/destinations"):
			var b map[string]any
			_ = json.NewDecoder(r.Body).Decode(&b)
			f.created = append(f.created, b)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "des_1"})
		case r.Method == "DELETE":
			f.deleted = append(f.deleted, r.URL.Path)
			w.WriteHeader(204)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func runCLI(t *testing.T, args ...string) error {
	t.Helper()
	return newApp().Run(context.Background(), append([]string{"mcp-dev"}, args...))
}

func runQuiet(t *testing.T, args ...string) (err error) {
	t.Helper()
	captureStdout(t, func() { err = runCLI(t, args...) })
	return err
}

func exitCode(err error) int {
	var e exitError
	var u usageError
	switch {
	case err == nil:
		return 0
	case asErr(err, &e):
		return e.code
	case asErr(err, &u):
		return 2
	}
	return 1
}

func asErr[T error](err error, target *T) bool {
	for err != nil {
		if e, ok := err.(T); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func TestPublishSendsSampleEvents(t *testing.T) {
	f := newFakeAPI(t)
	f.matched = []string{"des_x"}
	err := runQuiet(t, "publish", "--outpost-url", f.URL, "--api-key", "k", "--outpost-header", "X-Extra: 1", "--tenant", "acme", "--count", "2", "--event", "user.created", "--event", "order.created")
	require.NoError(t, err)
	require.Len(t, f.published, 4)
	for _, p := range f.published {
		assert.Equal(t, "acme", p.Tenant)
		assert.Equal(t, "Bearer k", p.Auth)
		assert.Equal(t, "1", p.Extra)
		assert.Contains(t, []string{"user.created", "order.created"}, p.Topic)
		assert.NotEmpty(t, p.Data)
	}
	assert.Equal(t, "user.created", f.published[0].Topic, "--event order is kept")
}

func TestPublishWithoutMatchWarns(t *testing.T) {
	f := newFakeAPI(t)
	out := captureStdout(t, func() {
		require.NoError(t, runCLI(t, "publish", "--outpost-url", f.URL, "--count", "1", "--event", "user.created"))
	})
	assert.Contains(t, out, "warning: no destination matched user.created")
	assert.Contains(t, out, "--sink")
}

func TestPublishFailureExits1AndContinues(t *testing.T) {
	f := newFakeAPI(t)
	f.failTopic = "order.created"
	err := runQuiet(t, "publish", "--outpost-url", f.URL, "--count", "1")
	assert.Equal(t, 1, exitCode(err))
	assert.Len(t, f.published, 4, "the other four events still went out")
}

func TestPublishDryRunAndUsage(t *testing.T) {
	f := newFakeAPI(t)
	require.NoError(t, runQuiet(t, "publish", "--outpost-url", f.URL, "--dry-run", "--sink"))
	assert.Empty(t, f.published)
	assert.Empty(t, f.created, "dry run creates no sink")
	for _, args := range [][]string{
		{"publish", "--event", "nope"},
		{"publish", "--count", "0"},
		{"publish", "--file", "a", "--server", "b"},
	} {
		assert.Equal(t, 2, exitCode(runQuiet(t, args...)), "%v", args)
	}
}

func TestPublishFromFile(t *testing.T) {
	f := newFakeAPI(t)
	path := filepath.Join(t.TempDir(), "list.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"events":[{"name":"x.y","payloadSchema":{"type":"object","properties":{"a":{"type":"string"}}}},{"name":"no.schema"}]}`), 0o644))
	require.NoError(t, runQuiet(t, "publish", "--outpost-url", f.URL, "--file", path, "--count", "1"))
	require.Len(t, f.published, 1)
	assert.Equal(t, "x.y", f.published[0].Topic)
	assert.Equal(t, "a-1", f.published[0].Data["a"])
}

func TestPublishSinkCreatesAndDeletesDestination(t *testing.T) {
	f := newFakeAPI(t)
	sinkWait = 100 * time.Millisecond
	out := captureStdout(t, func() {
		require.NoError(t, runCLI(t, "publish", "--outpost-url", f.URL, "--count", "1", "--event", "user.created",
			"--sink", "--sink-listen", "127.0.0.1:0", "--sink-url", "http://127.0.0.1:1/"))
	})
	assert.Equal(t, []string{"/tenants/demo"}, f.tenants)
	require.Len(t, f.created, 1)
	assert.Equal(t, "webhook", f.created[0]["type"])
	assert.Equal(t, []any{"user.created"}, f.created[0]["topics"])
	assert.Equal(t, []string{"/tenants/demo/destinations/des_1"}, f.deleted)
	assert.Contains(t, out, "deleted sink destination des_1")
}

func TestDiffExitCodes(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.json")
	b := filepath.Join(dir, "b.json")
	require.NoError(t, os.WriteFile(a, sampleEventsJSON(), 0o644))
	require.NoError(t, os.WriteFile(b, sampleEventsJSON(), 0o644))
	var err error
	out := captureStdout(t, func() { err = runCLI(t, "diff", a, b) })
	assert.Equal(t, 0, exitCode(err))
	assert.Contains(t, out, "equal: 5 events")

	changed := strings.Replace(string(sampleEventsJSON()), `"P3"`, `"P4"`, 1)
	require.NoError(t, os.WriteFile(b, []byte(changed), 0o644))
	out = captureStdout(t, func() { err = runCLI(t, "diff", a, b) })
	assert.Equal(t, 1, exitCode(err))
	assert.Contains(t, out, "~ incident.created")
	assert.Contains(t, out, `"P3" -> "P4"`)

	assert.Equal(t, 2, exitCode(runCLI(t, "diff", a)))
	assert.Equal(t, 2, exitCode(runCLI(t, "diff", a, "missing.json")))
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	done := make(chan string)
	go func() {
		b, _ := readAll(r)
		done <- string(b)
	}()
	fn()
	w.Close()
	os.Stdout = old
	return <-done
}

func readAll(r *os.File) ([]byte, error) {
	var out []byte
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			return out, nil
		}
	}
}
