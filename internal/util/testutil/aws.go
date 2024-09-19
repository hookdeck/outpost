package testutil

import (
	"context"
	"errors"
	"log"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/smithy-go"
	"github.com/hookdeck/EventKit/internal/mqs"
)

func DeclareTestAWSInfrastructure(ctx context.Context, cfg *mqs.AWSSQSConfig) (string, error) {
	creds, err := cfg.ToCredentials()
	if err != nil {
		return "", err
	}

	sdkConfig, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(creds),
	)
	if err != nil {
		return "", err
	}

	sqsClient := sqs.NewFromConfig(sdkConfig, func(o *sqs.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
	})

	queueURL, err := ensureQueue(ctx, sqsClient, cfg.Topic)
	if err != nil {
		return "", err
	}
	return queueURL, nil
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
				log.Println("Queue does not exist, creating...")
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
