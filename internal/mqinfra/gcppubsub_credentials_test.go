package mqinfra_test

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/mqinfra"
	"github.com/hookdeck/outpost/internal/util/gcpcreds"
	"github.com/stretchr/testify/assert"
)

func TestGCPPubSubInfra_CredentialType(t *testing.T) {
	nonServiceAccount := []struct {
		name        string
		credentials string
	}{
		{"other credential type", `{"type":"authorized_user","client_id":"id","client_secret":"secret","refresh_token":"token"}`},
		{"type key in different case overrides service_account", `{"type":"service_account","Type":"authorized_user","client_id":"id","client_secret":"secret","refresh_token":"token"}`},
	}
	for _, tc := range nonServiceAccount {
		t.Run(tc.name, func(t *testing.T) {
			infra := mqinfra.New(&mqinfra.MQInfraConfig{
				GCPPubSub: &mqinfra.GCPPubSubInfraConfig{
					ProjectID:                 "test-project",
					TopicID:                   "test-topic",
					SubscriptionID:            "test-subscription",
					ServiceAccountCredentials: tc.credentials,
				},
			})

			_, err := infra.Exist(context.Background())
			assert.ErrorIs(t, err, gcpcreds.ErrNotServiceAccount)
			assert.ErrorIs(t, infra.Declare(context.Background()), gcpcreds.ErrNotServiceAccount)
			assert.ErrorIs(t, infra.TearDown(context.Background()), gcpcreds.ErrNotServiceAccount)
		})
	}
}
