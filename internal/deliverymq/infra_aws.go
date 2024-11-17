package deliverymq

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/awsutil"
)

type DeliveryAWSInfra struct {
	config *mqs.AWSSQSConfig
}

func (i *DeliveryAWSInfra) DeclareInfrastructure(ctx context.Context) error {
	sqsClient, err := awsutil.SQSClientFromConfig(ctx, i.config)
	if err != nil {
		return err
	}
	if _, err := awsutil.EnsureQueue(ctx, sqsClient, i.config.Topic, createQueue); err != nil {
		return err
	}
	return nil
}

func NewDeliveryAWSInfra(config *mqs.AWSSQSConfig) DeliveryInfra {
	return &DeliveryAWSInfra{
		config: config,
	}
}

func createQueue(ctx context.Context, sqsClient *sqs.Client, queueName string) (*sqs.CreateQueueOutput, error) {
	return sqsClient.CreateQueue(ctx, &sqs.CreateQueueInput{
		QueueName: aws.String(queueName),
	})
}
