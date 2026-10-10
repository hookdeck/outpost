package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/cmd/e2e/configs"
	"github.com/hookdeck/outpost/internal/app"
	"github.com/hookdeck/outpost/internal/config"
	internalredis "github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/topicschema"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// orderCreatedSchema is the spec's example payload schema.
const orderCreatedSchema = `{
	"type": "object",
	"properties": {
		"orderId": {"type": "string", "x-mcp-filter": false},
		"total": {"type": "number", "description": "Order total in major units."},
		"currency": {"type": "string", "description": "ISO 4217 currency code."},
		"createdAt": {"type": "string", "format": "date-time", "x-mcp-filter": false}
	},
	"required": ["orderId", "total", "currency"]
}`

const orderUpdatedSchema = `{
	"type": "object",
	"properties": {
		"orderId": {"type": "string"},
		"status": {"type": "string", "enum": ["paid", "shipped"]}
	},
	"required": ["orderId", "status"]
}`

// schemaSentinel is a payload value that must never appear in validation
// errors.
const schemaSentinel = "sentinel-7f3a9c"

func topicSchemasJSON() string {
	return `{
		"order.created": {
			"description": "Fires when a new order is placed.",
			"payload_schema": ` + orderCreatedSchema + `,
			"validation": "enforce",
			"mcp": {"enabled": true}
		},
		"order.updated": {
			"description": "Fires when an order changes.",
			"payload_schema": ` + orderUpdatedSchema + `,
			"validation": "warn"
		}
	}`
}

// withTopicSchemas adds the order topics to a clone of the test topics and
// configures their schemas.
func withTopicSchemas(cfg *config.Config) {
	cfg.Topics = append(slices.Clone(cfg.Topics), "order.created", "order.updated")
	cfg.TopicsSchemas = config.NewTopicSchemas(topicSchemasJSON())
}

// TestE2E_TopicSchemas boots Outpost with topic schemas and checks
// publish-time validation, schema_valid in the logs and that GET /topics is
// unchanged.
func TestE2E_TopicSchemas(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	suite.Run(t, &topicSchemasSuite{})
}

type topicSchemasSuite struct {
	suite.Suite
	base *basicSuite
}

func (s *topicSchemasSuite) SetupSuite() {
	s.base = &basicSuite{
		logStorageType: configs.LogStorageTypeClickHouse,
		redisConfig:    testinfra.NewDragonflyStackConfig(s.T()),
		configure:      withTopicSchemas,
	}
	s.base.SetT(s.T())
	s.base.SetupSuite()
}

func (s *topicSchemasSuite) SetupTest() {
	s.base.SetT(s.T())
}

func (s *topicSchemasSuite) TearDownSuite() {
	s.base.TearDownSuite()
}

type validationErrorResponse struct {
	Status  int      `json:"status"`
	Message string   `json:"message"`
	Data    []string `json:"data"`
}

// publishStatus publishes without asserting the status.
func (s *topicSchemasSuite) publishStatus(url, tenantID, topic string, data any, result any) int {
	s.T().Helper()
	return s.base.doJSON(http.MethodPost, url, map[string]any{
		"tenant_id": tenantID,
		"topic":     topic,
		"data":      data,
	}, result)
}

func (s *topicSchemasSuite) TestEnforce_InvalidPublishRejectedWithoutValues() {
	tenant := s.base.createTenant()
	invalid := map[string]any{
		"orderId": schemaSentinel,
		"total":   schemaSentinel, // must be a number
		// currency missing
	}

	var resp validationErrorResponse
	status := s.publishStatus(s.base.apiURL("/publish"), tenant.ID, "order.created", invalid, &resp)
	s.Require().Equal(http.StatusUnprocessableEntity, status)
	s.Equal("validation error", resp.Message)
	s.Require().NotEmpty(resp.Data)
	joined := strings.Join(resp.Data, "\n")
	s.Contains(joined, "data.total")
	s.Contains(joined, `"currency"`)
	for _, e := range resp.Data {
		s.NotContains(e, schemaSentinel, "validation errors must not echo payload values")
	}
}

