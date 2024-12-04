package destwebhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type WebhookRequest struct {
	URL          string
	EventID      string
	Timestamp    int64
	RawBody      []byte
	Signatures   []string
	Metadata     map[string]string
	HeaderPrefix string
}

func NewWebhookRequest(url string, eventID string, rawBody []byte, metadata map[string]string, headerPrefix string, secrets []WebhookSecret) *WebhookRequest {
	timestamp := time.Now().Unix()
	var signatures []string

	if len(secrets) == 1 {
		// If there's only one secret, always use it regardless of age
		sig := generateSignature(secrets[0].Key, eventID, timestamp, rawBody)
		signatures = append(signatures, sig)
	} else if len(secrets) > 1 {
		// During rotation (multiple secrets), only use secrets from the last 24 hours
		for _, secret := range secrets {
			if time.Since(secret.CreatedAt) <= 24*time.Hour {
				sig := generateSignature(secret.Key, eventID, timestamp, rawBody)
				signatures = append(signatures, sig)
			}
		}
	}

	return &WebhookRequest{
		URL:          url,
		EventID:      eventID,
		Timestamp:    timestamp,
		RawBody:      rawBody,
		Signatures:   signatures,
		Metadata:     metadata,
		HeaderPrefix: headerPrefix,
	}
}

func (wr *WebhookRequest) ToHTTPRequest(ctx context.Context) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, "POST", wr.URL, bytes.NewBuffer(wr.RawBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	if len(wr.Signatures) > 0 {
		req.Header.Set(wr.HeaderPrefix+"signature", strings.Join(wr.Signatures, " "))
	}

	// Add metadata headers with the specified prefix
	for key, value := range wr.Metadata {
		req.Header.Set(wr.HeaderPrefix+strings.ToLower(key), value)
	}

	return req, nil
}

func generateSignature(secret string, eventID string, timestamp int64, rawBody []byte) string {
	// Construct the signed content: "{event_id}.{timestamp}.{raw_body}"
	signedContent := fmt.Sprintf("%s.%d.%s", eventID, timestamp, rawBody)

	// Generate HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signedContent))

	// Return formatted signature: t={timestamp},v1={hex_signature}
	return fmt.Sprintf("t=%d,v1=%x", timestamp, mac.Sum(nil))
}
