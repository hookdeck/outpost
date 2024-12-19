package destawssqs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

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

func New(loader metadata.MetadataLoader) (*AWSSQSDestination, error) {
	base, err := destregistry.NewBaseProvider(loader, "aws_sqs")
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
		BasePublisher: &destregistry.BasePublisher{},
		client:        sqsClient,
		queueURL:      cfg.QueueURL,
	}, nil
}

func (d *AWSSQSDestination) resolveMetadata(ctx context.Context, destination *models.Destination) (*AWSSQSDestinationConfig, *AWSSQSDestinationCredentials, error) {
	if err := d.BaseProvider.Validate(ctx, destination); err != nil {
		return nil, nil, err
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

	attrs := make(map[string]types.MessageAttributeValue)
	for k, v := range event.Metadata {
		attrs[k] = types.MessageAttributeValue{
			DataType:    aws.String("String"),
			StringValue: aws.String(v),
		}
	}

	return &sqs.SendMessageInput{
		QueueUrl:          awssdk.String(p.queueURL),
		MessageBody:       awssdk.String(string(dataBytes)),
		MessageAttributes: attrs,
	}, nil
}

func (p *AWSSQSPublisher) Publish(ctx context.Context, event *models.Event) error {
	if err := p.BasePublisher.StartPublish(); err != nil {
		return err
	}
	defer p.BasePublisher.FinishPublish()

	msg, err := p.Format(ctx, event)
	if err != nil {
		return err
	}

	if _, err = p.client.SendMessage(ctx, msg); err != nil {
		return destregistry.NewErrDestinationPublishAttempt(err, "aws_sqs", map[string]interface{}{
			"error": err.Error(),
		})
	}

	return nil
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
