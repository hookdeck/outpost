package config_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRabbitMQConfig_DLQName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		cfg       config.RabbitMQConfig
		queueType string
		want      string
	}{
		{
			name:      "delivery falls back to derived name",
			cfg:       config.RabbitMQConfig{DeliveryQueue: "outpost-delivery"},
			queueType: "deliverymq",
			want:      "outpost-delivery.dlq",
		},
		{
			name:      "log falls back to derived name",
			cfg:       config.RabbitMQConfig{LogQueue: "outpost-log"},
			queueType: "logmq",
			want:      "outpost-log.dlq",
		},
		{
			name:      "delivery override wins",
			cfg:       config.RabbitMQConfig{DeliveryQueue: "outpost-delivery", DeliveryDLQ: "dead_letter-outpost-delivery"},
			queueType: "deliverymq",
			want:      "dead_letter-outpost-delivery",
		},
		{
			name:      "log override wins",
			cfg:       config.RabbitMQConfig{LogQueue: "outpost-log", LogDLQ: "dead_letter-outpost-log"},
			queueType: "logmq",
			want:      "dead_letter-outpost-log",
		},
		{
			name:      "log override does not leak into delivery",
			cfg:       config.RabbitMQConfig{DeliveryQueue: "outpost-delivery", LogDLQ: "dead_letter-outpost-log"},
			queueType: "deliverymq",
			want:      "outpost-delivery.dlq",
		},
		{
			name:      "no queue name yields no dlq name",
			cfg:       config.RabbitMQConfig{},
			queueType: "deliverymq",
			want:      "",
		},
		{
			name:      "unknown queue type yields no dlq name",
			cfg:       config.RabbitMQConfig{DeliveryQueue: "outpost-delivery"},
			queueType: "somethingelse",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			infraCfg := tt.cfg.ToInfraConfig(tt.queueType)
			require.NotNil(t, infraCfg.RabbitMQ)
			assert.Equal(t, tt.want, infraCfg.RabbitMQ.DLQ)
		})
	}
}
