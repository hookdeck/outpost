package destaws

import (
	"context"
	"encoding/json"
	"fmt"

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

type AWSDestination struct {
	*destregistry.BaseProvider
}

type AWSDestinationConfig struct {
	Endpoint string // optional
	QueueURL string
}

type AWSDestinationCredentials struct {
	Key     string
	Secret  string
	Session string // optional
}

var _ destregistry.Provider = (*AWSDestination)(nil)

func New(loader *metadata.MetadataLoader) (*AWSDestination, error) {
	base, err := destregistry.NewBaseProvider(loader, "aws")
	if err != nil {
		return nil, err
	}

	return &AWSDestination{
		BaseProvider: base,
	}, nil
}

func (d *AWSDestination) Validate(ctx context.Context, destination *models.Destination) error {
	_, _, err := d.resolveMetadata(ctx, destination)
	if err != nil {
		return err
	}
	return nil
}

func (p *AWSDestination) CreatePublisher(ctx context.Context, destination *models.Destination) (destregistry.Publisher, error) {
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

	sqsClient := sqs.NewFromConfig(sdkConfig, func(o *sqs.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = awssdk.String(cfg.Endpoint)
		}
	})

	return &AWSPublisher{
		BasePublisher: &destregistry.BasePublisher{},
		client:        sqsClient,
		queueURL:      cfg.QueueURL,
	}, nil
}

func (d *AWSDestination) resolveMetadata(ctx context.Context, destination *models.Destination) (*AWSDestinationConfig, *AWSDestinationCredentials, error) {
	if err := d.BaseProvider.Validate(ctx, destination); err != nil {
		return nil, nil, err
	}

	return &AWSDestinationConfig{
			Endpoint: destination.Config["endpoint"],
			QueueURL: destination.Config["queue_url"],
		}, &AWSDestinationCredentials{
			Key:     destination.Credentials["key"],
			Secret:  destination.Credentials["secret"],
			Session: destination.Credentials["session"],
		}, nil
}

type AWSPublisher struct {
	*destregistry.BasePublisher
	client   *sqs.Client
	queueURL string
}

func (p *AWSPublisher) Close() error {
	p.BasePublisher.StartClose()
	return nil
}

func (p *AWSPublisher) Publish(ctx context.Context, event *models.Event) error {
	if err := p.BasePublisher.StartPublish(); err != nil {
		return err
	}
	defer p.BasePublisher.FinishPublish()

	dataBytes, err := json.Marshal(event.Data)
	if err != nil {
		return err
	}

	attrs := make(map[string]types.MessageAttributeValue)
	for k, v := range event.Metadata {
		attrs[k] = types.MessageAttributeValue{
			DataType:    aws.String("String"),
			StringValue: aws.String(v),
		}
	}

	if _, err = p.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:          awssdk.String(p.queueURL),
		MessageBody:       awssdk.String(string(dataBytes)),
		MessageAttributes: attrs,
	}); err != nil {
		return err
	}

	return nil
}
