package destcfqueues

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/models"
)

const (
	cloudflareAPIBaseURL = "https://api.cloudflare.com/client/v4"
	providerType         = "cloudflare_queues"
)

// CloudflareQueuesDestination implements the destregistry.Provider interface for Cloudflare Queues.
type CloudflareQueuesDestination struct {
	*destregistry.BaseProvider
}

// CloudflareQueuesConfig holds the configuration for a Cloudflare Queues destination.
type CloudflareQueuesConfig struct {
	AccountID string `json:"account_id" mapstructure:"account_id"`
	QueueID   string `json:"queue_id" mapstructure:"queue_id"`
}

// CloudflareQueuesCredentials holds the credentials for authenticating with Cloudflare.
type CloudflareQueuesCredentials struct {
	APIToken string `json:"api_token" mapstructure:"api_token"`
}

var _ destregistry.Provider = (*CloudflareQueuesDestination)(nil)

// New creates a new CloudflareQueuesDestination provider.
func New(loader metadata.MetadataLoader, basePublisherOpts []destregistry.BasePublisherOption) (*CloudflareQueuesDestination, error) {
	base, err := destregistry.NewBaseProvider(loader, providerType, basePublisherOpts...)
	if err != nil {
		return nil, err
	}

	return &CloudflareQueuesDestination{
		BaseProvider: base,
	}, nil
}

// Validate validates the destination configuration.
func (d *CloudflareQueuesDestination) Validate(ctx context.Context, destination *models.Destination) error {
	_, _, err := d.resolveMetadata(ctx, destination)
	if err != nil {
		return err
	}
	return nil
}

// CreatePublisher creates a new publisher for the destination.
func (d *CloudflareQueuesDestination) CreatePublisher(ctx context.Context, destination *models.Destination) (destregistry.Publisher, error) {
	cfg, creds, err := d.resolveMetadata(ctx, destination)
	if err != nil {
		return nil, err
	}

	httpClient, err := destregistry.NewHTTPClient(destregistry.HTTPClientConfig{})
	if err != nil {
		return nil, err
	}

	return &CloudflareQueuesPublisher{
		BasePublisher: d.BaseProvider.NewPublisher(destregistry.WithDeliveryMetadata(destination.DeliveryMetadata)),
		httpClient:    httpClient,
		accountID:     cfg.AccountID,
		queueID:       cfg.QueueID,
		apiToken:      creds.APIToken,
	}, nil
}

// ComputeTarget returns the target information for display purposes.
func (d *CloudflareQueuesDestination) ComputeTarget(destination *models.Destination) destregistry.DestinationTarget {
	accountID := destination.Config["account_id"]
	queueID := destination.Config["queue_id"]

	return destregistry.DestinationTarget{
		Target:    queueID,
		TargetURL: makeCloudflareQueuesDashboardURL(accountID, queueID),
	}
}

// resolveMetadata validates and resolves the destination configuration and credentials.
func (d *CloudflareQueuesDestination) resolveMetadata(ctx context.Context, destination *models.Destination) (*CloudflareQueuesConfig, *CloudflareQueuesCredentials, error) {
	if err := d.BaseProvider.Validate(ctx, destination); err != nil {
		return nil, nil, err
	}

	return &CloudflareQueuesConfig{
			AccountID: destination.Config["account_id"],
			QueueID:   destination.Config["queue_id"],
		}, &CloudflareQueuesCredentials{
			APIToken: destination.Credentials["api_token"],
		}, nil
}

// makeCloudflareQueuesDashboardURL constructs the Cloudflare dashboard URL for a queue.
func makeCloudflareQueuesDashboardURL(accountID, queueID string) string {
	if accountID == "" || queueID == "" {
		return ""
	}
	return fmt.Sprintf("https://dash.cloudflare.com/%s/queues/%s", accountID, queueID)
}

// CloudflareQueuesPublisher handles publishing events to Cloudflare Queues.
type CloudflareQueuesPublisher struct {
	*destregistry.BasePublisher
	httpClient *http.Client
	accountID  string
	queueID    string
	apiToken   string
}

// Close gracefully shuts down the publisher.
func (p *CloudflareQueuesPublisher) Close() error {
	p.BasePublisher.StartClose()
	return nil
}

// SetHTTPClient allows setting a custom HTTP client, primarily for testing purposes.
func (p *CloudflareQueuesPublisher) SetHTTPClient(client *http.Client) {
	p.httpClient = client
}

// cloudflareMessageRequest is the body for POST /accounts/{id}/queues/{id}/messages
// (single-message push). See https://developers.cloudflare.com/api/resources/queues/subresources/messages/methods/push/
type cloudflareMessageRequest struct {
	Body        messageBody `json:"body"`
	ContentType string      `json:"content_type"`
}

