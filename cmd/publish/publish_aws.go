package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/smithy-go"
)

const (
	AWSRegion        = "eu-central-1"
	AWSEndpoint      = "http://localhost:4566"
	PublishQueueName = "publish_sqs_queue"
)

func publishAWS(body map[string]interface{}) error {
	log.Printf("[x] Publishing AWS")

	ctx := context.Background()
	sqsClient, err := createSQSClient(context.Background())
	if err != nil {
		return err
	}

	queueURL, err := getQueue(ctx, sqsClient, PublishQueueName)
	if err != nil {
		return err
	}

	messageBody, err := json.Marshal(body)
	if err != nil {
		return err
	}

	sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    aws.String(queueURL),
		MessageBody: aws.String(string(messageBody)),
	})

	return nil
}

func declareAWS() error {
	log.Printf("[*] Declaring AWS Publish infra")
	sqsClient, err := createSQSClient(context.Background())
	if err != nil {
		return err
	}
	_, err = ensureQueue(context.Background(), sqsClient, PublishQueueName)
	return err
}

func createSQSClient(ctx context.Context) (*sqs.Client, error) {
	sdkConfig, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(AWSRegion),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	if err != nil {
		return nil, err
	}

	sqsClient := sqs.NewFromConfig(sdkConfig, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(AWSEndpoint)
	})

	return sqsClient, nil
}

func getQueue(ctx context.Context, sqsClient *sqs.Client, queueName string) (string, error) {
	queue, err := sqsClient.GetQueueUrl(ctx, &sqs.GetQueueUrlInput{
		QueueName: aws.String(queueName),
	})
	if err != nil {
		return "", err
	}
	return *queue.QueueUrl, nil
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
