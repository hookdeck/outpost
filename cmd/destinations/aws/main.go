package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/smithy-go"
)

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}

const DestinationQueueName = "destination_sqs_queue"

func run() error {
	ctx := context.Background()

	awsRegion := "eu-central-1"
	awsEndpoint := "http://localhost:4566"

	sdkConfig, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(awsRegion),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	if err != nil {
		return err
	}

	sqsClient := sqs.NewFromConfig(sdkConfig, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(awsEndpoint)
	})

	queueURL, err := ensureQueue(ctx, sqsClient, DestinationQueueName)
	if err != nil {
		return err
	}

	termChan := make(chan os.Signal, 1)
	signal.Notify(termChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		for {
			out, err := sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
				QueueUrl:              &queueURL,
				MaxNumberOfMessages:   1,
				VisibilityTimeout:     3,
				WaitTimeSeconds:       10,
				MessageAttributeNames: []string{"All"},
			})
			if err != nil {
				log.Printf("[*] error on recv: %v", err)
				continue
			}
			if len(out.Messages) == 0 {
				continue
			}
			for _, m := range out.Messages {
				log.Printf("[x] %s - %s\n", string(*m.Body), *m.MessageId)

				// Delete message (to ack)
				_, err = sqsClient.DeleteMessage(ctx, &sqs.DeleteMessageInput{
					QueueUrl:      &queueURL,
					ReceiptHandle: m.ReceiptHandle,
				})
				if err != nil {
					log.Printf("[x] error deleting message %s: %v", *m.MessageId, err)
					continue
				}
			}
		}
	}()

	log.Printf("[*] Ready to receive messages.\n\tEndpoint: %s\n\tQueue: %s", awsEndpoint, queueURL)
	log.Printf("[*] Waiting for logs. To exit press CTRL+C")
	<-termChan

	return nil
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
