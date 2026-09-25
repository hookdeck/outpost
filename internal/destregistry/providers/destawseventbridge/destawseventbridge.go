package destawseventbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awscreds "github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	smithy "github.com/aws/smithy-go"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/models"
)

// AWSEventBridgeConfig is the resolved destination config.
type AWSEventBridgeConfig struct {
	EventBusName string
	Region       string
	Endpoint     string
}

// AWSEventBridgeCredentials are optional: an empty Key/Secret is passed to the
// AWS SDK as an explicit (invalid) static credentials provider rather than
// omitted, so a destination with no credentials configured fails loudly with
// an auth error instead of the SDK silently falling back to its default
// credential chain (env vars, shared config, EC2/ECS/EKS instance role) and
// authenticating as whatever role this process happens to be running under.
type AWSEventBridgeCredentials struct {
	Key     string
	Secret  string
	Session string
}

// Provider implementation
type AWSEventBridgeProvider struct {
	*destregistry.BaseProvider
	source string
}

var _ destregistry.Provider = (*AWSEventBridgeProvider)(nil)

// Option is a functional option for configuring AWSEventBridgeProvider
type Option func(*AWSEventBridgeProvider)

// WithSource sets the fixed EventBridge "Source" field stamped on every
// published event. This is an app-level operator setting, not a per-destination
// config field: EventBridge rules commonly match on Source to identify which
// application emitted an event, so it should stay consistent across every
// EventBridge destination for a given Outpost deployment.
func WithSource(source string) Option {
	return func(p *AWSEventBridgeProvider) {
		if source != "" {
			p.source = source
		}
	}
}

// New constructs an AWSEventBridgeProvider.
func New(loader metadata.MetadataLoader, basePublisherOpts []destregistry.BasePublisherOption, opts ...Option) (*AWSEventBridgeProvider, error) {
	base, err := destregistry.NewBaseProvider(loader, "aws_eventbridge", basePublisherOpts...)
	if err != nil {
		return nil, err
	}

	provider := &AWSEventBridgeProvider{
		BaseProvider: base,
		source:       "outpost",
	}

	for _, opt := range opts {
		opt(provider)
	}

	return provider, nil
}

// Validate performs destination-specific validation
func (p *AWSEventBridgeProvider) Validate(ctx context.Context, destination *models.Destination) error {
	_, _, err := p.resolveConfig(ctx, destination)
	return err
}

// CreatePublisher creates a new publisher instance
func (p *AWSEventBridgeProvider) CreatePublisher(ctx context.Context, destination *models.Destination) (destregistry.Publisher, error) {
	cfg, creds, err := p.resolveConfig(ctx, destination)
	if err != nil {
		return nil, err
	}

	sdkConfig, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithCredentialsProvider(awscreds.NewStaticCredentialsProvider(
			creds.Key,
			creds.Secret,
			creds.Session,
		)),
		awsconfig.WithRegion(cfg.Region),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := eventbridge.NewFromConfig(sdkConfig, func(o *eventbridge.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = awssdk.String(cfg.Endpoint)
		}
	})

	return &AWSEventBridgePublisher{
		BasePublisher: p.BaseProvider.NewPublisher(destregistry.WithDeliveryMetadata(destination.DeliveryMetadata)),
		client:        client,
		eventBusName:  cfg.EventBusName,
		source:        p.source,
	}, nil
}

func (p *AWSEventBridgeProvider) resolveConfig(ctx context.Context, destination *models.Destination) (*AWSEventBridgeConfig, *AWSEventBridgeCredentials, error) {
	if err := p.BaseProvider.Validate(ctx, destination); err != nil {
		return nil, nil, err
	}

	if endpoint := destination.Config["endpoint"]; endpoint != "" {
		parsedURL, err := url.Parse(endpoint)
		if err != nil || !parsedURL.IsAbs() || (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") {
			return nil, nil, destregistry.NewErrDestinationValidation([]destregistry.ValidationErrorDetail{
				{
					Field: "config.endpoint",
					Type:  "pattern",
				},
			})
		}
	}

	return &AWSEventBridgeConfig{
			EventBusName: destination.Config["event_bus_name"],
			Region:       destination.Config["region"],
			Endpoint:     destination.Config["endpoint"],
		}, &AWSEventBridgeCredentials{
			Key:     destination.Credentials["key"],
			Secret:  destination.Credentials["secret"],
			Session: destination.Credentials["session"],
		}, nil
}

