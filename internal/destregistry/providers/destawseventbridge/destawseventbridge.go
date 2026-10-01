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

// DefaultDetailType is the DetailType of an event published without a topic.
// EventBridge rejects an entry with an empty DetailType.
const DefaultDetailType = "event"

type AWSEventBridgeConfig struct {
	EventBusName string
	Region       string
	Endpoint     string
}

type AWSEventBridgeCredentials struct {
	Key     string
	Secret  string
	Session string // optional
}

// Provider implementation
type AWSEventBridgeProvider struct {
	*destregistry.BaseProvider
	source string
}

var _ destregistry.Provider = (*AWSEventBridgeProvider)(nil)

// Option is a functional option for configuring AWSEventBridgeProvider
type Option func(*AWSEventBridgeProvider)

// WithSource sets the Source of every published event.
func WithSource(source string) Option {
	return func(p *AWSEventBridgeProvider) {
		p.source = source
	}
}

// New constructs an AWSEventBridgeProvider.
func New(loader metadata.MetadataLoader, basePublisherOpts []destregistry.BasePublisherOption, opts ...Option) (*AWSEventBridgeProvider, error) {
	base, err := destregistry.NewBaseProvider(loader, "aws_eventbridge", basePublisherOpts...)
	if err != nil {
		return nil, err
	}

	provider := &AWSEventBridgeProvider{BaseProvider: base}
	for _, opt := range opts {
		opt(provider)
	}

	if provider.source == "" {
		return nil, errors.New("aws_eventbridge: source is required (DESTINATIONS_AWS_EVENTBRIDGE_SOURCE)")
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

func (p *AWSEventBridgePublisher) Close() error {
	p.BasePublisher.StartClose()
	return nil
}

// Format prepares the event as a single-entry PutEventsInput. Detail carries
// the event data and its metadata.
func (p *AWSEventBridgePublisher) Format(ctx context.Context, event *models.Event) (*eventbridge.PutEventsInput, error) {
	detail, err := json.Marshal(map[string]interface{}{
		"metadata": p.BasePublisher.MakeMetadata(event, time.Now()),
		"data":     json.RawMessage(event.Data),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event detail: %w", err)
	}

	detailType := event.Topic
	if detailType == "" {
		detailType = DefaultDetailType
	}

	entry := types.PutEventsRequestEntry{
		Source:     awssdk.String(p.source),
		DetailType: awssdk.String(detailType),
		Detail:     awssdk.String(string(detail)),
	}
	if !event.Time.IsZero() {
		entry.Time = awssdk.Time(event.Time)
	}
	if p.eventBusName != "" {
		entry.EventBusName = awssdk.String(p.eventBusName)
	}

	return &eventbridge.PutEventsInput{
		Entries: []types.PutEventsRequestEntry{entry},
	}, nil
}

// Publish sends a single event via PutEvents. PutEvents reports rejected
// entries in the response body with a nil error, so both are checked.
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
		response := map[string]interface{}{"error": err.Error()}
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			response["error_code"] = apiErr.ErrorCode()
		}
		return failedDelivery(err, response)
	}

	if output.FailedEntryCount > 0 {
		entryErr := errors.New("eventbridge: entry rejected")
		response := map[string]interface{}{}
		if len(output.Entries) > 0 && output.Entries[0].ErrorCode != nil {
			code := awssdk.ToString(output.Entries[0].ErrorCode)
			entryErr = fmt.Errorf("%w: %s", entryErr, code)
			if message := awssdk.ToString(output.Entries[0].ErrorMessage); message != "" {
				entryErr = fmt.Errorf("%w: %s", entryErr, message)
			}
			response["error_code"] = code
		}
		response["error"] = entryErr.Error()
		return failedDelivery(entryErr, response)
	}

	response := map[string]interface{}{
		"source":      p.source,
		"detail_type": awssdk.ToString(input.Entries[0].DetailType),
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

func failedDelivery(err error, response map[string]interface{}) (*destregistry.Delivery, error) {
	return &destregistry.Delivery{
		Status:   "failed",
		Code:     "ERR",
		Response: response,
	}, destregistry.NewErrDestinationPublishAttempt(err, "aws_eventbridge", response)
}
