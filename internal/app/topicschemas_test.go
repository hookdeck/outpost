package app

import (
	"context"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/hookdeck/outpost/internal/topicschema"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadTopicCatalog_OnlyForAPIService(t *testing.T) {
	newApp := func(service, schemas string) *App {
		cfg := &config.Config{}
		cfg.InitDefaults()
		cfg.Service = service
		cfg.Topics = []string{"order.created"}
		cfg.TopicsSchemas = config.NewTopicSchemas(schemas)
		return &App{config: cfg, logger: testutil.CreateTestLogger(t)}
	}
	const valid = `{"order.created":{"validation":"warn","payload_schema":{"type":"object"}}}`
	// Invalid: mcp.enabled needs payload_schema.
	const invalid = `{"order.created":{"mcp":{"enabled":true}}}`

	for _, service := range []string{"", "all", "api"} {
		t.Run(service+" loads", func(t *testing.T) {
			a := newApp(service, valid)
			require.NoError(t, a.loadTopicCatalog(context.Background()))
			topic, ok := a.config.TopicCatalog().Topic("order.created")
			require.True(t, ok)
			assert.Equal(t, "warn", string(topic.Validation))

			err := newApp(service, invalid).loadTopicCatalog(context.Background())
			assert.ErrorIs(t, err, config.ErrInvalidTopicSchemas)
		})
	}

	for _, service := range []string{"delivery", "log"} {
		t.Run(service+" skips", func(t *testing.T) {
			a := newApp(service, invalid)
			require.NoError(t, a.loadTopicCatalog(context.Background()))
			assert.False(t, a.config.TopicCatalog().HasSchemas())
		})
	}
}

func TestApplyTopicSchemas(t *testing.T) {
	const created = `{"order.created":{"mcp":{"enabled":true},"payload_schema":{"type":"object","properties":{"total":{"type":"number"},"currency":{"type":"string"}}}}}`
	// Breaking: currency removed.
	const breaking = `{"order.created":{"mcp":{"enabled":true},"payload_schema":{"type":"object","properties":{"total":{"type":"number"}}}}}`

	redisClient := testutil.CreateTestRedisClient(t)
	newApp := func(t *testing.T, service, schemas string, allowBreaking bool) *App {
		cfg := &config.Config{}
		cfg.InitDefaults()
		cfg.Service = service
		cfg.Topics = []string{"order.created"}
		cfg.TopicsSchemas = config.NewTopicSchemas(schemas)
		cfg.TopicsAllowBreakingChanges = allowBreaking
		a := &App{config: cfg, logger: testutil.CreateTestLogger(t), redisClient: redisClient}
		require.NoError(t, a.loadTopicCatalog(context.Background()))
		return a
	}
	ctx := context.Background()

	t.Run("delivery and log skip", func(t *testing.T) {
		for _, service := range []string{"delivery", "log"} {
			require.NoError(t, newApp(t, service, created, false).applyTopicSchemas(ctx))
		}
		applied, err := topicschema.ReadApplied(ctx, redisClient, "")
		require.NoError(t, err)
		assert.Nil(t, applied)
	})

	t.Run("first apply records the configuration", func(t *testing.T) {
		a := newApp(t, "api", created, false)
		require.NoError(t, a.applyTopicSchemas(ctx))
		applied, err := topicschema.ReadApplied(ctx, redisClient, "")
		require.NoError(t, err)
		require.NotNil(t, applied)
		assert.Nil(t, a.brokenSchemas, "nothing broken")
	})

	t.Run("breaking change without live subscriptions passes", func(t *testing.T) {
		// Applied, then restored, so later subtests diff against created.
		require.NoError(t, newApp(t, "", breaking, false).applyTopicSchemas(ctx))
		require.NoError(t, newApp(t, "", created, false).applyTopicSchemas(ctx))
	})

	// A live subscription to order.created.
	store := tenantstore.New(tenantstore.Config{RedisClient: redisClient, IndexedTypes: []string{models.DestinationTypeMCP}})
	require.NoError(t, store.UpsertTenant(ctx, models.Tenant{ID: "t1", CreatedAt: time.Now(), UpdatedAt: time.Now()}))
	expiresAt := time.Now().Add(time.Hour)
	require.NoError(t, store.CreateDestination(ctx, models.Destination{
		ID: "sub_1", TenantID: "t1", Type: models.DestinationTypeMCP, Topics: models.Topics{"order.created"},
		Config: models.Config{"url": "https://example.com/"}, Credentials: models.Credentials{"secret": "x"},
		ExpiresAt: &expiresAt, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}))

	t.Run("breaking change with a live subscription fails", func(t *testing.T) {
		err := newApp(t, "all", breaking, false).applyTopicSchemas(ctx)
		var breakingErr *topicschema.BreakingChangeError
		require.ErrorAs(t, err, &breakingErr)
		assert.Contains(t, err.Error(), "order.created")
	})

	createdHash := newApp(t, "api", created, false).config.TopicCatalog().Snapshot().TopicHash("order.created")
	breakingHash := newApp(t, "api", breaking, false).config.TopicCatalog().Snapshot().TopicHash("order.created")

	t.Run("TOPICS_ALLOW_BREAKING_CHANGES applies it", func(t *testing.T) {
		a := newApp(t, "api", breaking, true)
		require.NoError(t, a.applyTopicSchemas(ctx))
		applied, err := topicschema.ReadApplied(ctx, redisClient, "")
		require.NoError(t, err)
		assert.NotEmpty(t, applied.Broken["order.created"], "the forced change records the broken schema")

		// For the subscribe handler: refreshes of subscriptions to the old
		// schema end them.
		assert.True(t, a.brokenSchemas.IsBroken("order.created", createdHash))
		assert.False(t, a.brokenSchemas.IsBroken("order.created", breakingHash))
	})

	t.Run("later starts load the broken schemas too", func(t *testing.T) {
		a := newApp(t, "all", breaking, false)
		require.NoError(t, a.applyTopicSchemas(ctx))
		assert.True(t, a.brokenSchemas.IsBroken("order.created", createdHash))

		other := newApp(t, "delivery", breaking, false)
		require.NoError(t, other.applyTopicSchemas(ctx))
		assert.Nil(t, other.brokenSchemas, "the API service only")
	})
}
