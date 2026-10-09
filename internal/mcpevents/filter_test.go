package mcpevents

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestArgumentsToFilter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		args string
		want string // JSON, "" for nil
	}{
		{"empty", `{}`, ""},
		{"scalar values", `{"currency":"USD","paid":true,"total":180}`, `{"data":{"currency":"USD","paid":true,"total":180}}`},
		{"list becomes $in", `{"currency":["EUR","USD"]}`, `{"data":{"currency":{"$in":["EUR","USD"]}}}`},
		{"operator object passes through", `{"total":{"$gte":100,"$lt":500}}`, `{"data":{"total":{"$gte":100,"$lt":500}}}`},
		{"spec example", `{"total":{"$gte":100},"currency":"USD"}`, `{"data":{"currency":"USD","total":{"$gte":100}}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := ParseSubscribeParams(json.RawMessage(`{"name":"t","arguments":` + tt.args + `,"delivery":{"url":"https://a.example/","secret":"` + testSecret + `"}}`))
			require.NoError(t, err)
			got := ArgumentsToFilter(p.ArgumentsMap)
			if tt.want == "" {
				assert.Nil(t, got)
				return
			}
			raw, err := json.Marshal(got)
			require.NoError(t, err)
			assert.JSONEq(t, tt.want, string(raw))
		})
	}
	assert.Nil(t, ArgumentsToFilter(nil))
}

func TestArgumentsToFilter_NoAliasing(t *testing.T) {
	t.Parallel()
	args := map[string]any{"a": []any{"x"}, "b": map[string]any{"$gt": 1.0}}
	f := ArgumentsToFilter(args)
	args["a"].([]any)[0] = "changed"
	args["b"].(map[string]any)["$gt"] = 2.0
	assert.Equal(t, models.Filter{"data": map[string]any{"a": map[string]any{"$in": []any{"x"}}, "b": map[string]any{"$gt": 1.0}}}, f)
}

func TestArgumentsToFilter_Matches(t *testing.T) {
	t.Parallel()
	p, err := ParseSubscribeParams(json.RawMessage(`{"name":"order.created","arguments":{"total":{"$gte":100},"currency":["EUR","USD"]},"delivery":{"url":"https://a.example/","secret":"` + testSecret + `"}}`))
	require.NoError(t, err)
	filter := ArgumentsToFilter(p.ArgumentsMap)
	event := func(data string) models.Event {
		return models.Event{ID: "e", Topic: "order.created", Time: time.Now(), Data: json.RawMessage(data)}
	}
	assert.True(t, models.MatchFilter(filter, event(`{"total":180,"currency":"USD"}`)))
	assert.True(t, models.MatchFilter(filter, event(`{"total":100,"currency":"EUR","x":1}`)))
	assert.False(t, models.MatchFilter(filter, event(`{"total":99,"currency":"USD"}`)))
	assert.False(t, models.MatchFilter(filter, event(`{"total":180,"currency":"GBP"}`)))
	assert.False(t, models.MatchFilter(filter, event(`{"currency":"USD"}`)))
}
