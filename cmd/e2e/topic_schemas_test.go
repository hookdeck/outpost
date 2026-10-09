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
// publish-time validation, schema_valid in the logs and GET /topics.
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

	for _, url := range []string{s.base.apiURL("/publish"), s.base.apiV2URL("/publish")} {
		var resp validationErrorResponse
		status := s.publishStatus(url, tenant.ID, "order.created", invalid, &resp)
		s.Require().Equal(http.StatusUnprocessableEntity, status, url)
		s.Equal("validation error", resp.Message)
		s.Require().NotEmpty(resp.Data)
		joined := strings.Join(resp.Data, "\n")
		s.Contains(joined, "data.total")
		s.Contains(joined, `"currency"`)
		for _, e := range resp.Data {
			s.NotContains(e, schemaSentinel, "validation errors must not echo payload values")
		}
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

	got := s.waitForEventV2(event.ID)
	s.Require().NotNil(got.SchemaValid)
	s.True(*got.SchemaValid)
}

func (s *topicSchemasSuite) TestWarn_InvalidPublishAcceptedAndMarked() {
	tenant := s.base.createTenant()
	dest := s.base.createWebhookDestination(tenant.ID, "order.updated", withSecret(testSecret))

	invalid := s.base.publish(tenant.ID, "order.updated", map[string]any{"orderId": "o_1", "status": schemaSentinel})
	s.base.waitForNewMockServerEvents(dest.mockID, 1)

	got := s.waitForEventV2(invalid.ID)
	s.Require().NotNil(got.SchemaValid, "warn mode records the verdict")
	s.False(*got.SchemaValid)

	// v1 exposes it too.
	var v1 eventWithSchemaValid
	s.Require().Equal(http.StatusOK, s.base.doJSON(http.MethodGet, s.base.apiURL("/events/"+invalid.ID), nil, &v1))
	s.Require().NotNil(v1.SchemaValid)
	s.False(*v1.SchemaValid)

	attempt := s.waitForAttemptV2(tenant.ID, invalid.ID)
	s.Require().NotNil(attempt.Event.SchemaValid, "the attempt's event carries the verdict")
	s.False(*attempt.Event.SchemaValid)
	s.Equal("success", attempt.Status)

	valid := s.base.publish(tenant.ID, "order.updated", map[string]any{"orderId": "o_1", "status": "paid"})
	s.base.waitForNewMockServerEvents(dest.mockID, 2)

	got = s.waitForEventV2(valid.ID)
	s.Require().NotNil(got.SchemaValid)
	s.True(*got.SchemaValid)

	attempt = s.waitForAttemptV2(tenant.ID, valid.ID)
	s.Require().NotNil(attempt.Event.SchemaValid)
	s.True(*attempt.Event.SchemaValid)
}

func (s *topicSchemasSuite) TestTopicsWithoutSchemaAreNotValidated() {
	tenant := s.base.createTenant()
	dest := s.base.createWebhookDestination(tenant.ID, "user.created", withSecret(testSecret))
	event := s.base.publish(tenant.ID, "user.created", map[string]any{"anything": true})
	s.base.waitForNewMockServerEvents(dest.mockID, 1)
	got := s.waitForEventV2(event.ID)
	s.Nil(got.SchemaValid)
}

func (s *topicSchemasSuite) TestListTopics_V1ReturnsNames() {
	var topics []string
	status := s.base.doJSON(http.MethodGet, s.base.apiURL("/topics"), nil, &topics)
	s.Require().Equal(http.StatusOK, status)
	s.Equal(append(slices.Clone(testutil.TestTopics), "order.created", "order.updated"), topics)
}

func (s *topicSchemasSuite) TestListTopics_V2ReturnsObjects() {
	var topics []struct {
		Name          string          `json:"name"`
		Description   string          `json:"description"`
		PayloadSchema json.RawMessage `json:"payload_schema"`
		Validation    string          `json:"validation"`
		MCP           struct {
			Enabled bool `json:"enabled"`
		} `json:"mcp"`
	}
	status := s.base.doJSON(http.MethodGet, s.base.apiV2URL("/topics"), nil, &topics)
	s.Require().Equal(http.StatusOK, status)

	names := make([]string, len(topics))
	for i, t := range topics {
		names[i] = t.Name
	}
	s.Require().Equal(append(slices.Clone(testutil.TestTopics), "order.created", "order.updated"), names)

	byName := map[string]int{}
	for i, t := range topics {
		byName[t.Name] = i
	}
	created := topics[byName["order.created"]]
	s.Equal("Fires when a new order is placed.", created.Description)
	s.Equal("enforce", created.Validation)
	s.True(created.MCP.Enabled)
	s.JSONEq(orderCreatedSchema, string(created.PayloadSchema))

	updated := topics[byName["order.updated"]]
	s.Equal("warn", updated.Validation)
	s.False(updated.MCP.Enabled)
	s.JSONEq(orderUpdatedSchema, string(updated.PayloadSchema))

	plain := topics[byName["user.created"]]
	s.Equal("off", plain.Validation)
	s.Empty(plain.PayloadSchema)
	s.False(plain.MCP.Enabled)
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

// waitForEventV2 polls GET /api/v2/events/:id until the event is logged.
func (s *topicSchemasSuite) waitForEventV2(eventID string) eventWithSchemaValid {
	s.T().Helper()
	deadline := time.Now().Add(attemptPollTimeout)
	for time.Now().Before(deadline) {
		var got eventWithSchemaValid
		status, body := s.base.doRawGet(s.base.apiV2URL("/events/" + eventID))
		if status == http.StatusOK {
			s.Require().NoError(json.Unmarshal(body, &got))
			return got
		}
		time.Sleep(100 * time.Millisecond)
	}
	s.Require().FailNowf("timeout", "timed out waiting for event %s", eventID)
	return eventWithSchemaValid{}
}

// waitForAttemptV2 polls GET /api/v2/attempts until the event has an attempt.
func (s *topicSchemasSuite) waitForAttemptV2(tenantID, eventID string) attemptWithEvent {
	s.T().Helper()
	deadline := time.Now().Add(attemptPollTimeout)
	for time.Now().Before(deadline) {
		var resp struct {
			Models []attemptWithEvent `json:"models"`
		}
		url := s.base.apiV2URL(fmt.Sprintf("/attempts?tenant_id=%s&event_id=%s&include=event", tenantID, eventID))
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
