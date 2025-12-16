package destwebhook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/hookdeck/outpost/internal/destregistry"
)

// HTTPRequestResult contains the result of an HTTP request execution.
// It handles the classification of errors into system errors vs delivery errors.
type HTTPRequestResult struct {
	// Delivery is the delivery result. Will be nil for system errors (context.Canceled).
	Delivery *destregistry.Delivery
	// Error is the error that occurred, if any.
	Error error
	// Response is the HTTP response, if one was received. Caller should NOT close the body.
	Response *http.Response
}

// ExecuteHTTPRequest executes an HTTP request and classifies the result.
//
// Error classification:
//   - context.Canceled: System error (service shutdown) → returns nil Delivery
//     so messagehandler treats it as PreDeliveryError (nack → requeue)
//   - Other network errors: Delivery error → returns Delivery with classified code
//     so messagehandler treats it as DeliveryError (ack + retry)
//   - HTTP 4xx/5xx: Delivery error → returns Delivery with status code
//   - HTTP 2xx/3xx: Success → returns Delivery with success status
//
// See: https://github.com/hookdeck/outpost/issues/571
func ExecuteHTTPRequest(ctx context.Context, client *http.Client, req *http.Request, provider string) *HTTPRequestResult {
	resp, err := client.Do(req)
	if err != nil {
		// Context canceled is a system error (e.g., service shutdown) - return nil
		// so it's treated as PreDeliveryError (nack → requeue for another instance).
		if errors.Is(err, context.Canceled) {
			return &HTTPRequestResult{
				Delivery: nil,
				Error: destregistry.NewErrDestinationPublishAttempt(err, provider, map[string]interface{}{
					"error":   "canceled",
					"message": err.Error(),
				}),
				Response: nil,
			}
		}

		// All other errors are destination-level failures (DeliveryError → ack + retry)
		return &HTTPRequestResult{
			Delivery: &destregistry.Delivery{
				Status: "failed",
				Code:   ClassifyNetworkError(err),
			},
			Error: destregistry.NewErrDestinationPublishAttempt(err, provider, map[string]interface{}{
				"error":   "request_failed",
				"message": err.Error(),
			}),
			Response: nil,
		}
	}

	// HTTP error response (4xx, 5xx)
	if resp.StatusCode >= 400 {
		delivery := &destregistry.Delivery{
			Status: "failed",
			Code:   fmt.Sprintf("%d", resp.StatusCode),
		}
		ParseHTTPResponse(delivery, resp)

		// Extract body for error details
		var bodyStr string
		if delivery.Response != nil {
			if body, ok := delivery.Response["body"]; ok {
				switch v := body.(type) {
				case string:
					bodyStr = v
				case map[string]interface{}:
					if jsonBytes, err := json.Marshal(v); err == nil {
						bodyStr = string(jsonBytes)
					}
				}
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
	ParseHTTPResponse(delivery, resp)

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
//
// Note: context.Canceled is handled separately as a system error (nack → requeue).
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

// ParseHTTPResponse reads and parses the HTTP response body into the delivery.
func ParseHTTPResponse(delivery *destregistry.Delivery, resp *http.Response) {
	bodyBytes, _ := io.ReadAll(resp.Body)

	if strings.Contains(resp.Header.Get("Content-Type"), "application/json") {
		var response map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &response); err != nil {
			delivery.Response = map[string]interface{}{
				"status": resp.StatusCode,
				"body":   string(bodyBytes),
			}
			return
		}
		delivery.Response = map[string]interface{}{
			"status": resp.StatusCode,
			"body":   response,
		}
	} else {
		delivery.Response = map[string]interface{}{
			"status": resp.StatusCode,
			"body":   string(bodyBytes),
		}
	}
}
