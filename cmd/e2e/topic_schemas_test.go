package e2e_test

import (
	"context"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/cmd/e2e/configs"
	"github.com/hookdeck/outpost/internal/app"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// TestE2E_TopicSchemas_InvalidConfigFailsStartup checks that a schema problem
// passes Validate, which does no I/O, and then fails app startup with an
// error instead of serving.
func TestE2E_TopicSchemas_InvalidConfigFailsStartup(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	testinfraCleanup := testinfra.Start(t)
	defer testinfraCleanup()
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name    string
		schemas string
		wantMsg string
	}{
		{
			name:    "mcp enabled without payload_schema",
			schemas: `{"order.created":{"mcp":{"enabled":true}}}`,
			wantMsg: "mcp.enabled requires payload_schema",
		},
		{
			name:    "key missing from TOPICS",
			schemas: `{"invoice.paid":{"description":"Not a configured topic."}}`,
			wantMsg: `"invoice.paid" is not in TOPICS`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := configs.Basic(t, configs.BasicOpts{LogStorage: configs.LogStorageTypePostgres})
			cfg.Topics = append(slices.Clone(cfg.Topics), "order.created")
			cfg.TopicsSchemas = config.NewTopicSchemas(tt.schemas)
			require.NoError(t, cfg.Validate(config.Flags{}), "Validate only checks the shape")
			configs.ApplyMigrations(t, &cfg)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- app.New(&cfg).Run(ctx) }()

			select {
			case err := <-done:
				require.ErrorIs(t, err, config.ErrInvalidTopicSchemas)
				require.Contains(t, err.Error(), tt.wantMsg)
			case <-time.After(30 * time.Second):
				cancel()
				<-done
				t.Fatal("app started instead of failing on invalid topic schemas")
			}
		})
	}
}

// TestE2E_TopicSchemas_TopicsFromSchemaKeys boots Outpost with topic schemas
// and no TOPICS: the schema keys become the topics, for GET /topics,
// publishing and destination topics alike.
func TestE2E_TopicSchemas_TopicsFromSchemaKeys(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	suite.Run(t, &topicsFromSchemasSuite{})
}

type topicsFromSchemasSuite struct {
	suite.Suite
	base *basicSuite
}

func (s *topicsFromSchemasSuite) SetupSuite() {
	s.base = &basicSuite{
		logStorageType: configs.LogStorageTypePostgres,
		redisConfig:    testinfra.NewDragonflyStackConfig(s.T()),
		configure: func(cfg *config.Config) {
			cfg.Topics = nil
			cfg.TopicsSchemas = config.NewTopicSchemas(`{
				"order.updated": {},
				"order.created": {
					"description": "Fires when a new order is placed.",
					"mcp": {"enabled": true},
					"payload_schema": {"type": "object", "properties": {"total": {"type": "number"}}}
				}
			}`)
		},
	}
	s.base.SetT(s.T())
	s.base.SetupSuite()
}

func (s *topicsFromSchemasSuite) SetupTest() {
	s.base.SetT(s.T())
}

func (s *topicsFromSchemasSuite) TearDownSuite() {
	s.base.TearDownSuite()
}

func (s *topicsFromSchemasSuite) TestListTopicsReturnsTheSchemaKeys() {
	var topics []string
	s.Require().Equal(http.StatusOK, s.base.doJSON(http.MethodGet, s.base.apiURL("/topics"), nil, &topics))
	s.Equal([]string{"order.created", "order.updated"}, topics)
}

func (s *topicsFromSchemasSuite) TestPublishAcceptsOnlyTheSchemaKeys() {
	tenant := s.base.createTenant()
	dest := s.base.createWebhookDestination(tenant.ID, "order.created", withSecret(testSecret))
	s.base.publish(tenant.ID, "order.created", map[string]any{"total": 1})
	s.Len(s.base.waitForNewMockServerEvents(dest.mockID, 1), 1)

	status := s.base.doJSON(http.MethodPost, s.base.apiURL("/publish"), map[string]any{
		"tenant_id": tenant.ID,
		"topic":     "user.created",
		"data":      map[string]any{},
	}, nil)
	s.Equal(http.StatusUnprocessableEntity, status, "a topic without a schema key isn't a topic")
}

func (s *topicsFromSchemasSuite) TestDestinationsTakeOnlyTheSchemaKeys() {
	tenant := s.base.createTenant()
	status := s.base.doJSON(http.MethodPost, s.base.apiURL("/tenants/"+tenant.ID+"/destinations"), map[string]any{
		"type":   "webhook",
		"topics": []string{"user.created"},
		"config": map[string]any{"url": "https://example.com/hook"},
	}, nil)
	s.Equal(http.StatusUnprocessableEntity, status)
}
