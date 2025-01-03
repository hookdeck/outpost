package publishmq

import (
	"errors"
	"fmt"

	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/spf13/viper"
)

type Config struct {
	MQ *mqs.QueueConfig
}

type queueConfigParser interface {
	parseQueue() (*mqs.QueueConfig, error)
}

func ParseConfig(viper *viper.Viper) (*Config, error) {
	infraType := detectInfraType(viper)
	if infraType == "" {
		return nil, errors.New("no message queue infrastructure configured")
	}

	var parser queueConfigParser
	switch infraType {
	case "awssqs":
		parser = &awsSQSParser{viper: viper}
	case "rabbitmq":
		parser = &rabbitMQParser{viper: viper}
	default:
		return nil, fmt.Errorf("unsupported infrastructure type: %s", infraType)
	}

	mq, err := parser.parseQueue()
	if err != nil {
		return nil, fmt.Errorf("failed to parse publish queue config: %w", err)
	}

	return &Config{
		MQ: mq,
	}, nil
}

func detectInfraType(viper *viper.Viper) string {
	if viper.IsSet("PUBLISH_AWS_SQS_ACCESS_KEY_ID") && viper.GetString("PUBLISH_AWS_SQS_ACCESS_KEY_ID") != "" {
		return "awssqs"
	}
	if viper.IsSet("PUBLISH_RABBITMQ_SERVER_URL") && viper.GetString("PUBLISH_RABBITMQ_SERVER_URL") != "" {
		return "rabbitmq"
	}
	return ""
}

type awsSQSParser struct {
	viper *viper.Viper
}

func (p *awsSQSParser) parseQueue() (*mqs.QueueConfig, error) {
	queue := p.viper.GetString("PUBLISH_AWS_SQS_QUEUE")
	if queue == "" {
		return nil, errors.New("PUBLISH_AWS_SQS_QUEUE is not set")
	}

	creds := fmt.Sprintf("%s:%s:",
		p.viper.GetString("PUBLISH_AWS_SQS_ACCESS_KEY_ID"),
		p.viper.GetString("PUBLISH_AWS_SQS_SECRET_ACCESS_KEY"),
	)

	region := p.viper.GetString("PUBLISH_AWS_SQS_REGION")
	if region == "" {
		return nil, errors.New("PUBLISH_AWS_SQS_REGION is not set")
	}

	return &mqs.QueueConfig{
		AWSSQS: &mqs.AWSSQSConfig{
			Endpoint:                  p.viper.GetString("PUBLISH_AWS_SQS_ENDPOINT"),
			Region:                    region,
			ServiceAccountCredentials: creds,
			Topic:                     queue,
		},
	}, nil
}

type rabbitMQParser struct {
	viper *viper.Viper
}

func (p *rabbitMQParser) parseQueue() (*mqs.QueueConfig, error) {
	serverURL := p.viper.GetString("PUBLISH_RABBITMQ_SERVER_URL")
	if serverURL == "" {
		return nil, errors.New("PUBLISH_RABBITMQ_SERVER_URL is not set")
	}

	queue := p.viper.GetString("PUBLISH_RABBITMQ_QUEUE")
	if queue == "" {
		return nil, errors.New("PUBLISH_RABBITMQ_QUEUE is not set")
	}

	return &mqs.QueueConfig{
		RabbitMQ: &mqs.RabbitMQConfig{
			ServerURL: serverURL,
			Exchange:  p.viper.GetString("PUBLISH_RABBITMQ_EXCHANGE"),
			Queue:     queue,
		},
	}, nil
}
