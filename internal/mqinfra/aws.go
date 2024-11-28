package mqinfra

import (
	"context"
	"errors"

	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/awsutil"
)

type infraAWSSQS struct {
	cfg *mqs.QueueConfig
}

func (infra *infraAWSSQS) Declare(ctx context.Context) error {
	if infra.cfg == nil || infra.cfg.AWSSQS == nil {
		return errors.New("failed assertion: cfg.AWSSQS != nil") // IMPOSSIBLE
	}

	sqsClient, err := awsutil.SQSClientFromConfig(ctx, infra.cfg.AWSSQS)
	if err != nil {
		return err
	}

	if _, err := awsutil.EnsureQueue(ctx, sqsClient, infra.cfg.AWSSQS.Topic, awsutil.MakeCreateQueue(nil)); err != nil {
		return err
	}

	return nil
}

func (infra *infraAWSSQS) TearDown(ctx context.Context) error {
	if infra.cfg == nil || infra.cfg.AWSSQS == nil {
		return errors.New("failed assertion: cfg.AWSSQS != nil") // IMPOSSIBLE
	}

	sqsClient, err := awsutil.SQSClientFromConfig(ctx, infra.cfg.AWSSQS)
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

	return nil
}
