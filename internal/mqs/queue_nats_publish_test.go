package mqs_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIntegrationMQ_NATS verifies that the NATS JetStream queue driver
// receives messages, surfaces them via Subscribe/Receive, and that Ack
// removes the message from the work queue.
func TestIntegrationMQ_NATSPublish(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	t.Cleanup(testinfra.Start(t))

	config := testinfra.NewMQNATSPublishConfig(t)
	queue := mqs.NewQueue(&config)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cleanup, err := queue.Init(ctx)
	require.NoError(t, err)
	defer cleanup()

	sub, err := queue.Subscribe(ctx)
	require.NoError(t, err)
	defer sub.Shutdown(ctx)

	payload, _ := json.Marshal(map[string]any{
		"tenant_id": "ignored-from-payload",
		"topic":     "user.created",
		"data":      map[string]any{"hello": "world"},
	})
	require.NoError(t, testutil.PublishToNATSStream(ctx, config.NATSPublish.Servers, config.NATSPublish.Accounts[0].Stream, payload))

	msg, err := sub.Receive(ctx)
	require.NoError(t, err)
	require.NotNil(t, msg)

	var got map[string]any
	require.NoError(t, json.Unmarshal(msg.Body, &got))
	assert.Equal(t, "user.created", got["topic"])
	// No TenantID override set on the account — payload value is preserved.
	assert.Equal(t, "ignored-from-payload", got["tenant_id"])
	msg.Ack()
}

// TestIntegrationMQ_NATS_TenantOverride verifies that when an account
// has TenantID set, the queue stamps that tenant_id on every event
// regardless of what the payload contained.
func TestIntegrationMQ_NATSPublish_TenantOverride(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	t.Cleanup(testinfra.Start(t))

	stream := "test-" + uuid.New().String()
	consumer := "test-" + uuid.New().String()
	config := mqs.QueueConfig{
		NATSPublish: &mqs.NATSPublishConfig{
			Servers: []string{testinfra.EnsureNATS(t)},
			Accounts: []mqs.NATSPublishAccountConfig{{
				Name:     "acme",
				Stream:   stream,
				Consumer: consumer,
				TenantID: "acme-tenant",
			}},
		},
	}
	require.NoError(t, testutil.DeclareTestNATSInfrastructure(context.Background(), config.NATSPublish))
	t.Cleanup(func() { _ = testutil.TeardownTestNATSInfrastructure(context.Background(), config.NATSPublish) })

	queue := mqs.NewQueue(&config)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cleanup, err := queue.Init(ctx)
	require.NoError(t, err)
	defer cleanup()

	sub, err := queue.Subscribe(ctx)
	require.NoError(t, err)
	defer sub.Shutdown(ctx)

	payload, _ := json.Marshal(map[string]any{
		"tenant_id": "spoofed-tenant",
		"topic":     "x.y",
	})
	require.NoError(t, testutil.PublishToNATSStream(ctx, config.NATSPublish.Servers, stream, payload))

	msg, err := sub.Receive(ctx)
	require.NoError(t, err)

	var got map[string]any
	require.NoError(t, json.Unmarshal(msg.Body, &got))
	assert.Equal(t, "acme-tenant", got["tenant_id"], "TenantID on account config must override payload")
	msg.Ack()
}

// TestIntegrationMQ_NATS_MultiAccount verifies that two accounts
// can be consumed in parallel and events end up tagged with the
// correct tenant_id.
func TestIntegrationMQ_NATSPublish_MultiAccount(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	t.Cleanup(testinfra.Start(t))

	streamA := "test-a-" + uuid.New().String()
	streamB := "test-b-" + uuid.New().String()
	config := mqs.QueueConfig{
		NATSPublish: &mqs.NATSPublishConfig{
			Servers: []string{testinfra.EnsureNATS(t)},
			Accounts: []mqs.NATSPublishAccountConfig{
				{Name: "acme", Stream: streamA, Consumer: "outpost", TenantID: "acme"},
				{Name: "globex", Stream: streamB, Consumer: "outpost", TenantID: "globex"},
			},
		},
	}
	require.NoError(t, testutil.DeclareTestNATSInfrastructure(context.Background(), config.NATSPublish))
	t.Cleanup(func() { _ = testutil.TeardownTestNATSInfrastructure(context.Background(), config.NATSPublish) })

	queue := mqs.NewQueue(&config)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cleanup, err := queue.Init(ctx)
	require.NoError(t, err)
	defer cleanup()

	sub, err := queue.Subscribe(ctx)
	require.NoError(t, err)
	defer sub.Shutdown(ctx)

	// Send one event to each stream.
	payloadA, _ := json.Marshal(map[string]any{"topic": "a"})
	payloadB, _ := json.Marshal(map[string]any{"topic": "b"})
	require.NoError(t, testutil.PublishToNATSStream(ctx, config.NATSPublish.Servers, streamA, payloadA))
	require.NoError(t, testutil.PublishToNATSStream(ctx, config.NATSPublish.Servers, streamB, payloadB))

	seen := map[string]string{}
	for range 2 {
		msg, err := sub.Receive(ctx)
		require.NoError(t, err)
		var got map[string]any
		require.NoError(t, json.Unmarshal(msg.Body, &got))
		seen[got["tenant_id"].(string)] = got["topic"].(string)
		msg.Ack()
	}

	assert.Equal(t, "a", seen["acme"])
	assert.Equal(t, "b", seen["globex"])
}

