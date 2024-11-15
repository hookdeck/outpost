package aws

import (
	"context"
	"encoding/json"
	"errors"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/hookdeck/outpost/internal/destinationadapter/adapters"
)

type AWSDestination struct {
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

var _ adapters.DestinationAdapter = (*AWSDestination)(nil)

func New() *AWSDestination {
	return &AWSDestination{}
}

func (d *AWSDestination) Validate(ctx context.Context, destination adapters.DestinationAdapterValue) error {
	_, err := parseConfig(destination)
	if err != nil {
		return err
	}
	_, err = parseCredentials(destination)
	return err
}

func (d *AWSDestination) Publish(ctx context.Context, destination adapters.DestinationAdapterValue, event *adapters.Event) error {
	config, err := parseConfig(destination)
	if err != nil {
		return err
	}
	credentials, err := parseCredentials(destination)
	if err != nil {
		return err
	}
	return publishEvent(ctx, config, credentials, event)
}

func parseConfig(destination adapters.DestinationAdapterValue) (*AWSDestinationConfig, error) {
	if destination.Type != "aws" {
		return nil, errors.New("invalid destination type")
	}

	destinationConfig := &AWSDestinationConfig{
		Endpoint: destination.Config["endpoint"],
		QueueURL: destination.Config["queue_url"],
	}

	if destinationConfig.QueueURL == "" {
		return nil, errors.New("queue_url is required for aws destination config")
	}

	return destinationConfig, nil
}

func parseCredentials(destination adapters.DestinationAdapterValue) (*AWSDestinationCredentials, error) {
	if destination.Type != "aws" {
		return nil, errors.New("invalid destination type")
	}

	destinationCredentials := &AWSDestinationCredentials{
		Key:     destination.Credentials["key"],
		Secret:  destination.Credentials["secret"],
		Session: destination.Credentials["session"],
	}

	if destinationCredentials.Key == "" {
		return nil, errors.New("key is required for aws destination credentials")
	}

	if destinationCredentials.Secret == "" {
		return nil, errors.New("secret is required for aws destination credentials")
	}

	return destinationCredentials, nil
}

func publishEvent(ctx context.Context, cfg *AWSDestinationConfig, creds *AWSDestinationCredentials, event *adapters.Event) error {
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
