package config_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNATSConfig_DLQName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		cfg       config.NATSConfig
		queueType string
		want      string
	}{
		{
			name:      "delivery falls back to derived name",
			cfg:       config.NATSConfig{DeliverySubject: "outpost-delivery"},
			queueType: "deliverymq",
			want:      "dlq.outpost-delivery",
		},
		{
			name:      "log falls back to derived name",
			cfg:       config.NATSConfig{LogSubject: "outpost-log"},
			queueType: "logmq",
			want:      "dlq.outpost-log",
		},
		{
			name:      "delivery override wins",
			cfg:       config.NATSConfig{DeliverySubject: "outpost-delivery", DeliveryDLQ: "dead-letter.outpost-delivery"},
			queueType: "deliverymq",
			want:      "dead-letter.outpost-delivery",
		},
		{
			name:      "log override wins",
			cfg:       config.NATSConfig{LogSubject: "outpost-log", LogDLQ: "dead-letter.outpost-log"},
			queueType: "logmq",
			want:      "dead-letter.outpost-log",
		},
		{
			name:      "log override does not leak into delivery",
			cfg:       config.NATSConfig{DeliverySubject: "outpost-delivery", LogDLQ: "dead-letter.outpost-log"},
			queueType: "deliverymq",
			want:      "dlq.outpost-delivery",
		},
		{
			name:      "no subject yields no dlq name",
			cfg:       config.NATSConfig{},
			queueType: "deliverymq",
			want:      "",
		},
		{
			name:      "unknown queue type yields no dlq name",
			cfg:       config.NATSConfig{DeliverySubject: "outpost-delivery"},
			queueType: "somethingelse",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			infraCfg := tt.cfg.ToInfraConfig(tt.queueType)
			require.NotNil(t, infraCfg.NATS)
			assert.Equal(t, tt.want, infraCfg.NATS.DLQ)
		})
	}
}

func TestNATSConfig_IsConfigured(t *testing.T) {
	t.Parallel()

	assert.True(t, (&config.NATSConfig{ServerURL: "nats://localhost:4222"}).IsConfigured())
	assert.False(t, (&config.NATSConfig{}).IsConfigured())
}

func TestNATSConfig_GetProviderType(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "nats", (&config.NATSConfig{}).GetProviderType())
}
