package mqs_test

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGCPPubSubQueue_CredentialType(t *testing.T) {
	newQueue := func(credentials string) *mqs.GCPPubSubQueue {
		return mqs.NewGCPPubSubQueue(&mqs.GCPPubSubConfig{
			ProjectID:                 "test-project",
			TopicID:                   "test-topic",
			SubscriptionID:            "test-subscription",
			ServiceAccountCredentials: credentials,
		}, time.Second)
	}

	t.Run("service account key", func(t *testing.T) {
		q := newQueue(testutil.GCPServiceAccountKeyJSON(t))
		cleanup, err := q.Init(context.Background())
		require.NoError(t, err)
		cleanup()
	})

	nonServiceAccount := []struct {
		name        string
		credentials string
	}{
		{"other credential type", `{"type":"authorized_user","client_id":"id","client_secret":"secret","refresh_token":"token"}`},
		{"type key in different case overrides service_account", `{"type":"service_account","Type":"authorized_user","client_id":"id","client_secret":"secret","refresh_token":"token"}`},
	}
	for _, tc := range nonServiceAccount {
		t.Run(tc.name, func(t *testing.T) {
			q := newQueue(tc.credentials)

			_, err := q.Init(context.Background())
			assert.ErrorContains(t, err, "service_account")

			_, err = q.Subscribe(context.Background())
			assert.ErrorContains(t, err, "service_account")
		})
	}
}
