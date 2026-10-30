package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/urfave/cli/v3"
)

// conn is the connection to the Outpost API shared by serve, check and tour.
type conn struct {
	URL    string
	APIKey string
	Header http.Header
}

func outpostFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "outpost-url", Value: "http://localhost:3333/api/v1", Sources: cli.EnvVars("OUTPOST_URL"),
			Usage: "Outpost API base URL, including any path prefix"},
		&cli.StringFlag{Name: "api-key", Value: "apikey", Sources: cli.EnvVars("OUTPOST_API_KEY"),
			Usage: "sent as Authorization: Bearer"},
		&cli.StringSliceFlag{Name: "outpost-header", Sources: cli.EnvVars("OUTPOST_HEADERS"),
			Usage: "extra request header 'Name: value', repeatable (OUTPOST_HEADERS: comma-separated)"},
	}
}

func outpostConn(cmd *cli.Command) (conn, error) {
	c := conn{
		URL:    strings.TrimRight(cmd.String("outpost-url"), "/"),
		APIKey: cmd.String("api-key"),
		Header: http.Header{},
	}
	for _, h := range cmd.StringSlice("outpost-header") {
		name, value, err := parseHeader(h)
		if err != nil {
			return c, err
		}
		c.Header.Add(name, value)
	}
	return c, nil
}

// parseHeader splits "Name: value" at the first colon.
func parseHeader(s string) (string, string, error) {
	name, value, ok := strings.Cut(s, ":")
	name, value = strings.TrimSpace(name), strings.TrimSpace(value)
	if !ok || name == "" {
		return "", "", usagef("--outpost-header %q: want 'Name: value'", s)
	}
	return name, value, nil
}

type publishFunc func(ctx context.Context, event string, data map[string]any) (string, error)

// publishResult is Outpost's answer to /publish.
type publishResult struct {
	ID             string   `json:"id"`
	Duplicate      bool     `json:"duplicate"`
	DestinationIDs []string `json:"destination_ids"`
}

// publishOne publishes one event to Outpost's /publish for a tenant.
func (c conn) publishOne(ctx context.Context, tenant, topic string, data any) (publishResult, error) {
	var res publishResult
	body, err := json.Marshal(map[string]any{
		"tenant_id": tenant, "topic": topic, "data": data, "eligible_for_retry": true,
	})
	if err != nil {
		return res, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+"/publish", bytes.NewReader(body))
	if err != nil {
		return res, err
	}
	req.Header.Set("Content-Type", "application/json")
	out, status, err := c.do(req)
	if err != nil {
		return res, err
	}
	if status/100 != 2 {
		return res, fmt.Errorf("publish: http %d: %s", status, clip(string(out), 200))
	}
	_ = json.Unmarshal(out, &res)
	return res, nil
}

// do sends a request to Outpost with the API key and the extra headers.
func (c conn) do(req *http.Request) ([]byte, int, error) {
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	for k, vs := range c.Header {
		req.Header.Del(k)
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return out, resp.StatusCode, nil
}

// publisher publishes to Outpost's /publish for a tenant and returns the
// event ID.
func (c conn) publisher(tenant string) publishFunc {
	return func(ctx context.Context, event string, data map[string]any) (string, error) {
		res, err := c.publishOne(ctx, tenant, event, data)
		return res.ID, err
	}
}