// TestIntegrationMQ_NATS_AccountsDir verifies the directory watcher:
//  1. Initial accounts in the directory are picked up at Init time.
//  2. A new account directory created after Init triggers a new
//     connection and the queue receives events from it.
func TestIntegrationMQ_NATSPublish_AccountsDir(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}
	t.Parallel()
	t.Cleanup(testinfra.Start(t))

	accountsDir := t.TempDir()
	servers := []string{testinfra.EnsureNATS(t)}

	// Pre-create one account on disk before Init.
	initialStream := "init-" + uuid.New().String()
	initialAcc := mqs.NATSPublishAccountConfig{
		Name:     "initial",
		Stream:   initialStream,
		Consumer: "outpost",
		TenantID: "initial-tenant",
	}
	writeAccountDir(t, accountsDir, initialAcc)

	// Provision JetStream resources for both initial + late account up-front
	// so the watcher only has to add the file to trigger the new connection.
	lateStream := "late-" + uuid.New().String()
	lateAcc := mqs.NATSPublishAccountConfig{
		Name:     "late",
		Stream:   lateStream,
		Consumer: "outpost",
		TenantID: "late-tenant",
	}
	provisionConfig := &mqs.NATSPublishConfig{
		Servers:  servers,
		Accounts: []mqs.NATSPublishAccountConfig{initialAcc, lateAcc},
	}
	require.NoError(t, testutil.DeclareTestNATSInfrastructure(context.Background(), provisionConfig))
	t.Cleanup(func() { _ = testutil.TeardownTestNATSInfrastructure(context.Background(), provisionConfig) })

	config := mqs.QueueConfig{
		NATSPublish: &mqs.NATSPublishConfig{
			Servers:     servers,
			AccountsDir: accountsDir,
		},
	}

	queue := mqs.NewQueue(&config)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cleanup, err := queue.Init(ctx)
	require.NoError(t, err)
	defer cleanup()

	sub, err := queue.Subscribe(ctx)
	require.NoError(t, err)
	defer sub.Shutdown(ctx)

	// Inject one event for the initial account.
	initialPayload, _ := json.Marshal(map[string]any{"src": "initial"})
	require.NoError(t, testutil.PublishToNATSStream(ctx, servers, initialStream, initialPayload))

	msg1, err := sub.Receive(ctx)
	require.NoError(t, err)
	var got1 map[string]any
	require.NoError(t, json.Unmarshal(msg1.Body, &got1))
	assert.Equal(t, "initial-tenant", got1["tenant_id"])
	msg1.Ack()

	// Drop a new account directory in — the watcher should pick it up
	// and open a connection within the debounce window.
	writeAccountDir(t, accountsDir, lateAcc)

	// Give the watcher a moment to reconcile.
	deadline := time.Now().Add(5 * time.Second)
	latePayload, _ := json.Marshal(map[string]any{"src": "late"})
	var msg2 *mqs.Message
	for time.Now().Before(deadline) {
		require.NoError(t, testutil.PublishToNATSStream(ctx, servers, lateStream, latePayload))
		readCtx, readCancel := context.WithTimeout(ctx, 1*time.Second)
		msg2, err = sub.Receive(readCtx)
		readCancel()
		if err == nil {
			break
		}
	}
	require.NotNil(t, msg2, "expected to receive event from late-added account")
	var got2 map[string]any
	require.NoError(t, json.Unmarshal(msg2.Body, &got2))
	assert.Equal(t, "late-tenant", got2["tenant_id"])
	msg2.Ack()
}

// writeAccountDir lays out a single account inside accountsDir using
// the meta.yaml + (empty) user.creds convention. Tests using no-auth
// NATS leave CredentialsFile empty in meta.yaml.
func writeAccountDir(t *testing.T, accountsDir string, acc mqs.NATSPublishAccountConfig) {
	t.Helper()
	dir := filepath.Join(accountsDir, acc.Name)
	require.NoError(t, os.MkdirAll(dir, 0o755))

	meta := "name: " + acc.Name + "\n" +
		"stream: " + acc.Stream + "\n" +
		"consumer: " + acc.Consumer + "\n" +
		"tenant_id: " + acc.TenantID + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "meta.yaml"), []byte(meta), 0o600))
}

// TestNATS_SubscribeLazyInit_EmptyAccountsDir covers the publishmq-path
// regression: the publishmq consumer constructs the queue via mqs.NewQueue and
// calls Subscribe directly, never Init. Subscribe must therefore lazily
// initialize the queue and tolerate a still-empty accounts directory at
// startup — returning a working (empty) subscription that the directory
// watcher fills as credentials land — instead of failing the consumer
// permanently with "nats: queue not initialized".
//
// This runs without a broker: with zero accounts on disk no NATS connection is
// dialled, so it is a fast unit test rather than an integration test.
func TestNATSPublish_SubscribeLazyInit_EmptyAccountsDir(t *testing.T) {
	t.Parallel()

	config := mqs.QueueConfig{
		NATSPublish: &mqs.NATSPublishConfig{
			Servers:     []string{"nats://127.0.0.1:4222"},
			AccountsDir: t.TempDir(), // exists but empty
		},
	}

	queue := mqs.NewQueue(&config)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Intentionally no queue.Init(ctx) — mirror the publishmq consumer path.
	sub, err := queue.Subscribe(ctx)
	require.NoError(t, err)
	require.NotNil(t, sub)
	_ = sub.Shutdown(context.Background())
}
