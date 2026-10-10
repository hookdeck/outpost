package app

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/config"
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
	const valid = `{"order.created":{"description":"An order was placed.","payload_schema":{"type":"object"}}}`
	// Invalid: mcp.enabled needs payload_schema.
	const invalid = `{"order.created":{"mcp":{"enabled":true}}}`

	for _, service := range []string{"", "all", "api"} {
		t.Run(service+" loads", func(t *testing.T) {
			a := newApp(service, valid)
			require.NoError(t, a.loadTopicCatalog())
			topic, ok := a.config.TopicCatalog().Topic("order.created")
			require.True(t, ok)
			assert.Equal(t, "An order was placed.", topic.Description)

			err := newApp(service, invalid).loadTopicCatalog()
			assert.ErrorIs(t, err, config.ErrInvalidTopicSchemas)
		})
	}

	for _, service := range []string{"delivery", "log"} {
		t.Run(service+" skips", func(t *testing.T) {
			a := newApp(service, invalid)
			require.NoError(t, a.loadTopicCatalog())
			assert.False(t, a.config.TopicCatalog().HasSchemas())
		})
	}
}

// Without TOPICS, the API service takes the topics from the schema keys; the
// other services never read the schemas, so their TOPICS stays unset.
func TestLoadTopicCatalog_TopicsFromSchemaKeys(t *testing.T) {
	newApp := func(service string) *App {
		cfg := &config.Config{}
		cfg.InitDefaults()
		cfg.Service = service
		cfg.TopicsSchemas = config.NewTopicSchemas(`{"order.updated":{},"order.created":{}}`)
		return &App{config: cfg, logger: testutil.CreateTestLogger(t)}
	}
	for _, service := range []string{"", "all", "api"} {
		a := newApp(service)
		require.NoError(t, a.loadTopicCatalog())
		assert.Equal(t, []string{"order.created", "order.updated"}, a.config.Topics, service)
	}
	for _, service := range []string{"delivery", "log"} {
		a := newApp(service)
		require.NoError(t, a.loadTopicCatalog())
		assert.Empty(t, a.config.Topics, service)
	}
}

func TestApplyTopicSchemas(t *testing.T) {
	const created = `{"order.created":{"mcp":{"enabled":true},"payload_schema":{"type":"object","properties":{"total":{"type":"number"},"currency":{"type":"string"}}}}}`
	// Additive: note added.
	const additive = `{"order.created":{"mcp":{"enabled":true},"payload_schema":{"type":"object","properties":{"total":{"type":"number"},"currency":{"type":"string"},"note":{"type":"string"}}}}}`
	// Breaking: currency removed.
	const breaking = `{"order.created":{"mcp":{"enabled":true},"payload_schema":{"type":"object","properties":{"total":{"type":"number"},"note":{"type":"string"}}}}}`
	// The topic leaves MCP, so its subscriptions would end.
	const notMCP = `{"order.created":{"payload_schema":{"type":"object"}}}`

	ctx := context.Background()
	redisClient := testutil.CreateTestRedisClient(t)
	newApp := func(t *testing.T, service, schemas string, allowBreaking bool) *App {
		cfg := &config.Config{}
		cfg.InitDefaults()
		cfg.Service = service
		cfg.Topics = []string{"order.created"}
		cfg.TopicsSchemas = config.NewTopicSchemas(schemas)
		cfg.TopicsAllowBreakingChanges = allowBreaking
		a := &App{config: cfg, logger: testutil.CreateTestLogger(t), redisClient: redisClient}
		require.NoError(t, a.loadTopicCatalog())
		return a
	}
	applied := func(t *testing.T) *topicschema.Applied {
		a, err := topicschema.ReadApplied(ctx, redisClient, "")
		require.NoError(t, err)
		return a
	}

	t.Run("delivery and log skip", func(t *testing.T) {
		for _, service := range []string{"delivery", "log"} {
			require.NoError(t, newApp(t, service, breaking, false).applyTopicSchemas(ctx))
		}
		assert.Nil(t, applied(t))
	})

	t.Run("the first apply records the configuration", func(t *testing.T) {
		a := newApp(t, "api", created, false)
		require.NoError(t, a.applyTopicSchemas(ctx))
		require.NotNil(t, applied(t))
		assert.Equal(t, a.config.TopicCatalog().Snapshot().Hash(), applied(t).Hash)
	})

	t.Run("an additive change is applied", func(t *testing.T) {
		a := newApp(t, "", additive, false)
		require.NoError(t, a.applyTopicSchemas(ctx))
		assert.Equal(t, a.config.TopicCatalog().Snapshot().Hash(), applied(t).Hash)
	})

	t.Run("a breaking change fails startup with the changes", func(t *testing.T) {
		err := newApp(t, "all", breaking, false).applyTopicSchemas(ctx)
		var breakingErr *topicschema.BreakingChangeError
		require.ErrorAs(t, err, &breakingErr)
		require.Len(t, breakingErr.Changes, 1)
		assert.Equal(t, "order.created", breakingErr.Changes[0].Topic)
		assert.Equal(t, topicschema.ChangePropertyRemoved, breakingErr.Changes[0].Kind)
		assert.Contains(t, err.Error(), "TOPICS_ALLOW_BREAKING_CHANGES")
		assert.Equal(t, newApp(t, "api", additive, false).config.TopicCatalog().Snapshot().Hash(), applied(t).Hash, "nothing recorded")
	})

	t.Run("ending every MCP-enabled topic fails startup", func(t *testing.T) {
		err := newApp(t, "api", notMCP, false).applyTopicSchemas(ctx)
		var breakingErr *topicschema.BreakingChangeError
		require.ErrorAs(t, err, &breakingErr)
		assert.Equal(t, topicschema.ChangeTopicEnded, breakingErr.Changes[0].Kind)
	})

	t.Run("TOPICS_ALLOW_BREAKING_CHANGES applies it", func(t *testing.T) {
		before := newApp(t, "api", additive, false).config.TopicCatalog().Snapshot().TopicHash("order.created")
		a := newApp(t, "api", breaking, true)
		require.NoError(t, a.applyTopicSchemas(ctx))
		got := applied(t)
		assert.Equal(t, a.config.TopicCatalog().Snapshot().Hash(), got.Hash)
		assert.True(t, got.IsBroken("order.created", before), "the old schema is recorded as broken")
	})
}
