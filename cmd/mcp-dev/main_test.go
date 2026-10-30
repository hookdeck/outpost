package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/mcpevents/mcptest/mcpclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

func TestParseHeader(t *testing.T) {
	for in, want := range map[string][2]string{
		"X-Debug: 1":              {"X-Debug", "1"},
		"X-A:b:c":                 {"X-A", "b:c"},
		"  Name  :  spaced value": {"Name", "spaced value"},
		"X-Empty:":                {"X-Empty", ""},
	} {
		name, value, err := parseHeader(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, [2]string{name, value}, in)
	}
	for _, in := range []string{"no colon", ": value", "  : v", ""} {
		_, _, err := parseHeader(in)
		var u usageError
		assert.ErrorAs(t, err, &u, in)
	}
}

func TestParsePrincipal(t *testing.T) {
	name, p, err := parsePrincipal("alice=t1:user.created,user.updated")
	require.NoError(t, err)
	assert.Equal(t, "alice", name)
	assert.Equal(t, "t1", p.Tenant)
	assert.Equal(t, []string{"user.created", "user.updated"}, p.AllowedTopics)
	_, p, err = parsePrincipal("bob=t2")
	require.NoError(t, err)
	assert.Empty(t, p.AllowedTopics)
	for _, bad := range []string{"alice", "=t1", "alice=", "alice=:a"} {
		_, _, err := parsePrincipal(bad)
		assert.Error(t, err, bad)
	}
}

func TestParseTTL(t *testing.T) {
	v, err := parseTTL("")
	require.NoError(t, err)
	assert.Nil(t, v)
	v, err = parseTTL("null")
	require.NoError(t, err)
	assert.Equal(t, mcpclient.TTLNull, v)
	v, err = parseTTL("5000")
	require.NoError(t, err)
	assert.Equal(t, int64(5000), v)
	_, err = parseTTL("soon")
	assert.Error(t, err)
}

func TestParseObject(t *testing.T) {
	m, err := parseObject("data", `{"a":1}`)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"a": float64(1)}, m)
	for _, bad := range []string{`[1]`, `"x"`, `null`, `{`} {
		_, err := parseObject("data", bad)
		var u usageError
		assert.ErrorAs(t, err, &u, bad)
	}
}

func TestOutpostHeadersFromEnvAreCommaSplit(t *testing.T) {
	t.Setenv("OUTPOST_HEADERS", "X-One: 1,X-Two: 2")
	t.Setenv("OUTPOST_URL", "http://proxy.example/prefix/api/v1/")
	var got conn
	app := &cli.Command{Name: "t", Flags: outpostFlags(), Action: func(_ context.Context, cmd *cli.Command) (err error) {
		got, err = outpostConn(cmd)
		return err
	}}
	require.NoError(t, app.Run(context.Background(), []string{"t"}))
	assert.Equal(t, "http://proxy.example/prefix/api/v1", got.URL)
	assert.Equal(t, "apikey", got.APIKey)
	assert.Equal(t, "1", got.Header.Get("X-One"))
	assert.Equal(t, "2", got.Header.Get("X-Two"))

	require.NoError(t, app.Run(context.Background(), []string{"t", "--outpost-header", "X-Three: 3", "--outpost-header", "X-Four: 4"}))
	assert.Equal(t, "3", got.Header.Get("X-Three"))
	assert.Equal(t, "4", got.Header.Get("X-Four"))
}

func TestBadFlagsAreUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"check", "--data", "[1]"},
		{"check", "--outpost-header", "nocolon"},
		{"check", "--profile", "other"},
		{"check", "--ttl-ms", "0"},
		{"client", "subscribe", "--event", "e", "--url", "u", "--ttl-ms", "soon"},
		{"client", "subscribe"},
		{"serve", "--principal", "bad"},
		{"receive", "--profile", "x"},
		{"receive", "--nope"},
	} {
		err := newApp().Run(context.Background(), append([]string{"mcp-dev"}, args...))
		var u usageError
		assert.ErrorAs(t, err, &u, "%v", args)
	}
}

func TestSampleEventsLoad(t *testing.T) {
	var schema config.MCPEventsSchema
	require.NoError(t, schema.UnmarshalText([]byte(filepath.Join("samples", "events.json"))))
	_, err := os.Stat(filepath.Join("samples", "events.json"))
	require.NoError(t, err)
}

func TestCompact(t *testing.T) {
	out := compact(map[string]any{"a": "short", "b": strings.Repeat("x", 100)}, 10)
	assert.Contains(t, out, `"a":"short"`)
	assert.Contains(t, out, "…")
}
