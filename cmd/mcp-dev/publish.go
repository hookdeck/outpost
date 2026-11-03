package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type publishFunc func(ctx context.Context, event string, data map[string]any) (string, error)

// publisher publishes to Outpost's /publish for a tenant and returns the
// event ID.
func (c conn) publisher(tenant string) publishFunc {
	hc := &http.Client{Timeout: 30 * time.Second}
	return func(ctx context.Context, event string, data map[string]any) (string, error) {
		body, err := json.Marshal(map[string]any{
			"tenant_id": tenant, "topic": event, "data": data, "eligible_for_retry": true,
		})
		if err != nil {
			return "", err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+"/publish", bytes.NewReader(body))
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Content-Type", "application/json")
		for k, vs := range c.Header {
			req.Header.Del(k)
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}
		resp, err := hc.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		out, _ := io.ReadAll(resp.Body)
		if resp.StatusCode/100 != 2 {
			return "", fmt.Errorf("publish: http %d: %s", resp.StatusCode, clip(string(out), 200))
		}
		var res struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(out, &res)
		return res.ID, nil
	}
}
