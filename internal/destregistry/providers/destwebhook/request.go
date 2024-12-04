package destwebhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"net/http"
	"sort"
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

	// Sort secrets by creation date, newest first
	sortedSecrets := make([]WebhookSecret, len(secrets))
	copy(sortedSecrets, secrets)
	sort.Slice(sortedSecrets, func(i, j int) bool {
		return sortedSecrets[i].CreatedAt.After(sortedSecrets[j].CreatedAt)
	})

	// Always use latest secret
	latestSecret := sortedSecrets[0]
	req.Signatures = append(req.Signatures, generateSignature(latestSecret.Key, req.Timestamp, req.RawBody))

	// Add signatures for non-expired secrets that aren't the latest
	now := time.Now()
	for _, secret := range sortedSecrets[1:] {
		if now.Sub(secret.CreatedAt) < 24*time.Hour {
			req.Signatures = append(req.Signatures, generateSignature(secret.Key, req.Timestamp, req.RawBody))
		}
	}

	return req
}

func (wr *WebhookRequest) ToHTTPRequest(ctx context.Context) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", wr.URL, bytes.NewBuffer(wr.RawBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
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

func generateSignature(secret string, timestamp int64, rawBody []byte) string {
	// Construct the signed content: "{timestamp}.{raw_body}"
	signedContent := fmt.Sprintf("%d.%s", timestamp, rawBody)

	// Generate HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signedContent))

	// Return just the hex signature
	return fmt.Sprintf("%x", mac.Sum(nil))
}
