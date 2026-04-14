package opevents

import (
	"fmt"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/mqs"
	"go.uber.org/zap"
)

// Config holds the configuration for the operator events system.
// yaml/env tags live in internal/config; this is the domain-level struct.
type Config struct {
	Topics []string

	// Sink configs — at most one should be set. Presence determines sink type.
	HTTP      *HTTPSinkConfig
	AWSSQS    *AWSSQSSinkConfig
	GCPPubSub *GCPPubSubSinkConfig
	RabbitMQ  *RabbitMQSinkConfig
}

type HTTPSinkConfig struct {
	URL           string
	SigningSecret string `json:"-"`
}

type AWSSQSSinkConfig struct {
	QueueURL        string
	AccessKeyID     string `json:"-"`
	SecretAccessKey string `json:"-"`
	Region          string
	Endpoint        string // optional, for local dev
}

type GCPPubSubSinkConfig struct {
	ProjectID                 string
	TopicID                   string
	ServiceAccountCredentials string `json:"-"`
}

type RabbitMQSinkConfig struct {
	ServerURL string
	Exchange  string
}

// NewSink returns the appropriate Sink based on config.
// Returns NoopSink if no sink is configured.
// If topics are specified but no sink is configured, it logs a warning and
// returns NoopSink (operator events will be dropped).
func NewSink(cfg Config, logger *logging.Logger) (Sink, error) {
	if cfg.HTTP != nil {
		return NewHTTPSink(cfg.HTTP.URL, cfg.HTTP.SigningSecret), nil
	}
	if cfg.AWSSQS != nil {
		return newMQSinkFromAWSSQS(cfg.AWSSQS)
	}
	if cfg.GCPPubSub != nil {
		return newMQSinkFromGCPPubSub(cfg.GCPPubSub)
	}
	if cfg.RabbitMQ != nil {
		return newMQSinkFromRabbitMQ(cfg.RabbitMQ)
	}
	if len(cfg.Topics) > 0 {
		logger.Warn("opevents: topics are configured but no sink is set; operator events will be dropped", zap.Any("topics", cfg.Topics))
		return &NoopSink{}, nil
	}
	return &NoopSink{}, nil
}

func newMQSinkFromAWSSQS(cfg *AWSSQSSinkConfig) (*MQSink, error) {
	creds := fmt.Sprintf("%s:%s:", cfg.AccessKeyID, cfg.SecretAccessKey)
	queue := mqs.NewQueue(&mqs.QueueConfig{
		AWSSQS: &mqs.AWSSQSConfig{
			Endpoint:                  cfg.Endpoint,
			Region:                    cfg.Region,
			ServiceAccountCredentials: creds,
			Topic:                     cfg.QueueURL,
		},
	})
	return NewMQSink(queue), nil
}

func newMQSinkFromGCPPubSub(cfg *GCPPubSubSinkConfig) (*MQSink, error) {
	queue := mqs.NewQueue(&mqs.QueueConfig{
		GCPPubSub: &mqs.GCPPubSubConfig{
			ProjectID:                 cfg.ProjectID,
			TopicID:                   cfg.TopicID,
			ServiceAccountCredentials: cfg.ServiceAccountCredentials,
		},
	})
	return NewMQSink(queue), nil
}

func newMQSinkFromRabbitMQ(cfg *RabbitMQSinkConfig) (*MQSink, error) {
	queue := mqs.NewQueue(&mqs.QueueConfig{
		RabbitMQ: &mqs.RabbitMQConfig{
			ServerURL: cfg.ServerURL,
			Exchange:  cfg.Exchange,
		},
	})
	return NewMQSink(queue), nil
}
