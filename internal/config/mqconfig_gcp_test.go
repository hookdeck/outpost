package config_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGCPPubSubConfig_DLQNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		cfg       config.GCPPubSubConfig
		queueType string
		wantTopic string
		wantSub   string
	}{
		{
			name:      "delivery falls back to derived names",
			cfg:       config.GCPPubSubConfig{Project: "p", DeliveryTopic: "outpost-delivery"},
			queueType: "deliverymq",
			wantTopic: "outpost-delivery-dlq",
			wantSub:   "outpost-delivery-dlq-sub",
		},
		{
			name:      "log falls back to derived names",
			cfg:       config.GCPPubSubConfig{Project: "p", LogTopic: "outpost-log"},
			queueType: "logmq",
			wantTopic: "outpost-log-dlq",
			wantSub:   "outpost-log-dlq-sub",
		},
		{
			name:      "topic and subscription overrides both win",
			cfg:       config.GCPPubSubConfig{Project: "p", DeliveryTopic: "outpost-delivery", DeliveryDLQTopic: "dead_letter-delivery", DeliveryDLQSubscription: "dead_letter-delivery-consumer"},
			queueType: "deliverymq",
			wantTopic: "dead_letter-delivery",
			wantSub:   "dead_letter-delivery-consumer",
		},
		{
			name:      "derived subscription follows an overridden dlq topic",
			cfg:       config.GCPPubSubConfig{Project: "p", DeliveryTopic: "outpost-delivery", DeliveryDLQTopic: "dead_letter-delivery"},
			queueType: "deliverymq",
			wantTopic: "dead_letter-delivery",
			wantSub:   "dead_letter-delivery-sub",
		},
		{
			name:      "subscription override alone leaves topic derived",
			cfg:       config.GCPPubSubConfig{Project: "p", DeliveryTopic: "outpost-delivery", DeliveryDLQSubscription: "dead_letter-delivery-consumer"},
			queueType: "deliverymq",
			wantTopic: "outpost-delivery-dlq",
			wantSub:   "dead_letter-delivery-consumer",
		},
		{
			name:      "log overrides do not leak into delivery",
			cfg:       config.GCPPubSubConfig{Project: "p", DeliveryTopic: "outpost-delivery", LogDLQTopic: "dead_letter-log", LogDLQSubscription: "dead_letter-log-consumer"},
			queueType: "deliverymq",
			wantTopic: "outpost-delivery-dlq",
			wantSub:   "outpost-delivery-dlq-sub",
		},
		{
			name:      "no topic name yields no dlq names",
			cfg:       config.GCPPubSubConfig{Project: "p"},
			queueType: "deliverymq",
			wantTopic: "",
			wantSub:   "",
		},
		{
			name:      "unknown queue type yields no dlq names",
			cfg:       config.GCPPubSubConfig{Project: "p", DeliveryTopic: "outpost-delivery"},
			queueType: "somethingelse",
			wantTopic: "",
			wantSub:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			infraCfg := tt.cfg.ToInfraConfig(tt.queueType)
			require.NotNil(t, infraCfg.GCPPubSub)
			assert.Equal(t, tt.wantTopic, infraCfg.GCPPubSub.DLQTopicID)
			assert.Equal(t, tt.wantSub, infraCfg.GCPPubSub.DLQSubscriptionID)
		})
	}
}
