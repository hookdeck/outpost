package mqinfra

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/aws/smithy-go"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/awsutil"
)

type infraAWSSQS struct {
	cfg *MQInfraConfig
}

func (infra *infraAWSSQS) Exist(ctx context.Context) (bool, error) {
	if infra.cfg == nil || infra.cfg.AWSSQS == nil {
		return false, errors.New("failed assertion: cfg.AWSSQS != nil") // IMPOSSIBLE
	}

	sqsClient, err := awsutil.SQSClientFromConfig(ctx, &mqs.AWSSQSConfig{
		Endpoint:                  infra.cfg.AWSSQS.Endpoint,
		Region:                    infra.cfg.AWSSQS.Region,
		ServiceAccountCredentials: infra.cfg.AWSSQS.ServiceAccountCredentials,
		Topic:                     infra.cfg.AWSSQS.Topic,
	})
	if err != nil {
		return false, err
	}

	// Check if main queue exists
	_, err = awsutil.RetrieveQueueURL(ctx, sqsClient, infra.cfg.AWSSQS.Topic)
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			switch apiErr.(type) {
			case *types.QueueDoesNotExist:
				return false, nil
			default:
				return false, err
			}
		}
		return false, err
	}

	// Check if DLQ exists
	dlqName := infra.cfg.AWSSQS.Topic + "-dlq"
	_, err = awsutil.RetrieveQueueURL(ctx, sqsClient, dlqName)
	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) {
			switch apiErr.(type) {
			case *types.QueueDoesNotExist:
				return false, nil
			default:
				return false, err
			}
		}
		return false, err
	}

	return true, nil
}

func (infra *infraAWSSQS) Declare(ctx context.Context) error {
	if infra.cfg == nil || infra.cfg.AWSSQS == nil {
		return errors.New("failed assertion: cfg.AWSSQS != nil") // IMPOSSIBLE
	}

	sqsClient, err := awsutil.SQSClientFromConfig(ctx, &mqs.AWSSQSConfig{
		Endpoint:                  infra.cfg.AWSSQS.Endpoint,
		Region:                    infra.cfg.AWSSQS.Region,
		ServiceAccountCredentials: infra.cfg.AWSSQS.ServiceAccountCredentials,
		Topic:                     infra.cfg.AWSSQS.Topic,
	})
	if err != nil {
		return err
	}

	attributes := map[string]string{}
	if infra.cfg.Policy.VisibilityTimeout > 0 {
		attributes["VisibilityTimeout"] = fmt.Sprintf("%d", infra.cfg.Policy.VisibilityTimeout)
	}

	dlqName := infra.cfg.AWSSQS.Topic + "-dlq"
	dlqURL, err := awsutil.EnsureQueue(ctx, sqsClient, dlqName, awsutil.MakeCreateQueue(attributes))
	if err != nil {
		return err
	}

	dlqArn, err := awsutil.RetrieveQueueARN(ctx, sqsClient, dlqURL)
	if err != nil {
		return err
	}

	attributesWithRedrivePolicy := attributes
	attributesWithRedrivePolicy["RedrivePolicy"] = fmt.Sprintf(`{"deadLetterTargetArn":"%s","maxReceiveCount":"%d"}`, dlqArn, infra.cfg.Policy.RetryLimit+1)

	if _, err := awsutil.EnsureQueue(ctx, sqsClient, infra.cfg.AWSSQS.Topic, awsutil.MakeCreateQueue(attributesWithRedrivePolicy)); err != nil {
		return err
	}

	return nil
}

func (infra *infraAWSSQS) TearDown(ctx context.Context) error {
	if infra.cfg == nil || infra.cfg.AWSSQS == nil {
		return errors.New("failed assertion: cfg.AWSSQS != nil") // IMPOSSIBLE
	}

	sqsClient, err := awsutil.SQSClientFromConfig(ctx, &mqs.AWSSQSConfig{
		Endpoint:                  infra.cfg.AWSSQS.Endpoint,
		Region:                    infra.cfg.AWSSQS.Region,
		ServiceAccountCredentials: infra.cfg.AWSSQS.ServiceAccountCredentials,
		Topic:                     infra.cfg.AWSSQS.Topic,
	})
	if err != nil {
		return err
	}

	queueURL, err := awsutil.RetrieveQueueURL(ctx, sqsClient, infra.cfg.AWSSQS.Topic)
	if err != nil {
		return err
	}

	if err := awsutil.DeleteQueue(ctx, sqsClient, queueURL); err != nil {
		return err
	}

	dlqURL, err := awsutil.RetrieveQueueURL(ctx, sqsClient, infra.cfg.AWSSQS.Topic+"-dlq")
	if err != nil {
		return err
	}

	if err := awsutil.DeleteQueue(ctx, sqsClient, dlqURL); err != nil {
		return err
	}

	return nil
}
