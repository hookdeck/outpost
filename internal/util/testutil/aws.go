package testutil

import (
	"context"

	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/awsutil"
)

func DeclareTestAWSInfrastructure(ctx context.Context, cfg *mqs.AWSSQSConfig, attributes map[string]string) (string, error) {
	sqsClient, err := awsutil.SQSClientFromConfig(ctx, cfg)
	if err != nil {
		return "", err
	}
	queueURL, err := awsutil.EnsureQueue(ctx, sqsClient, cfg.Topic, awsutil.MakeCreateQueue(attributes))
	if err != nil {
		return "", err
	}
	return queueURL, nil
}
