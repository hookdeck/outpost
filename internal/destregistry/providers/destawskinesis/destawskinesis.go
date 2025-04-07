package destawskinesis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	awscreds "github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/kinesis"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/jmespath/go-jmespath"
)

// Configuration types
type AWKKinesisConfig struct {
	StreamName           string
	Region               string
	Endpoint             string
	PartitionKeyTemplate string
}

type AWSKinesisCredentials struct {
	Key     string
	Secret  string
	Session string // optional
}

// Provider implementation
type AWSKinesisProvider struct {
	*destregistry.BaseProvider
}

var _ destregistry.Provider = (*AWSKinesisProvider)(nil) // Ensure interface compliance

// Constructor
func New(loader metadata.MetadataLoader) (*AWSKinesisProvider, error) {
	base, err := destregistry.NewBaseProvider(loader, "aws_kinesis")
	if err != nil {
		return nil, err
	}
	return &AWSKinesisProvider{BaseProvider: base}, nil
}

// Validate performs destination-specific validation
func (p *AWSKinesisProvider) Validate(ctx context.Context, destination *models.Destination) error {
	_, _, err := p.resolveConfig(ctx, destination)
	return err
}

// CreatePublisher creates a new publisher instance
func (p *AWSKinesisProvider) CreatePublisher(ctx context.Context, destination *models.Destination) (destregistry.Publisher, error) {
	config, credentials, err := p.resolveConfig(ctx, destination)
	if err != nil {
		return nil, err
	}

	// Configure AWS SDK
	sdkConfig, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithCredentialsProvider(awscreds.NewStaticCredentialsProvider(
			credentials.Key,
			credentials.Secret,
			credentials.Session,
		)),
		awsconfig.WithRegion(config.Region),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create Kinesis client with custom endpoint if provided
	kinesisClient := kinesis.NewFromConfig(sdkConfig, func(o *kinesis.Options) {
		if config.Endpoint != "" {
			o.BaseEndpoint = awssdk.String(config.Endpoint)
		}
	})

	return &AWSKinesisPublisher{
		BasePublisher:        &destregistry.BasePublisher{},
		client:               kinesisClient,
		streamName:           config.StreamName,
		partitionKeyTemplate: config.PartitionKeyTemplate,
	}, nil
}

// resolveConfig parses the destination config and credentials
func (p *AWSKinesisProvider) resolveConfig(ctx context.Context, destination *models.Destination) (*AWKKinesisConfig, *AWSKinesisCredentials, error) {
	// Validate basic requirements using the base provider
	if err := p.BaseProvider.Validate(ctx, destination); err != nil {
		return nil, nil, err
	}

	// Validate endpoint if provided
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

	return &AWKKinesisConfig{
			StreamName:           destination.Config["stream_name"],
			Region:               destination.Config["region"],
			Endpoint:             destination.Config["endpoint"],
			PartitionKeyTemplate: destination.Config["partition_key_template"],
		}, &AWSKinesisCredentials{
			Key:     destination.Credentials["key"],
			Secret:  destination.Credentials["secret"],
			Session: destination.Credentials["session"],
		}, nil
}

// ComputeTarget returns a human-readable target for display
func (p *AWSKinesisProvider) ComputeTarget(destination *models.Destination) string {
	streamName := destination.Config["stream_name"]
	region := destination.Config["region"]
	return fmt.Sprintf("%s in %s", streamName, region)
}

// Preprocess sets defaults and standardizes values
func (p *AWSKinesisProvider) Preprocess(newDestination *models.Destination, originalDestination *models.Destination, opts *destregistry.PreprocessDestinationOpts) error {
	// No preprocessing needed for current config fields
	return nil
}

// Publisher implementation
type AWSKinesisPublisher struct {
	*destregistry.BasePublisher
	client               *kinesis.Client
	streamName           string
	partitionKeyTemplate string
}

// Close handles resource cleanup
func (p *AWSKinesisPublisher) Close() error {
	p.BasePublisher.StartClose()
	// No specific resources to clean up as the AWS SDK handles its own cleanup
	return nil
}