// cloudflareAPIResponse represents the response from the Cloudflare API.
type cloudflareAPIResponse struct {
	Success  bool                 `json:"success"`
	Errors   []cloudflareAPIError `json:"errors"`
	Messages []string             `json:"messages"`
	Result   *cloudflareResult    `json:"result"`
}

type cloudflareResult struct {
	Metadata struct {
		Metrics struct {
			BacklogBytes             int64 `json:"backlog_bytes"`
			BacklogCount             int64 `json:"backlog_count"`
			OldestMessageTimestampMs int64 `json:"oldest_message_timestamp_ms"`
		} `json:"metrics"`
	} `json:"metadata"`
}

// cloudflareAPIError represents an error from the Cloudflare API.
type cloudflareAPIError struct {
	Code             int    `json:"code"`
	Message          string `json:"message"`
	DocumentationURL string `json:"documentation_url,omitempty"`
	Source           *struct {
		Pointer string `json:"pointer"`
	} `json:"source,omitempty"`
}

// messageBody is the wrapper Outpost places inside the Cloudflare message body.
type messageBody struct {
	Data     json.RawMessage   `json:"data"`
	Metadata map[string]string `json:"metadata"`
}

// Format builds the HTTP request for publishing a single message to a Cloudflare Queue.
func (p *CloudflareQueuesPublisher) Format(ctx context.Context, event *models.Event) (*http.Request, error) {
	reqPayload := cloudflareMessageRequest{
		Body: messageBody{
			Data:     event.Data,
			Metadata: p.BasePublisher.MakeMetadata(event, time.Now()),
		},
		ContentType: "json",
	}

	payloadBytes, err := json.Marshal(reqPayload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request payload: %w", err)
	}

	url := fmt.Sprintf("%s/accounts/%s/queues/%s/messages", cloudflareAPIBaseURL, p.accountID, p.queueID)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiToken)

	return req, nil
}

// Publish sends an event to Cloudflare Queues.
func (p *CloudflareQueuesPublisher) Publish(ctx context.Context, event *models.Event) (*destregistry.Delivery, error) {
	if err := p.BasePublisher.StartPublish(); err != nil {
		return nil, err
	}
	defer p.BasePublisher.FinishPublish()

	req, err := p.Format(ctx, event)
	if err != nil {
		return nil, err
	}

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return &destregistry.Delivery{
			Status: "failed",
			Code:   "ERR",
			Response: map[string]interface{}{
				"error": err.Error(),
			},
		}, destregistry.NewErrDestinationPublishAttempt(err, providerType, map[string]interface{}{
			"error": err.Error(),
		})
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return &destregistry.Delivery{
			Status: "failed",
			Code:   "ERR",
			Response: map[string]interface{}{
				"error": fmt.Sprintf("failed to read response body: %s", err.Error()),
			},
		}, destregistry.NewErrDestinationPublishAttempt(err, providerType, map[string]interface{}{
			"error": fmt.Sprintf("failed to read response body: %s", err.Error()),
		})
	}

	statusCode := strconv.Itoa(resp.StatusCode)

	// Any non-2xx is a failure regardless of body parseability.
	if resp.StatusCode >= 400 {
		var apiResponse cloudflareAPIResponse
		errorMsg := fmt.Sprintf("request failed with status %d", resp.StatusCode)
		response := map[string]interface{}{
			"status": resp.StatusCode,
			"body":   string(bodyBytes),
		}
		if json.Unmarshal(bodyBytes, &apiResponse) == nil && len(apiResponse.Errors) > 0 {
			errorMsg = apiResponse.Errors[0].Message
			response["errors"] = apiResponse.Errors
		}
		return &destregistry.Delivery{
				Status:   "failed",
				Code:     statusCode,
				Response: response,
			}, destregistry.NewErrDestinationPublishAttempt(
				fmt.Errorf("cloudflare API error: %s", errorMsg),
				providerType,
				response,
			)
	}

	// 2xx. Try to parse — if the body is unparseable but status is OK,
	// trust the status. CF returns valid JSON on success; only weird
	// proxies would land us here.
	var apiResponse cloudflareAPIResponse
	if err := json.Unmarshal(bodyBytes, &apiResponse); err == nil {
		if !apiResponse.Success {
			errorMsg := "cloudflare API reported success=false"
			if len(apiResponse.Errors) > 0 {
				errorMsg = apiResponse.Errors[0].Message
			}
			response := map[string]interface{}{
				"status":  resp.StatusCode,
				"success": apiResponse.Success,
				"errors":  apiResponse.Errors,
			}
			return &destregistry.Delivery{
					Status:   "failed",
					Code:     statusCode,
					Response: response,
				}, destregistry.NewErrDestinationPublishAttempt(
					fmt.Errorf("cloudflare API error: %s", errorMsg),
					providerType,
					response,
				)
		}
	}

	return &destregistry.Delivery{
		Status: "success",
		Code:   "OK",
		Response: map[string]interface{}{
			"status": resp.StatusCode,
		},
	}, nil
}