// ComputeTarget returns a human-readable target for display
func (p *AWSEventBridgeProvider) ComputeTarget(destination *models.Destination) destregistry.DestinationTarget {
	eventBusName := destination.Config["event_bus_name"]
	region := destination.Config["region"]
	return destregistry.DestinationTarget{
		Target:    fmt.Sprintf("%s in %s", eventBusName, region),
		TargetURL: makeEventBridgeConsoleURL(eventBusName, region),
	}
}

// makeEventBridgeConsoleURL builds a console deep-link for a plain event bus
// name. An ARN can name a bus in another account/region, so it's left as a
// target string only rather than guessed at as a same-account console link.
func makeEventBridgeConsoleURL(eventBusName, region string) string {
	if eventBusName == "" || region == "" || strings.HasPrefix(eventBusName, "arn:") {
		return ""
	}
	return fmt.Sprintf("https://%s.console.aws.amazon.com/events/home?region=%s#/eventbus/%s",
		region, region, url.QueryEscape(eventBusName))
}

// Preprocess re-validates the resolved config so a bad config.endpoint is
// rejected at preprocess time rather than surfacing only on first publish.
func (p *AWSEventBridgeProvider) Preprocess(newDestination *models.Destination, originalDestination *models.Destination, opts *destregistry.PreprocessDestinationOpts) error {
	if newDestination.Config == nil {
		return nil
	}
	if _, _, err := p.resolveConfig(context.Background(), newDestination); err != nil {
		return err
	}
	return nil
}

// Publisher implementation
type AWSEventBridgePublisher struct {
	*destregistry.BasePublisher
	client       *eventbridge.Client
	eventBusName string
	source       string
}

// NewAWSEventBridgePublisher creates a publisher for testing Format() and
// other client-independent logic without a live/mocked AWS client.
func NewAWSEventBridgePublisher(client *eventbridge.Client, eventBusName, source string) *AWSEventBridgePublisher {
	return &AWSEventBridgePublisher{
		BasePublisher: &destregistry.BasePublisher{},
		client:        client,
		eventBusName:  eventBusName,
		source:        source,
	}
}

func (p *AWSEventBridgePublisher) Close() error {
	p.BasePublisher.StartClose()
	return nil
}

// Format prepares the event as a single-entry PutEventsInput. EventBridge has
// no side channel for metadata the way SQS message attributes do, so the
// metadata travels inside Detail alongside the event data, same envelope
// shape as the aws_kinesis provider's metadata-in-payload mode.
func (p *AWSEventBridgePublisher) Format(ctx context.Context, event *models.Event) (*eventbridge.PutEventsInput, error) {
	metadata := p.BasePublisher.MakeMetadata(event, time.Now())
	metadataMap := make(map[string]interface{}, len(metadata))
	for k, v := range metadata {
		metadataMap[k] = v
	}

	envelope := map[string]interface{}{
		"metadata": metadataMap,
		"data":     json.RawMessage(event.Data),
	}
	detail, err := json.Marshal(envelope)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event detail: %w", err)
	}

	entry := types.PutEventsRequestEntry{
		Source:     awssdk.String(p.source),
		DetailType: awssdk.String(event.Topic),
		Detail:     awssdk.String(string(detail)),
	}
	if p.eventBusName != "" {
		entry.EventBusName = awssdk.String(p.eventBusName)
	}

	return &eventbridge.PutEventsInput{
		Entries: []types.PutEventsRequestEntry{entry},
	}, nil
}

