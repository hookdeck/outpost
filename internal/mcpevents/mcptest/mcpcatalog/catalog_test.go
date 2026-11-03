package mcpcatalog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecode(t *testing.T) {
	list := `{"events":[{"name":"a","n":1}]}`
	for name, in := range map[string]string{
		"plain":       list,
		"with type":   `{"resultType":"complete","nextCursor":null,"events":[{"name":"a","n":1}]}`,
		"json-rpc":    `{"jsonrpc":"2.0","id":1,"result":` + list + `}`,
		"bare array":  `[{"name":"a","n":1}]`,
		"yaml":        "events:\n  - name: a\n    n: 1\n",
		"yaml in rpc": "result:\n  events:\n    - {name: a, n: 1}\n",
	} {
		events, err := Decode([]byte(in))
		require.NoError(t, err, name)
		require.Len(t, events, 1, name)
		assert.Equal(t, "a", events[0]["name"], name)
		assert.EqualValues(t, 1, events[0]["n"], name)
	}
	_, err := Decode([]byte(`{"nextCursor":"x","events":[]}`))
	assert.ErrorContains(t, err, "partial list")
	for _, bad := range []string{`{"foo":1}`, `"x"`, `{"events":[1]}`, `{`} {
		_, err := Decode([]byte(bad))
		assert.Error(t, err, bad)
	}
}

func TestYAMLAndJSONNumbersCompareEqual(t *testing.T) {
	a, err := Decode([]byte("events:\n  - {name: a, max: 5}\n"))
	require.NoError(t, err)
	b, err := Decode([]byte(`{"events":[{"name":"a","max":5}]}`))
	require.NoError(t, err)
	changes, err := Diff(a, b, nil)
	require.NoError(t, err)
	assert.Empty(t, changes)
}

func listServer(t *testing.T, pages int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Params struct {
				Cursor string `json:"cursor"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		page := 0
		if req.Params.Cursor != "" {
			fmt.Sscanf(req.Params.Cursor, "p%d", &page)
		}
		next := "null"
		if page+1 < pages {
			next = fmt.Sprintf(`"p%d"`, page+1)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":%s,"result":{"resultType":"complete","events":[{"name":"e%d","delivery":["webhook"]}],"nextCursor":%s}}`, req.ID, page, next)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFromServerFollowsPages(t *testing.T) {
	srv := listServer(t, 3)
	events, err := FromServer(context.Background(), mcpclient.New(srv.URL, "p", mcpclient.Stateless()))
	require.NoError(t, err)
	require.Len(t, events, 3)
	assert.Equal(t, "e0", events[0]["name"])
	assert.Equal(t, "e2", events[2]["name"])
}

func TestFromServerStopsAfterTooManyPages(t *testing.T) {
	srv := listServer(t, maxPages+5)
	_, err := FromServer(context.Background(), mcpclient.New(srv.URL, "p", mcpclient.Stateless()))
	assert.ErrorContains(t, err, "pages")
}

func TestSampleEventsFile(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "cmd", "mcp-dev", "samples", "events.json"))
	require.NoError(t, err)
	events, err := Decode(data)
	require.NoError(t, err)
	require.NotEmpty(t, events)
	now := time.Now()
	for _, e := range events {
		schema := e["payloadSchema"].(map[string]any)
		s := compileSchema(t, schema)
		first := roundTrip(t, Sample(schema, 0, now))
		for i := 0; i < 5; i++ {
			v := roundTrip(t, Sample(schema, i, now))
			assert.NoError(t, s.Validate(v), "%v i=%d", e["name"], i)
			if i == 1 {
				assert.NotEqual(t, first, v, "%v: i=0 and i=1 differ", e["name"])
			}
		}
	}
}

func ev(name string, extra map[string]any) Event {
	e := Event{"name": name}
	for k, v := range extra {
		e[k] = v
	}
	return e
}

func TestDiff(t *testing.T) {
	a := []Event{
		ev("a", map[string]any{"delivery": []any{"webhook"}, "payloadSchema": map[string]any{"enum": []any{"x", "y"}, "a.b": 1}}),
		ev("gone", nil),
	}
	b := []Event{
		ev("new", nil),
		ev("a", map[string]any{"delivery": []any{"poll"}, "payloadSchema": map[string]any{"enum": []any{"x", "y", "z"}, "a.b": 2}}),
	}
	changes, err := Diff(a, b, DefaultIgnore)
	require.NoError(t, err)
	var got []string
	for _, c := range changes {
		got = append(got, c.Kind+" "+c.Event+" "+c.Path)
	}
	assert.Equal(t, []string{
		`changed a payloadSchema["a.b"]`,
		"changed a payloadSchema.enum[2]",
		"removed gone ",
		"added new ",
	}, got)

	changes, err = Diff(a[:1], b[1:], nil)
	require.NoError(t, err)
	assert.Contains(t, fmt.Sprint(changes), "delivery[0]")

	changes, err = Diff([]Event{a[0], a[1]}, []Event{a[1], a[0]}, nil)
	require.NoError(t, err)
	assert.Empty(t, changes, "order does not matter")

	_, err = Diff([]Event{ev("d", nil), ev("d", nil)}, nil, nil)
	assert.Error(t, err)
}

func TestFromServerDecodesUnknownFields(t *testing.T) {
	srv := listServer(t, 1)
	events, err := FromServer(context.Background(), mcpclient.New(srv.URL, "p", mcpclient.Stateless()))
	require.NoError(t, err)
	assert.True(t, bytes.Contains([]byte(fmt.Sprint(events[0])), []byte("delivery")))
	assert.False(t, strings.Contains(fmt.Sprint(events), "resultType"))
}
