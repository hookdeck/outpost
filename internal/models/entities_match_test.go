package models_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDestination_IsExpired(t *testing.T) {
	t.Parallel()

	now := time.Now()
	for _, tc := range []struct {
		name      string
		expiresAt *time.Time
		expected  bool
	}{
		{"no expiry", nil, false},
		{"future", ptr(now.Add(time.Millisecond)), false},
		{"exactly now", ptr(now), true},
		{"past", ptr(now.Add(-time.Hour)), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := models.Destination{ExpiresAt: tc.expiresAt}
			assert.Equal(t, tc.expected, d.IsExpired(now))
		})
	}
}

func TestDestination_ExpiresAtJSON(t *testing.T) {
	t.Parallel()

	d := testutil.DestinationFactory.Any()
	b, err := json.Marshal(d)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "expires_at", "omitted when unset, so v1 bodies don't change")

	expiresAt := time.Date(2030, 1, 2, 3, 4, 5, 6_000_000, time.UTC)
	d.ExpiresAt = &expiresAt
	b, err = json.Marshal(d)
	require.NoError(t, err)
	assert.Contains(t, string(b), `"expires_at":"2030-01-02T03:04:05.006Z"`)

	var back models.Destination
	require.NoError(t, json.Unmarshal(b, &back))
	require.NotNil(t, back.ExpiresAt)
	assert.True(t, expiresAt.Equal(*back.ExpiresAt))
}

func TestTopics_MatchExactTopic(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		topics     models.Topics
		eventTopic string
		expected   bool
	}{
		{"listed", models.Topics{"user.created"}, "user.created", true},
		{"not listed", models.Topics{"user.created"}, "user.updated", false},
		{"empty event topic", models.Topics{"user.created"}, "", false},
		{"star event topic", models.Topics{"user.created"}, "*", false},
		{"star subscription", models.Topics{"*"}, "user.created", false},
		{"star subscription, star event", models.Topics{"*"}, "*", false},
		{"pattern subscription", models.Topics{"user.*"}, "user.created", false},
		{"prefix is not a match", models.Topics{"user.created"}, "user.created.v2", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, tc.topics.MatchExactTopic(tc.eventTopic))
		})
	}
}

func TestMatchDestinationTopic(t *testing.T) {
	t.Parallel()

	assert.True(t, models.IsExactTopicType(models.DestinationTypeMCP))
	assert.False(t, models.IsExactTopicType("webhook"))

	for _, eventTopic := range []string{"", "*"} {
		assert.True(t, models.MatchDestinationTopic("webhook", models.Topics{"user.created"}, eventTopic, true),
			"other types keep matching %q", eventTopic)
		assert.False(t, models.MatchDestinationTopic("mcp", models.Topics{"user.created"}, eventTopic, true),
			"mcp never matches %q", eventTopic)
	}
	assert.True(t, models.MatchDestinationTopic("webhook", models.Topics{"user.*"}, "user.created", true))
	assert.False(t, models.MatchDestinationTopic("mcp", models.Topics{"user.*"}, "user.created", true))
	assert.True(t, models.MatchDestinationTopic("mcp", models.Topics{"user.created"}, "user.created", false))
}

func TestDestination_MatchEvent(t *testing.T) {
	t.Parallel()

	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTopic("user.created"),
		testutil.EventFactory.WithData(json.RawMessage(`{"plan":"pro"}`)),
	)
	base := func(opts ...func(*models.Destination)) models.Destination {
		return testutil.DestinationFactory.Any(append([]func(*models.Destination){
			testutil.DestinationFactory.WithTopics([]string{"user.created"}),
		}, opts...)...)
	}

	for _, tc := range []struct {
		name        string
		destination models.Destination
		event       models.Event
		expected    bool
	}{
		{"matches", base(), event, true},
		{"disabled", base(testutil.DestinationFactory.WithDisabledAt(time.Now())), event, false},
		{"expired", base(testutil.DestinationFactory.WithExpiresAt(time.Now().Add(-time.Second))), event, false},
		{"not expired yet", base(testutil.DestinationFactory.WithExpiresAt(time.Now().Add(time.Hour))), event, true},
		{"filter matches", base(testutil.DestinationFactory.WithFilter(models.Filter{"data": map[string]any{"plan": "pro"}})), event, true},
		{"filter misses", base(testutil.DestinationFactory.WithFilter(models.Filter{"data": map[string]any{"plan": "free"}})), event, false},
		{"mcp exact topic", base(testutil.DestinationFactory.WithType("mcp")), event, true},
		{"mcp on a topic-less publish", base(testutil.DestinationFactory.WithType("mcp")), withTopic(event, ""), false},
		{"mcp on a * publish", base(testutil.DestinationFactory.WithType("mcp")), withTopic(event, "*"), false},
		{"webhook on a * publish", base(), withTopic(event, "*"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.expected, tc.destination.MatchEvent(tc.event, true))
		})
	}
}

func TestFilterInput_SharedAcrossFilters(t *testing.T) {
	t.Parallel()

	event := testutil.EventFactory.Any(
		testutil.EventFactory.WithTopic("order.created"),
		testutil.EventFactory.WithMetadata(map[string]string{"source": "api"}),
		testutil.EventFactory.WithData(json.RawMessage(`{"total":10,"items":[{"sku":"a"}]}`)),
	)
	input := models.NewFilterInput(event)
	before, err := json.Marshal(input)
	require.NoError(t, err)

	filters := []models.Filter{
		{"data": map[string]any{"total": map[string]any{"$gte": float64(5)}}},
		{"data": map[string]any{"items": map[string]any{"$in": []any{map[string]any{"sku": "a"}}}}},
		{"metadata": map[string]any{"source": "api"}, "topic": "order.created"},
		{"data": map[string]any{"total": map[string]any{"$lt": float64(5)}}},
		{"$or": []any{map[string]any{"data": map[string]any{"total": float64(1)}}, map[string]any{"id": event.ID}}},
	}
	for range 3 {
		for _, f := range filters {
			assert.Equal(t, models.MatchFilter(f, event), models.MatchFilterInput(f, input))
		}
	}
	after, err := json.Marshal(input)
	require.NoError(t, err)
	assert.JSONEq(t, string(before), string(after), "matching never modifies the shared input")

	t.Run("invalid data matches as empty data", func(t *testing.T) {
		bad := event
		bad.Data = json.RawMessage(`not-json`)
		input := models.NewFilterInput(bad)
		assert.Equal(t, map[string]any{}, input["data"])
		assert.True(t, models.MatchFilterInput(nil, input))
	})
}

func ptr[T any](v T) *T { return &v }

func withTopic(e models.Event, topic string) models.Event {
	e.Topic = topic
	return e
}
