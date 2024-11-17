package logmq

import (
	"context"

	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/awsutil"
)

type LogAWSInfra struct {
	config *mqs.AWSSQSConfig
}

func (i *LogAWSInfra) DeclareInfrastructure(ctx context.Context) error {
	sqsClient, err := awsutil.SQSClientFromConfig(ctx, i.config)
	if err != nil {
		return err
	}
	if _, err := awsutil.EnsureQueue(ctx, sqsClient, i.config.Topic, awsutil.MakeCreateQueue(nil)); err != nil {
		return err
	}
	return nil
}

func NewLogAWSInfra(config *mqs.AWSSQSConfig) LogInfra {
	return &LogAWSInfra{
		config: config,
	}
}
