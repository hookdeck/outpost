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
	"github.com/hookdeck/outpost/internal/netguard"
)

// Attempt codes besides HTTP statuses and destwebhook's network codes.
const (
	CodePayloadTooLarge   = "payload_too_large"
	CodeInvalidEventID    = "invalid_event_id"
	CodeThrottled         = "throttled"
	CodeAddressNotAllowed = "address_not_allowed"
)

// maxDrainBytes bounds how much of a response past the stored part is read
// and discarded so the connection can be reused; a longer body costs the
// connection instead.
const maxDrainBytes = 64 << 10

// headerNames are the delivery headers, spelled as the MCP Events
// documentation shows them: http.Header.Set would canonicalize
// X-MCP-Subscription-Id to X-Mcp-Subscription-Id and webhook-id to
// Webhook-Id. Header names are case-insensitive, so this is cosmetic, but it
// matches what receivers see in the docs.
var headerNames = [...]string{
	mcpevents.HeaderContentType,
	mcpevents.HeaderWebhookID,
	mcpevents.HeaderWebhookTimestamp,
	mcpevents.HeaderWebhookSignature,
	mcpevents.HeaderSubscriptionID,
}

// Publisher delivers events to one subscription. It holds no resources: the
// client and host limiter belong to the provider, so Close returns at once.
type Publisher struct {
	client               *http.Client
	hostLimiter          *netguard.HostLimiter
	url                  string
	hostPort             string
	subscriptionID       string
	secrets              []mcpevents.Secret
	userAgent            string
	maxResponseBodyBytes int
	now                  func() time.Time
}

var _ destregistry.Publisher = (*Publisher)(nil)

// Close implements destregistry.Publisher; there is nothing to release.
func (p *Publisher) Close() error {
	return nil
}

// Publish sends one event as an MCP event envelope. Every outcome but a
// cancelled ctx (shutdown: nil Delivery, so the message is requeued) is a
// Delivery: a 2xx is a success; any other status, 3xx included since
// redirects are never followed, fails with the status as its code, and 410
// and 413 are not retried; an envelope over 256 KiB or an event ID that
// can't be a header fails without a request and is not retried; a callback
// host at its in-flight limit fails at once as "throttled", and a request the
// address guard refuses as "address_not_allowed", both retried.
func (p *Publisher) Publish(ctx context.Context, event *models.Event) (*destregistry.Delivery, error) {
	body, err := mcpevents.EventEnvelope(event)
	if err != nil {
		return envelopeFailure(err)
	}

	release, ok := p.hostLimiter.TryAcquire(p.hostPort)
	if !ok {
		return failure(CodeThrottled, "too many attempts in flight to the callback host", errors.New("destmcp: callback host at its in-flight limit"), false)
	}
	defer release()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		return failure("ERR", "could not build the request", err, true)
	}
	if err := p.setHeaders(req.Header, event.ID, body); err != nil {
		return failure("ERR", "no valid signing secret", err, true)
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

// setHeaders sets the delivery headers, signed with every key valid now (both
// during a rotation).
func (p *Publisher) setHeaders(h http.Header, msgID string, body []byte) error {
	now := p.now()
	signed := make(http.Header, len(headerNames))
	if err := mcpevents.SetHeaders(signed, msgID, p.subscriptionID, now, body, mcpevents.ActiveKeys(p.secrets, now)); err != nil {
		return err
	}
	for _, name := range headerNames {
		h[name] = signed[http.CanonicalHeaderKey(name)]
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
	return delivery, &destregistry.ErrDestinationPublishAttempt{
		Err:          fmt.Errorf("callback responded with status %d", resp.StatusCode),
		Provider:     Type,
		Data:         map[string]interface{}{"error": "http_error", "status": resp.StatusCode},
		NonRetryable: resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusRequestEntityTooLarge,
	}
}

// readBody returns the first maxResponseBodyBytes of the body (ok false when
// bodies aren't stored), drains up to maxDrainBytes more so the connection
// can be reused, and closes it. A read error keeps what was read: the status
// already decided the outcome.
func (p *Publisher) readBody(resp *http.Response) (string, bool) {
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusSwitchingProtocols {
		// The body is the raw connection; never wait on it.
		return "", p.maxResponseBodyBytes > 0
	}
	var stored []byte
	if p.maxResponseBodyBytes > 0 {
		stored, _ = io.ReadAll(io.LimitReader(resp.Body, int64(p.maxResponseBodyBytes)))
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDrainBytes))
	return string(stored), p.maxResponseBodyBytes > 0
}

// envelopeFailure records an event that can't be sent as an MCP envelope.
// Retrying can't change the event, so none of these are retried.
func envelopeFailure(err error) (*destregistry.Delivery, error) {
	switch {
	case errors.Is(err, mcpevents.ErrEnvelopeTooLarge):
		return failure(CodePayloadTooLarge, "the event envelope exceeds 256 KiB", err, true)
	case errors.Is(err, mcpevents.ErrInvalidEventID):
		return failure(CodeInvalidEventID, "the event ID must be 1 to 256 visible ASCII characters", err, true)
	default:
		return failure("ERR", "the event data is not valid JSON", err, true)
	}
}

// failure is a failed attempt decided before or instead of a response.
func failure(code, message string, err error, nonRetryable bool) (*destregistry.Delivery, error) {
	return &destregistry.Delivery{
		Status:   models.AttemptStatusFailed,
		Code:     code,
		Response: map[string]interface{}{"error": message},
	}, &destregistry.ErrDestinationPublishAttempt{
		Err:          err,
		Provider:     Type,
		Data:         map[string]interface{}{"error": code},
		NonRetryable: nonRetryable,
	}
}

// classifyRequestError maps a client error to an attempt code: the guard's
// refusals are address_not_allowed, timeouts of any kind are timeout, and
// everything else gets the webhook network codes.
func classifyRequestError(err error) string {
	if netguard.IsAddressNotAllowed(err) {
		return CodeAddressNotAllowed
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	return destwebhook.ClassifyNetworkError(err)
}

// requestErrorMessage is the client error without the request URL, which
// can carry the receiver's path tokens; it is for logs only.
func requestErrorMessage(err error) string {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return urlErr.Err.Error()
	}
	return err.Error()
}
