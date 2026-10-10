package app

import (
	"testing"

	"github.com/hookdeck/outpost/internal/config"
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