// Publish sends a single event via PutEvents.
//
// PutEvents is a batch API even for one entry, and it reports per-entry
// success/failure in the response body rather than solely via the returned
// Go error: a request can come back with err == nil and FailedEntryCount == 1
// if that one entry was rejected (bad detail JSON, an unauthorized source,
// etc.), so both the top-level error and the entry's own ErrorCode must be
// checked before a publish can be called successful.
func (p *AWSEventBridgePublisher) Publish(ctx context.Context, event *models.Event) (*destregistry.Delivery, error) {
	if err := p.BasePublisher.StartPublish(); err != nil {
		return nil, err
	}
	defer p.BasePublisher.FinishPublish()

	input, err := p.Format(ctx, event)
	if err != nil {
		return destregistry.NewFormatError("aws_eventbridge", "", err)
	}

	output, err := p.client.PutEvents(ctx, input)
	if err != nil {
		code, message := classifyError(err)
		return &destregistry.Delivery{
				Status: "failed",
				Code:   "ERR",
				Response: map[string]interface{}{
					"error_code": code,
					"error":      message,
				},
			}, destregistry.NewErrDestinationPublishAttempt(err, "aws_eventbridge", map[string]interface{}{
				"error_code": code,
				"error":      message,
			})
	}

	if output.FailedEntryCount > 0 && len(output.Entries) > 0 {
		result := output.Entries[0]
		code, message := classifyErrorCode(awssdk.ToString(result.ErrorCode))
		entryErr := fmt.Errorf("eventbridge: entry rejected: %s", code)
		return &destregistry.Delivery{
				Status: "failed",
				Code:   "ERR",
				Response: map[string]interface{}{
					"error_code": code,
					"error":      message,
				},
			}, destregistry.NewErrDestinationPublishAttempt(entryErr, "aws_eventbridge", map[string]interface{}{
				"error_code": code,
				"error":      message,
			})
	}

	response := map[string]interface{}{
		"source":      p.source,
		"detail_type": event.Topic,
	}
	if len(output.Entries) > 0 {
		response["event_id"] = awssdk.ToString(output.Entries[0].EventId)
	}
	if p.eventBusName != "" {
		response["event_bus_name"] = p.eventBusName
	}

	return &destregistry.Delivery{
		Status:   "success",
		Code:     "OK",
		Response: response,
	}, nil
}

// classifyError sanitizes a top-level PutEvents error (a transport/auth
// failure that never reached the per-entry response) into a safe code and a
// generic, non-AWS-authored description. AWS error messages can carry
// account IDs and ARNs in the free-text portion; only the closed-set error
// code is safe to surface as-is, so the message here is always ours, never
// AWS's.
func classifyError(err error) (code string, message string) {
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		return classifyErrorCode(apiErr.ErrorCode())
	}
	return "request_failed", "the request to EventBridge failed"
}

// classifyErrorCode maps a PutEvents per-entry (or API-level) error code to a
// safe code/message pair. The code itself is one of EventBridge's own
// documented, non-sensitive enum values and is passed through; the message is
// authored here rather than using AWS's own ErrorMessage, which can embed
// account IDs or ARNs.
func classifyErrorCode(errorCode string) (code string, message string) {
	switch errorCode {
	case "":
		return "unknown", "the event was rejected for an unspecified reason"
	case "AccessDeniedException", "NotAuthorizedForSourceException", "NotAuthorizedForDetailTypeException":
		return errorCode, "the configured credentials are not authorized for this event bus, source, or detail type"
	case "InvalidAccountIdException":
		return errorCode, "the configured event bus is not valid for this account"
	case "InvalidArgument", "MalformedDetail":
		return errorCode, "the event was rejected as malformed"
	case "ThrottlingException":
		return errorCode, "the request was throttled by EventBridge"
	case "InternalFailure":
		return errorCode, "EventBridge reported an internal failure"
	case "RedactionFailure":
		return errorCode, "EventBridge failed to redact the event"
	default:
		return errorCode, "the event was rejected by EventBridge"
	}
}
