package deliverymq

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/smithy-go"
	"github.com/hookdeck/EventKit/internal/mqs"
)

type DeliveryAWSInfra struct {
	config *mqs.AWSSQSConfig
}

var _ DeliveryInfra = &DeliveryAWSInfra{}

func (i *DeliveryAWSInfra) DeclareInfrastructure(ctx context.Context) error {
	creds, err := i.config.ToCredentials()
	if err != nil {
		return err
	}

	sdkConfig, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(i.config.Region),
		config.WithCredentialsProvider(creds),
	)
	if err != nil {
		return err
	}

	sqsClient := sqs.NewFromConfig(sdkConfig, func(o *sqs.Options) {
		if i.config.Endpoint != "" {
			o.BaseEndpoint = aws.String(i.config.Endpoint)
		}
	})

	_, err = ensureQueue(ctx, sqsClient, i.config.Topic)
	return err
}

func NewDeliveryAWSInfra(config *mqs.AWSSQSConfig) *DeliveryAWSInfra {
	return &DeliveryAWSInfra{
		config: config,
	}
}

func ensureQueue(ctx context.Context, sqsClient *sqs.Client, queueName string) (string, error) {
	queue, err := sqsClient.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{
		QueueName: aws.String(queueName),
	})
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			switch apiErr.(type) {
			case *types.QueueDoesNotExist:
				createdQueue, err := sqsClient.CreateQueue(ctx, &sqs.CreateQueueInput{
					QueueName: aws.String(queueName),
				})
				if err != nil {
					return "", err
				}
				return *createdQueue.QueueUrl, nil
			default:
				return "", err
			}
		}
		return "", err
	}
	return *queue.QueueUrl, nil
}
