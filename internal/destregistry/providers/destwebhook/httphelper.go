package destwebhook

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/hookdeck/outpost/internal/destregistry"
)

// HTTPRequestResult contains the result of an HTTP request execution.
type HTTPRequestResult struct {
	// Delivery is the delivery result.
	Delivery *destregistry.Delivery
	// Error is the error that occurred, if any.
	Error error
	// Response is the HTTP response, if one was received. Caller should NOT close the body.
	Response *http.Response
}

// ExecuteHTTPRequest executes an HTTP request and classifies the result.
//
// Most errors return a Delivery object with a classified error code so the
// caller can record a failed attempt.
//
// Proxy *infrastructure* errors (ErrProxyInfra) return Delivery: nil so the
// caller signals the queue to nack the message instead of recording a
// customer-visible attempt. See registry.go for the nil-attempt handling.
//
// maxResponseBodyBytes caps how much of the destination response body is read
// and stored on the attempt. 0 means no limit. See ParseHTTPResponse.
//
// See: https://github.com/hookdeck/outpost/issues/571
func ExecuteHTTPRequest(ctx context.Context, client *http.Client, req *http.Request, provider string, maxResponseBodyBytes int) *HTTPRequestResult {
	resp, err := client.Do(req)
	if err != nil {
		// Proxy infrastructure error: nack via nil Delivery so the customer's
		// retry budget is not charged for our infra outage.
		var infraErr *ErrProxyInfra
		if errors.As(err, &infraErr) {
			return &HTTPRequestResult{
				Delivery: nil,
				Error: destregistry.NewErrDestinationPublishAttempt(err, provider, map[string]interface{}{
					"error":   "proxy_infrastructure",
					"message": infraErr.Error(),
				}),
				Response: nil,
			}
		}

		// Proxy-attributed destination error: use the explicit Code instead of
		// substring-matching the underlying error.
		var code string
		var destErr *ErrProxyDestination
		if errors.As(err, &destErr) {
			code = destErr.Code
		} else {
			code = ClassifyNetworkError(err)
		}

		data := map[string]interface{}{
			"error":   "request_failed",
			"message": err.Error(),
		}
		// Attach raw proxy diagnostics (e.g. Envoy flag + response-code-details)
		// to the publish-attempt error so operators can grep them in logs.
		// Keys are owned by whichever proxy populated them. Not placed in the
		// delivery's ResponseData — customer-visible attempt stays free of
		// proxy details.
		if destErr != nil {
			for k, v := range destErr.Diagnostics {
				data[k] = v
			}
		}

		return &HTTPRequestResult{
			Delivery: &destregistry.Delivery{
				Status: "failed",
				Code:   code,
			},
			Error:    destregistry.NewErrDestinationPublishAttempt(err, provider, data),
			Response: nil,
		}
	}

	// HTTP error response (4xx, 5xx)
	if resp.StatusCode >= 400 {
		delivery := &destregistry.Delivery{
			Status: "failed",
			Code:   fmt.Sprintf("%d", resp.StatusCode),
		}
		ParseHTTPResponse(delivery, resp, maxResponseBodyBytes)

		// Extract body for error details
		var bodyStr string
		if delivery.Response != nil {
			if body, ok := delivery.Response["body"].(string); ok {
				bodyStr = body
			}
		}

		return &HTTPRequestResult{
			Delivery: delivery,
			Error: destregistry.NewErrDestinationPublishAttempt(
				fmt.Errorf("request failed with status %d: %s", resp.StatusCode, bodyStr),
				provider,
				map[string]interface{}{
					"status": resp.StatusCode,
					"body":   bodyStr,
				}),
			Response: resp,
		}
	}

	// Success
	delivery := &destregistry.Delivery{
		Status: "success",
		Code:   fmt.Sprintf("%d", resp.StatusCode),
	}
	ParseHTTPResponse(delivery, resp, maxResponseBodyBytes)

	return &HTTPRequestResult{
		Delivery: delivery,
		Error:    nil,
		Response: resp,
	}
}

// ClassifyNetworkError returns a descriptive error code based on the error type.
// All errors classified here are destination-level failures (DeliveryError → ack + retry).
//
// Error codes and their meanings:
//   - dns_error:          Domain doesn't exist or DNS lookup failed
//   - connection_refused: Server not running or rejecting connections
//   - connection_reset:   Connection was dropped by the server
//   - network_unreachable: Network path to destination is unavailable
//   - timeout:            Request took too long (I/O timeout or context deadline)
//   - tls_error:          TLS/SSL certificate or handshake failure
//   - redirect_error:     Too many redirects
//   - network_error:      Other network-related failures (catch-all)
func ClassifyNetworkError(err error) string {
	if err == nil {
		return "unknown"
	}

	errStr := err.Error()

	switch {
	case strings.Contains(errStr, "no such host"):
		return "dns_error"
	case strings.Contains(errStr, "connection refused"):
		return "connection_refused"
	case strings.Contains(errStr, "connection reset"):
		return "connection_reset"
	case strings.Contains(errStr, "network is unreachable"):
		return "network_unreachable"
	case strings.Contains(errStr, "i/o timeout"):
		return "timeout"
	case strings.Contains(errStr, "context deadline exceeded"):
		return "timeout"
	case strings.Contains(errStr, "tls:") || strings.Contains(errStr, "x509:"):
		return "tls_error"
	case strings.Contains(errStr, "too many redirects") || strings.Contains(errStr, "stopped after"):
		return "redirect_error"
	default:
		return "network_error"
	}
}

// ParseHTTPResponse reads the HTTP response body into the delivery as a raw string.
// The body is stored verbatim regardless of content type to preserve data integrity.
//
// maxBytes caps the stored body so an oversized response can't push the attempt
// log past the queue's per-message size limit (which would fail to publish and
// retry forever). 0 disables the cap. When the body exceeds maxBytes it is
// replaced wholesale with a placeholder rather than truncated, so consumers never
// receive a partial/corrupt body. We read with a maxBytes+1 LimitReader: enough to
// detect the overflow without buffering the whole body, but it means we stop
// before draining resp.Body, so that connection won't be reused — an acceptable
// trade-off versus downloading an arbitrarily large body just to discard it.
func ParseHTTPResponse(delivery *destregistry.Delivery, resp *http.Response, maxBytes int) {
	var body string
	if maxBytes > 0 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, int64(maxBytes)+1))
		if len(bodyBytes) > maxBytes {
			body = fmt.Sprintf("Response body exceeded %d bytes and was not stored", maxBytes)
		} else {
			body = string(bodyBytes)
		}
	} else {
		bodyBytes, _ := io.ReadAll(resp.Body)
		body = string(bodyBytes)
	}
	delivery.Response = map[string]interface{}{
		"status": resp.StatusCode,
		"body":   body,
	}
}