func (s *topicSchemasSuite) TestEnforce_ValidPublishDelivered() {
	tenant := s.base.createTenant()
	dest := s.base.createWebhookDestination(tenant.ID, "order.created", withSecret(testSecret))

	// Rejected first, so the single delivery below is the valid event.
	var rejected validationErrorResponse
	status := s.publishStatus(s.base.apiURL("/publish"), tenant.ID, "order.created", map[string]any{"orderId": "o_1"}, &rejected)
	s.Require().Equal(http.StatusUnprocessableEntity, status)

	event := s.base.publish(tenant.ID, "order.created", map[string]any{
		"orderId":   "o_1",
		"total":     42.5,
		"currency":  "USD",
		"createdAt": "2026-10-09T12:00:00Z",
	})
	events := s.base.waitForNewMockServerEvents(dest.mockID, 1)
	s.Require().Len(events, 1)
	s.Equal("o_1", events[0].Payload["orderId"])

	got := s.waitForEvent(event.ID)
	s.Require().NotNil(got.SchemaValid)
	s.True(*got.SchemaValid)
}

func (s *topicSchemasSuite) TestWarn_InvalidPublishAcceptedAndMarked() {
	tenant := s.base.createTenant()
	dest := s.base.createWebhookDestination(tenant.ID, "order.updated", withSecret(testSecret))

	invalid := s.base.publish(tenant.ID, "order.updated", map[string]any{"orderId": "o_1", "status": schemaSentinel})
	s.base.waitForNewMockServerEvents(dest.mockID, 1)

	got := s.waitForEvent(invalid.ID)
	s.Require().NotNil(got.SchemaValid, "warn mode records the verdict")
	s.False(*got.SchemaValid)

	attempt := s.waitForAttempt(tenant.ID, invalid.ID)
	s.Require().NotNil(attempt.Event.SchemaValid, "the attempt's event carries the verdict")
	s.False(*attempt.Event.SchemaValid)
	s.Equal("success", attempt.Status)

	valid := s.base.publish(tenant.ID, "order.updated", map[string]any{"orderId": "o_1", "status": "paid"})
	s.base.waitForNewMockServerEvents(dest.mockID, 2)

	got = s.waitForEvent(valid.ID)
	s.Require().NotNil(got.SchemaValid)
	s.True(*got.SchemaValid)

	attempt = s.waitForAttempt(tenant.ID, valid.ID)
	s.Require().NotNil(attempt.Event.SchemaValid)
	s.True(*attempt.Event.SchemaValid)
}

func (s *topicSchemasSuite) TestTopicsWithoutSchemaAreNotValidated() {
	tenant := s.base.createTenant()
	dest := s.base.createWebhookDestination(tenant.ID, "user.created", withSecret(testSecret))
	event := s.base.publish(tenant.ID, "user.created", map[string]any{"anything": true})
	s.base.waitForNewMockServerEvents(dest.mockID, 1)
	got := s.waitForEvent(event.ID)
	s.Nil(got.SchemaValid)
}

// GET /topics is unchanged: topic names, schemas or not.
func (s *topicSchemasSuite) TestListTopics_ReturnsNames() {
	var topics []string
	status := s.base.doJSON(http.MethodGet, s.base.apiURL("/topics"), nil, &topics)
	s.Require().Equal(http.StatusOK, status)
	s.Equal(append(slices.Clone(testutil.TestTopics), "order.created", "order.updated"), topics)
}

type eventWithSchemaValid struct {
	ID          string `json:"id"`
	SchemaValid *bool  `json:"schema_valid"`
}

type attemptWithEvent struct {
	ID      string               `json:"id"`
	EventID string               `json:"event_id"`
	Status  string               `json:"status"`
	Event   eventWithSchemaValid `json:"event"`
}

