package destaws

import (
	"context"
	"encoding/json"

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
	sqsClient *sqs.Client
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
	cfg, creds, err := d.resolveMetadata(ctx, destination)
	if err != nil {
		return err
	}
	if d.sqsClient == nil {
		sdkConfig, err := config.LoadDefaultConfig(ctx,
			// TODO: use proper credentials
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(creds.Key, creds.Secret, creds.Session)),
		)
		if err != nil {
			return err
		}

		d.sqsClient = sqs.NewFromConfig(sdkConfig, func(o *sqs.Options) {
			if cfg.Endpoint != "" {
				o.BaseEndpoint = awssdk.String(cfg.Endpoint)
			}
		})
	}
	return nil
}

func (d *AWSDestination) Publish(ctx context.Context, destination *models.Destination, event *models.Event) error {
	config, credentials, err := d.resolveMetadata(ctx, destination)
	if err != nil {
		return destregistry.NewErrDestinationPublish(err)
	}
	if err := publishEvent(ctx, config, credentials, event); err != nil {
		return destregistry.NewErrDestinationPublish(err)
	}
	return nil
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

func publishEvent(ctx context.Context, cfg *AWSDestinationConfig, creds *AWSDestinationCredentials, event *models.Event) error {
	dataBytes, err := json.Marshal(event.Data)
	if err != nil {
		return err
	}

	sdkConfig, err := config.LoadDefaultConfig(ctx,
		// TODO: use proper credentials
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(creds.Key, creds.Secret, creds.Session)),
	)
	if err != nil {
		return err
	}

	sqsClient := sqs.NewFromConfig(sdkConfig, func(o *sqs.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = awssdk.String(cfg.Endpoint)
		}
	})

	attrs := make(map[string]types.MessageAttributeValue)
	for k, v := range event.Metadata {
		attrs[k] = types.MessageAttributeValue{
			DataType:    aws.String("String"),
			StringValue: aws.String(v),
		}
	}

	_, err = sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:          awssdk.String(cfg.QueueURL),
		MessageBody:       awssdk.String(string(dataBytes)),
		MessageAttributes: attrs,
	})
	return err
}
