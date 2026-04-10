package opevents

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const signatureHeader = "X-Outpost-Signature"

// HTTPSink sends operation events via HTTP POST with optional HMAC-SHA256 signing.
type HTTPSink struct {
	url           string
	signingSecret string
	client        *http.Client
}

// NewHTTPSink creates an HTTP sink. If signingSecret is non-empty, each request
// body is signed with HMAC-SHA256 and the signature is sent in the
// X-Outpost-Signature header.
func NewHTTPSink(url, signingSecret string) *HTTPSink {
	return &HTTPSink{
		url:           url,
		signingSecret: signingSecret,
		client:        &http.Client{Timeout: 30 * time.Second},
	}
}

func (s *HTTPSink) Init(ctx context.Context) error { return nil }
func (s *HTTPSink) Close() error                   { return nil }

func (s *HTTPSink) Send(ctx context.Context, event *OperationEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("opevents: failed to marshal event: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("opevents: failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	if s.signingSecret != "" {
		mac := hmac.New(sha256.New, []byte(s.signingSecret))
		mac.Write(body)
		sig := hex.EncodeToString(mac.Sum(nil))
		req.Header.Set(signatureHeader, "v0="+sig)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("opevents: failed to send event: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("opevents: HTTP sink returned status %d: %s", resp.StatusCode, string(snippet))
	}

	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}
