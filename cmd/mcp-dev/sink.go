package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"
)

// webhookSink is a temporary webhook destination on a tenant that answers 200
// to every delivery, so Outpost keeps the published events.
type webhookSink struct {
	c        conn
	tenant   string
	received atomic.Int64
	srv      *http.Server
	id       string
}

func startSink(ctx context.Context, c conn, tenant, listen, publicURL string, topics []string) (*webhookSink, error) {
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, fmt.Errorf("can't listen on %s: %w", listen, err)
	}
	s := &webhookSink{c: c, tenant: url.PathEscape(tenant)}
	s.srv = &http.Server{ReadHeaderTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		s.received.Add(1)
		w.WriteHeader(http.StatusOK)
	})}
	go func() { _ = s.srv.Serve(ln) }()
	// Create the tenant if it doesn't exist (PUT is an upsert).
	treq, err := http.NewRequestWithContext(ctx, http.MethodPut, c.URL+"/tenants/"+s.tenant, nil)
	if err != nil {
		s.srv.Close()
		return nil, err
	}
	if out, status, err := c.do(treq); err != nil || status/100 != 2 {
		s.srv.Close()
		if err == nil {
			err = fmt.Errorf("create tenant %s: http %d: %s", tenant, status, clip(string(out), 200))
		}
		return nil, err
	}
	body, _ := json.Marshal(map[string]any{
		"type": "webhook", "topics": topics, "config": map[string]any{"url": publicURL},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL+"/tenants/"+s.tenant+"/destinations", bytes.NewReader(body))
	if err != nil {
		s.srv.Close()
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	out, status, err := c.do(req)
	if err == nil && status/100 != 2 {
		err = fmt.Errorf("create sink destination: http %d: %s", status, clip(string(out), 200))
	}
	var created struct {
		ID string `json:"id"`
	}
	if err == nil {
		err = json.Unmarshal(out, &created)
	}
	if err != nil {
		s.srv.Close()
		return nil, err
	}
	s.id = created.ID
	return s, nil
}

// waitFor blocks until the sink got n deliveries or the timeout passes.
func (s *webhookSink) waitFor(ctx context.Context, n int64, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for s.received.Load() < n && time.Now().Before(deadline) && ctx.Err() == nil {
		time.Sleep(100 * time.Millisecond)
	}
}

// close deletes the destination and stops the listener.
func (s *webhookSink) close() error {
	defer s.srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.c.URL+"/tenants/"+s.tenant+"/destinations/"+s.id, nil)
	if err != nil {
		return err
	}
	out, status, err := s.c.do(req)
	if err == nil && status/100 != 2 {
		err = fmt.Errorf("delete sink destination %s: http %d: %s", s.id, status, clip(string(out), 200))
	}
	return err
}

// sinkWait is how long publish waits for the sink to see every delivery.
var sinkWait = 10 * time.Second
