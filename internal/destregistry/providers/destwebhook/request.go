package destwebhook

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type WebhookRequest struct {
	URL          string
	Timestamp    int64
	RawBody      []byte
	Signatures   []string
	Metadata     map[string]string
	HeaderPrefix string
}

func NewWebhookRequest(url string, rawBody []byte, metadata map[string]string, headerPrefix string, secrets []WebhookSecret) *WebhookRequest {
	req := &WebhookRequest{
		URL:          url,
		Timestamp:    time.Now().Unix(),
		RawBody:      rawBody,
		Metadata:     metadata,
		HeaderPrefix: headerPrefix,
		Signatures:   []string{},
	}

	if len(secrets) == 0 {
		return req
	}

	sm := NewSignatureManager(secrets)
	req.Signatures = sm.GenerateSignatures(time.Unix(req.Timestamp, 0), req.RawBody)

	return req
}

func (wr *WebhookRequest) ToHTTPRequest(ctx context.Context) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", wr.URL, bytes.NewBuffer(wr.RawBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	// Add timestamp header
	req.Header.Set(wr.HeaderPrefix+"timestamp", fmt.Sprintf("%d", wr.Timestamp))

	if len(wr.Signatures) > 0 {
		// Format: t=123,v0=abc123,def456
		signatureHeader := fmt.Sprintf("t=%d,v0=%s",
			wr.Timestamp,
			strings.Join(wr.Signatures, ","),
		)
		req.Header.Set(wr.HeaderPrefix+"signature", signatureHeader)
	}

	// Add metadata headers with the specified prefix
	for key, value := range wr.Metadata {
		req.Header.Set(wr.HeaderPrefix+strings.ToLower(key), value)
	}

	return req, nil
}
