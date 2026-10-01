package services_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/services"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Each consumer worker subscribes with its own queue's byte limit, and with
// none when the limit is off.
func TestSupervisedConsumerWorker_MaxBytes(t *testing.T) {
	const (
		publishBytes  = 1 << 20
		deliveryBytes = 2 << 20
	)
	cases := []struct {
		key string
		on  int64
	}{
		{key: config.SupervisorWorkerPublishMQ, on: publishBytes},
		{key: config.SupervisorWorkerDeliveryMQ, on: deliveryBytes},
		{key: config.SupervisorWorkerLogMQ, on: 0},
	}
	for _, tc := range cases {
		for _, restart := range []bool{false, true} {
			for _, limits := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/restart=%t/limits=%t", tc.key, restart, limits), func(t *testing.T) {
					cfg := supervisorConfig()
					if restart {
						cfg = supervisorConfig(tc.key)
					}
					want := int64(0)
					if limits {
						cfg.PublishMaxConcurrencyBytes = publishBytes
						cfg.DeliveryMaxConcurrencyBytes = deliveryBytes
						want = tc.on
					}

					errSubscribe := errors.New("subscribe")
					var got mqs.SubscribeOptions
					subscribe := func(ctx context.Context, opts ...mqs.SubscribeOption) (mqs.Subscription, error) {
						got = mqs.ApplySubscribeOptions(opts)
						return nil, errSubscribe
					}

					w, _ := services.NewSupervisedConsumerWorker(cfg, tc.key, tc.key+"-consumer",
						subscribe, handlerFunc(nil), 7, testutil.CreateTestLogger(t))
					require.ErrorIs(t, w.Run(context.Background()), errSubscribe)

					assert.Equal(t, 7, got.Concurrency)
					assert.Equal(t, want, got.MaxBytes)
				})
			}
		}
	}
}