// waitForEvent polls GET /events/:id until the event is logged.
func (s *topicSchemasSuite) waitForEvent(eventID string) eventWithSchemaValid {
	s.T().Helper()
	deadline := time.Now().Add(attemptPollTimeout)
	for time.Now().Before(deadline) {
		var got eventWithSchemaValid
		status, body := s.base.doRawGet(s.base.apiURL("/events/" + eventID))
		if status == http.StatusOK {
			s.Require().NoError(json.Unmarshal(body, &got))
			return got
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.Require().FailNowf("timeout", "timed out waiting for event %s", eventID)
	return eventWithSchemaValid{}
}

// waitForAttempt polls GET /attempts until the event has an attempt.
func (s *topicSchemasSuite) waitForAttempt(tenantID, eventID string) attemptWithEvent {
	s.T().Helper()
	deadline := time.Now().Add(attemptPollTimeout)
	for time.Now().Before(deadline) {
		var resp struct {
			Models []attemptWithEvent `json:"models"`
		}
		url := s.base.apiURL(fmt.Sprintf("/attempts?tenant_id=%s&event_id=%s&include=event", tenantID, eventID))
		if s.base.doJSON(http.MethodGet, url, nil, &resp) == http.StatusOK && len(resp.Models) > 0 {
			return resp.Models[0]
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.Require().FailNowf("timeout", "timed out waiting for an attempt of event %s", eventID)
	return attemptWithEvent{}
}

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

// TestE2E_TopicSchemas_BreakingChangeAtStartup runs Outpost several times
// against one Redis. A breaking change to an MCP-enabled topic fails startup
// with the list of changes; TOPICS_ALLOW_BREAKING_CHANGES applies it, after
// which the new configuration starts without the flag.
func TestE2E_TopicSchemas_BreakingChangeAtStartup(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping e2e test")
	}
	testinfraCleanup := testinfra.Start(t)
	defer testinfraCleanup()
	gin.SetMode(gin.TestMode)
	redisConfig := testinfra.NewDragonflyStackConfig(t)

	const original = `{"type":"object","properties":{"total":{"type":"number"},"currency":{"type":"string"}}}`
	// total narrows from number to string: not a widening.
	const breaking = `{"type":"object","properties":{"total":{"type":"string"},"currency":{"type":"string"}}}`
	newConfig := func(schema string, allowBreaking bool) config.Config {
		cfg := configs.Basic(t, configs.BasicOpts{LogStorage: configs.LogStorageTypePostgres, RedisConfig: redisConfig})
		cfg.Topics = nil
		cfg.TopicsSchemas = config.NewTopicSchemas(`{"order.created":{"mcp":{"enabled":true},"payload_schema":` + schema + `}}`)
		cfg.TopicsAllowBreakingChanges = allowBreaking
		return cfg
	}

	// 1. The first start records the configuration.
	startStandaloneApp(t, newConfig(original, false)).stop()

	// 2. The breaking change fails startup, listing the change.
	t.Run("breaking change refused", func(t *testing.T) {
		cfg := newConfig(breaking, false)
		prepareStandaloneConfig(t, &cfg)
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := app.New(&cfg).Run(ctx)
		var breakingErr *topicschema.BreakingChangeError
		require.ErrorAs(t, err, &breakingErr)
		require.Contains(t, err.Error(), "order.created /properties/total")
		require.Contains(t, err.Error(), "TOPICS_ALLOW_BREAKING_CHANGES")
	})

	// 3. TOPICS_ALLOW_BREAKING_CHANGES applies it. The running instance
	// reports its configuration as live.
	forced := newConfig(breaking, true)
	running := startStandaloneApp(t, forced)
	rdb, err := internalredis.New(context.Background(), redisConfig)
	require.NoError(t, err)
	defer rdb.Close()
	applied, err := topicschema.ReadApplied(context.Background(), rdb, "")
	require.NoError(t, err)
	require.NotNil(t, applied)
	require.NotEmpty(t, applied.Broken["order.created"], "the old schema is recorded as broken")
	require.Eventually(t, func() bool {
		live, err := topicschema.IsLive(context.Background(), rdb, "", applied.Hash)
		return err == nil && live
	}, 10*time.Second, 100*time.Millisecond, "the heartbeat reports the running configuration")
	running.stop()

	// 4. The forced configuration is now the applied one: it starts without
	// the flag.
	startStandaloneApp(t, newConfig(breaking, false)).stop()
}
