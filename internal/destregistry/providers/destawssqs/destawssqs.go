package destawssqs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
	"github.com/hookdeck/outpost/internal/models"
)

type AWSSQSDestination struct {
	*destregistry.BaseProvider
}

type AWSSQSDestinationConfig struct {
	Endpoint string
	QueueURL string
}

type AWSSQSDestinationCredentials struct {
	Key     string
	Secret  string
	Session string // optional
}

var _ destregistry.Provider = (*AWSSQSDestination)(nil)

func New(loader metadata.MetadataLoader, basePublisherOpts []destregistry.BasePublisherOption) (*AWSSQSDestination, error) {
	base, err := destregistry.NewBaseProvider(loader, "aws_sqs", basePublisherOpts...)
	if err != nil {
		return nil, err
	}

	return &AWSSQSDestination{
		BaseProvider: base,
	}, nil
}

func (d *AWSSQSDestination) Validate(ctx context.Context, destination *models.Destination) error {
	_, _, err := d.resolveMetadata(ctx, destination)
	if err != nil {
		return err
	}
	return nil
}

func (p *AWSSQSDestination) CreatePublisher(ctx context.Context, destination *models.Destination) (destregistry.Publisher, error) {
	cfg, creds, err := p.resolveMetadata(ctx, destination)
	if err != nil {
		return nil, err
	}

	sdkConfig, err := config.LoadDefaultConfig(ctx,
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			creds.Key,
			creds.Secret,
			creds.Session,
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	baseURL, region, err := ParseQueueURL(cfg.QueueURL)
	if err != nil {
		return nil, err
	}
	if cfg.Endpoint != "" {
		baseURL = cfg.Endpoint
	}

	sqsClient := sqs.NewFromConfig(sdkConfig, func(o *sqs.Options) {
		if baseURL != "" {
			o.BaseEndpoint = awssdk.String(baseURL)
		}
		if region != "" {
			o.Region = region
		}
	})

	return &AWSSQSPublisher{
		BasePublisher: p.BaseProvider.NewPublisher(destregistry.WithDeliveryMetadata(destination.DeliveryMetadata)),
		client:        sqsClient,
		queueURL:      cfg.QueueURL,
	}, nil
}

func (d *AWSSQSDestination) resolveMetadata(ctx context.Context, destination *models.Destination) (*AWSSQSDestinationConfig, *AWSSQSDestinationCredentials, error) {
	if err := d.BaseProvider.Validate(ctx, destination); err != nil {
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

	return &AWSSQSDestinationConfig{
			Endpoint: destination.Config["endpoint"],
			QueueURL: destination.Config["queue_url"],
		}, &AWSSQSDestinationCredentials{
			Key:     destination.Credentials["key"],
			Secret:  destination.Credentials["secret"],
			Session: destination.Credentials["session"],
		}, nil
}

type AWSSQSPublisher struct {
	*destregistry.BasePublisher
	client   *sqs.Client
	queueURL string
}

func (p *AWSSQSPublisher) Close() error {
	p.BasePublisher.StartClose()
	return nil
}

func (p *AWSSQSPublisher) Format(ctx context.Context, event *models.Event) (*sqs.SendMessageInput, error) {
	dataBytes, err := json.Marshal(event.Data)
	if err != nil {
		return nil, err
	}

	metadata := p.BasePublisher.MakeMetadata(event, time.Now())
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}

	return &sqs.SendMessageInput{
		QueueUrl:    awssdk.String(p.queueURL),
		MessageBody: awssdk.String(string(dataBytes)),
		MessageAttributes: map[string]types.MessageAttributeValue{
			"metadata": {
				DataType:    aws.String("String"),
				StringValue: aws.String(string(metadataBytes)),
			},
		},
	}, nil
}

func (p *AWSSQSPublisher) Publish(ctx context.Context, event *models.Event) (*destregistry.Delivery, error) {
	if err := p.BasePublisher.StartPublish(); err != nil {
		return nil, err
	}
	defer p.BasePublisher.FinishPublish()

	msg, err := p.Format(ctx, event)
	if err != nil {
		return nil, err
	}

	if _, err = p.client.SendMessage(ctx, msg); err != nil {
		return &destregistry.Delivery{
				Status: "failed",
				Code:   "ERR",
				Response: map[string]interface{}{
					"error": err.Error(),
				},
			}, destregistry.NewErrDestinationPublishAttempt(err, "aws_sqs", map[string]interface{}{
				"error": err.Error(),
			})
	}

	return &destregistry.Delivery{
		Status:   "success",
		Code:     "OK",
		Response: map[string]interface{}{},
	}, nil
}

// ParseQueueURL extracts the full URL into baseURL & region
func ParseQueueURL(queueURL string) (baseURL string, region string, err error) {
	parsedURL, err := url.Parse(queueURL)
	if err != nil {
		err = fmt.Errorf("failed to parse queue URL: %v", err)
		return
	}

	baseURL = fmt.Sprintf("%s://%s", parsedURL.Scheme, parsedURL.Host)
	if strings.Contains(baseURL, "amazonaws.com") {
		region = strings.Split(parsedURL.Host, ".")[1]
		return
	}

	return
}

func (d *AWSSQSDestination) ComputeTarget(destination *models.Destination) destregistry.DestinationTarget {
	return destregistry.DestinationTarget{
		Target:    destination.Config["queue_url"],
		TargetURL: makeAWSSQSConsoleURL(destination.Config["queue_url"]),
	}
}

func makeAWSSQSConsoleURL(queueURL string) string {
	// Check if it's a valid AWS SQS URL
	if !strings.Contains(queueURL, ".amazonaws.com/") || !strings.Contains(queueURL, "sqs.") {
		return ""
	}

	// Parse the URL to extract region
	u, err := url.Parse(queueURL)
	if err != nil {
		return ""
	}

	// Extract region from hostname (e.g., sqs.us-east-1.amazonaws.com)
	parts := strings.Split(u.Host, ".")
	if len(parts) < 3 || parts[0] != "sqs" {
		return ""
	}
	region := parts[1]

	// URL encode the queue URL for the fragment
	encodedQueueURL := url.QueryEscape(queueURL)

	// Construct console URL with region subdomain
	return fmt.Sprintf("https://%s.console.aws.amazon.com/sqs/v3/home?region=%s#/queues/%s",
		region, region, encodedQueueURL)
}
