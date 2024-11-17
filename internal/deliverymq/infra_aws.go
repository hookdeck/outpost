package deliverymq

import (
	"context"

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

	if _, err := awsutil.EnsureQueue(ctx, sqsClient, i.config.Topic, awsutil.MakeCreateQueue(nil)); err != nil {
		return err
	}
	return nil

	// TODO: continue implementation // testing
	// queueName := i.config.Topic
	// dlqName := i.config.Topic + "_dlq"

	// // dlq
	// dlqURL, err := awsutil.EnsureQueue(ctx, sqsClient, dlqName, awsutil.MakeCreateQueue(nil))
	// if err != nil {
	// 	return err
	// }

	// // deliverymq
	// dlqARN, err := awsutil.GetQueueARN(ctx, sqsClient, dlqURL)
	// if err != nil {
	// 	return err
	// }
	// maxReceiveCount := "5"
	// deliverymqAttributes := map[string]string{
	// 	"RedrivePolicy": `{"deadLetterTargetArn":"` + dlqARN + `","maxReceiveCount":"` + maxReceiveCount + `"}`,
	// }
	// if _, err := awsutil.EnsureQueue(ctx, sqsClient, queueName, awsutil.MakeCreateQueue(deliverymqAttributes)); err != nil {
	// 	return err
	// }

	// return nil
}

func NewDeliveryAWSInfra(config *mqs.AWSSQSConfig) DeliveryInfra {
	return &DeliveryAWSInfra{
		config: config,
	}
}
