package destmcp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/mcpevents"
	"github.com/hookdeck/outpost/internal/models"
)

const CodePayloadTooLarge = "payload_too_large"

const maxDrainBytes = 64 << 10

type Publisher struct {
	client               *http.Client
	url                  string
	tenantID             string
	destinationID        string
	secrets              []mcpevents.Secret
	userAgent            string
	maxResponseBodyBytes int
	now                  func() time.Time
}

var _ destregistry.Publisher = (*Publisher)(nil)

func (p *Publisher) Close() error {
	return nil
}

func (p *Publisher) Publish(ctx context.Context, event *models.Event) (*destregistry.Delivery, error) {
	body, err := mcpevents.EventEnvelope(event)
	if err != nil {
		return envelopeFailure(err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		return failure("ERR", "could not build the request", err, true, false)
	}
	if err := p.setHeaders(req.Header, event.ID, body); err != nil {
		return failure("ERR", "no valid signing secret", err, true, false)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) && ctx.Err() != nil {
			return nil, err
		}
		code := classifyRequestError(err)
		return &destregistry.Delivery{Status: models.AttemptStatusFailed, Code: code},
			&destregistry.ErrDestinationPublishAttempt{
				Err:      err,
				Provider: Type,
				Data:     map[string]interface{}{"error": "request_failed", "code": code, "message": requestErrorMessage(err)},
			}
	}
	return p.handleResponse(resp)
}

func (p *Publisher) setHeaders(h http.Header, eventID string, body []byte) error {
	now := p.now()
	msgID := mcpevents.DeliveryMessageID(p.tenantID, p.destinationID, eventID)
	if err := mcpevents.SetHeaders(h, msgID, p.destinationID, now, body, mcpevents.ActiveKeys(p.secrets, now)); err != nil {
		return err
	}
	if p.userAgent != "" {
		h.Set("User-Agent", p.userAgent)
	}
	return nil
}

func (p *Publisher) handleResponse(resp *http.Response) (*destregistry.Delivery, error) {
	response := map[string]interface{}{"status": resp.StatusCode}
	if body, ok := p.readBody(resp); ok {
		response["body"] = body
	}
	delivery := &destregistry.Delivery{
		Status:   models.AttemptStatusSuccess,
		Code:     strconv.Itoa(resp.StatusCode),
		Response: response,
	}
	if resp.StatusCode >= 200 && resp.StatusCode <= 299 {
		return delivery, nil
	}
	delivery.Status = models.AttemptStatusFailed
	tooLargeOrGone := resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusRequestEntityTooLarge
	return delivery, &destregistry.ErrDestinationPublishAttempt{
		Err:              fmt.Errorf("callback responded with status %d", resp.StatusCode),
		Provider:         Type,
		Data:             map[string]interface{}{"error": "http_error", "status": resp.StatusCode},
		NonRetryable:     tooLargeOrGone,
		SkipFailureCount: tooLargeOrGone,
	}
}

func (p *Publisher) readBody(resp *http.Response) (string, bool) {
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusSwitchingProtocols {
		return "", p.maxResponseBodyBytes > 0
	}
	var stored []byte
	if p.maxResponseBodyBytes > 0 {
		stored, _ = io.ReadAll(io.LimitReader(resp.Body, int64(p.maxResponseBodyBytes)))
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrainBytes))
	return string(stored), p.maxResponseBodyBytes > 0
}

func envelopeFailure(err error) (*destregistry.Delivery, error) {
	if errors.Is(err, mcpevents.ErrEnvelopeTooLarge) {
		return failure(CodePayloadTooLarge, "the event envelope exceeds 256 KiB", err, true, true)
	}
	return failure("ERR", "the event data is not valid JSON", err, true, false)
}

func failure(code, message string, err error, nonRetryable, skipFailureCount bool) (*destregistry.Delivery, error) {
	return &destregistry.Delivery{
			Status:   models.AttemptStatusFailed,
			Code:     code,
			Response: map[string]interface{}{"error": message},
		}, &destregistry.ErrDestinationPublishAttempt{
			Err:              err,
			Provider:         Type,
			Data:             map[string]interface{}{"error": code},
			NonRetryable:     nonRetryable,
			SkipFailureCount: skipFailureCount,
		}
}

func classifyRequestError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	return destwebhook.ClassifyNetworkError(err)
}

func requestErrorMessage(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return urlErr.Err.Error()
	}
	return err.Error()
}
