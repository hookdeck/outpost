package mqinfra

import (
	"errors"
	"fmt"

	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/spf13/viper"
)

type Config struct {
	DeliveryMQ *mqs.QueueConfig
	LogMQ      *mqs.QueueConfig
}

type queueConfigParser interface {
	parseQueue(queueType string) (*mqs.QueueConfig, error)
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

	deliveryMQ, err := parser.parseQueue("DELIVERY")
	if err != nil {
		return nil, fmt.Errorf("failed to parse delivery queue config: %w", err)
	}
	deliveryMQ.Policy.RetryLimit = viper.GetInt("DELIVERY_RETRY_LIMIT")

	logMQ, err := parser.parseQueue("LOG")
	if err != nil {
		return nil, fmt.Errorf("failed to parse log queue config: %w", err)
	}
	logMQ.Policy.RetryLimit = viper.GetInt("LOG_RETRY_LIMIT")

	return &Config{
		DeliveryMQ: deliveryMQ,
		LogMQ:      logMQ,
	}, nil
}

func detectInfraType(viper *viper.Viper) string {
	if viper.IsSet("AWS_SQS_ACCESS_KEY_ID") && viper.GetString("AWS_SQS_ACCESS_KEY_ID") != "" {
		return "awssqs"
	}
	if viper.IsSet("RABBITMQ_SERVER_URL") && viper.GetString("RABBITMQ_SERVER_URL") != "" {
		return "rabbitmq"
	}
	return ""
}
