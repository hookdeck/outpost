package config_test

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublishMQConfig_GetInfraType_AWSSQS(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cfg  config.PublishAWSSQSConfig
		want string
	}{
		{
			name: "region only (IAM role via default credential chain)",
			cfg:  config.PublishAWSSQSConfig{Region: "us-east-1"},
			want: "awssqs",
		},
		{
			name: "region with static keys",
			cfg:  config.PublishAWSSQSConfig{AccessKeyID: "AKID", SecretAccessKey: "SECRET", Region: "us-east-1"},
			want: "awssqs",
		},
		{
			name: "keys without region",
			cfg:  config.PublishAWSSQSConfig{AccessKeyID: "AKID", SecretAccessKey: "SECRET"},
			want: "",
		},
		{
			name: "empty",
			cfg:  config.PublishAWSSQSConfig{},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.PublishMQConfig{AWSSQS: tt.cfg}
			assert.Equal(t, tt.want, cfg.GetInfraType())
		})
	}
}

func TestPublishMQConfig_Validate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		cfg     config.PublishMQConfig
		wantErr bool
	}{
		{
			name: "aws sqs both keys set",
			cfg:  config.PublishMQConfig{AWSSQS: config.PublishAWSSQSConfig{AccessKeyID: "AKID", SecretAccessKey: "SECRET", Region: "us-east-1"}},
		},
		{
			name: "aws sqs both keys empty (default credential chain)",
			cfg:  config.PublishMQConfig{AWSSQS: config.PublishAWSSQSConfig{Region: "us-east-1"}},
		},
		{
			name:    "aws sqs only access key set",
			cfg:     config.PublishMQConfig{AWSSQS: config.PublishAWSSQSConfig{AccessKeyID: "AKID", Region: "us-east-1"}},
			wantErr: true,
		},
		{
			name:    "aws sqs only secret key set",
			cfg:     config.PublishMQConfig{AWSSQS: config.PublishAWSSQSConfig{SecretAccessKey: "SECRET", Region: "us-east-1"}},
			wantErr: true,
		},
		{
			name: "no publish provider configured",
			cfg:  config.PublishMQConfig{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.cfg.Validate()
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestPublishMQConfig_ProxyURL(t *testing.T) {
	t.Parallel()
	const proxy = "http://user:pass@proxy:10000"

	t.Run("rabbitmq publish queue dials through the proxy", func(t *testing.T) {
		t.Parallel()
		cfg := config.PublishMQConfig{
			RabbitMQ: config.PublishRabbitMQConfig{ServerURL: "amqp://broker:5672", Queue: "q"},
			ProxyURL: proxy,
		}
		require.NotNil(t, cfg.GetQueueConfig().RabbitMQ.Dial)
		assert.False(t, cfg.ProxyIgnored())
	})

	t.Run("unset proxy keeps the direct dial", func(t *testing.T) {
		t.Parallel()
		cfg := config.PublishMQConfig{
			RabbitMQ: config.PublishRabbitMQConfig{ServerURL: "amqp://broker:5672", Queue: "q"},
			ProxyURL: " \n",
		}
		assert.Nil(t, cfg.GetQueueConfig().RabbitMQ.Dial)
		assert.False(t, cfg.ProxyIgnored())
	})

	t.Run("invalid proxy fails the dial instead of connecting directly", func(t *testing.T) {
		t.Parallel()
		cfg := config.PublishMQConfig{
			RabbitMQ: config.PublishRabbitMQConfig{ServerURL: "amqp://broker:5672", Queue: "q"},
			ProxyURL: "socks5://proxy:1080",
		}
		dial := cfg.GetQueueConfig().RabbitMQ.Dial
		require.NotNil(t, dial)
		_, err := dial(context.Background(), "tcp", "broker:5672")
		require.Error(t, err)
	})

	for name, cfg := range map[string]config.PublishMQConfig{
		"awssqs":          {AWSSQS: config.PublishAWSSQSConfig{Region: "us-east-1", Queue: "q"}},
		"gcppubsub":       {GCPPubSub: config.PublishGCPPubSubConfig{Project: "p", Topic: "t", Subscription: "s"}},
		"azureservicebus": {AzureServiceBus: config.PublishAzureServiceBusConfig{ConnectionString: "cs", Topic: "t", Subscription: "s"}},
	} {
		t.Run(name+" ignores the proxy", func(t *testing.T) {
			t.Parallel()
			cfg.ProxyURL = proxy
			require.NoError(t, cfg.Validate())
			require.NotNil(t, cfg.GetQueueConfig())
			assert.True(t, cfg.ProxyIgnored())
		})
	}

	t.Run("no publish provider does not report the proxy as ignored", func(t *testing.T) {
		t.Parallel()
		cfg := config.PublishMQConfig{ProxyURL: proxy}
		assert.False(t, cfg.ProxyIgnored())
	})
}

func TestPublishProxyURL_InternalMQsConnectDirectly(t *testing.T) {
	t.Parallel()
	c := &config.Config{}
	c.InitDefaults()
	c.MQs.RabbitMQ.ServerURL = "amqp://internal:5672"
	c.PublishMQ.RabbitMQ.ServerURL = "amqp://publish:5672"
	c.PublishMQ.RabbitMQ.Queue = "publish"
	c.PublishMQ.ProxyURL = "http://proxy:10000"

	for _, queueType := range []string{"deliverymq", "logmq"} {
		qc, err := c.MQs.ToQueueConfig(context.Background(), queueType)
		require.NoError(t, err)
		require.NotNil(t, qc.RabbitMQ)
		assert.Nil(t, qc.RabbitMQ.Dial, queueType)
	}
	require.NotNil(t, c.PublishMQ.GetQueueConfig().RabbitMQ.Dial)
}