// evaluatePartitionKey extracts the partition key from the event using the JMESPath template
func (p *AWSKinesisPublisher) evaluatePartitionKey(payload map[string]interface{}, eventID string) (string, error) {
	// If no template is specified or empty, use event ID
	if p.partitionKeyTemplate == "" {
		return eventID, nil
	}

	// Evaluate the JMESPath template
	result, err := jmespath.Search(p.partitionKeyTemplate, payload)
	if err != nil {
		return "", fmt.Errorf("error evaluating partition key template: %w", err)
	}

	// Handle nil result - fall back to event ID
	if result == nil {
		return eventID, nil
	}

	// Convert the result to string based on its type
	switch v := result.(type) {
	case string:
		if v == "" {
			return eventID, nil // Fall back to event ID if empty string
		}
		return v, nil
	case float64:
		return fmt.Sprintf("%g", v), nil
	case int:
		return fmt.Sprintf("%d", v), nil
	case bool:
		return fmt.Sprintf("%t", v), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

// Publish sends an event to the Kinesis stream
func (p *AWSKinesisPublisher) Publish(ctx context.Context, event *models.Event) (*destregistry.Delivery, error) {
	if err := p.BasePublisher.StartPublish(); err != nil {
		return nil, err
	}
	defer p.BasePublisher.FinishPublish()

	// Prepare metadata
	metadata := p.BasePublisher.MakeMetadata(event, time.Now())
	// We must convert the metadata to a map[string]interface{} to properly evaluate the JMESPath template
	// because JMESPath expects a map[string]interface{} instead of map[string]string
	metadataMap := make(map[string]interface{})
	for k, v := range metadata {
		metadataMap[k] = v
	}

	// Create payload with metadata and data
	payload := map[string]interface{}{
		"metadata": metadataMap,
		"data":     event.Data,
	}

	// Serialize payload to JSON
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, destregistry.NewErrDestinationPublishAttempt(
			err,
			"aws_kinesis",
			map[string]interface{}{
				"error":   "json_marshal_failed",
				"message": err.Error(),
			},
		)
	}

	// Get partition key from template or use event ID as default
	partitionKey, err := p.evaluatePartitionKey(payload, event.ID)
	if err != nil {
		// If template evaluation fails, log the error and fall back to event ID
		partitionKey = event.ID
	}

	// Create the PutRecord input
	input := &kinesis.PutRecordInput{
		StreamName:   awssdk.String(p.streamName),
		Data:         data,
		PartitionKey: awssdk.String(partitionKey),
	}

	// Send the record to Kinesis
	result, err := p.client.PutRecord(ctx, input)
	if err != nil {
		return &destregistry.Delivery{
				Status: "failed",
				Code:   "ERR",
				Response: map[string]interface{}{
					"error": err.Error(),
				},
			}, destregistry.NewErrDestinationPublishAttempt(
				err,
				"aws_kinesis",
				map[string]interface{}{
					"error":         formatAWSError(err),
					"stream_name":   p.streamName,
					"partition_key": partitionKey,
				},
			)
	}

	// Return success with partition key info
	return &destregistry.Delivery{
		Status: "success",
		Code:   "OK",
		Response: map[string]interface{}{
			"shard_id":        *result.ShardId,
			"sequence_number": *result.SequenceNumber,
			"partition_key":   partitionKey,
		},
	}, nil
}

// Helper function to format AWS errors
func formatAWSError(err error) string {
	if strings.Contains(err.Error(), "ResourceNotFoundException") {
		return "stream_not_found"
	} else if strings.Contains(err.Error(), "AccessDeniedException") {
		return "access_denied"
	} else if strings.Contains(err.Error(), "ProvisionedThroughputExceededException") {
		return "throughput_exceeded"
	} else if strings.Contains(err.Error(), "ValidationException") {
		return "validation_error"
	}
	return "request_failed"
}
