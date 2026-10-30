package apirouter_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// includeRecordingLogStore records the Include of the last attempt list.
type includeRecordingLogStore struct {
	logstore.LogStore
	include []logstore.Field
}

func (s *includeRecordingLogStore) ListAttempt(ctx context.Context, req logstore.ListAttemptRequest) (logstore.ListAttemptResponse, error) {
	s.include = req.Include
	return s.LogStore.ListAttempt(ctx, req)
}

func TestAPI_ListAttemptsIncludeReadsFields(t *testing.T) {
	tests := []struct {
		name         string
		query        string
		fields       []logstore.Field
		event        bool
		eventData    bool
		responseData bool
	}{
		{"none", "", nil, false, false, false},
		{"event", "include=event", []logstore.Field{logstore.FieldEventMetadata}, true, false, false},
		{"event.data", "include=event.data", []logstore.Field{logstore.FieldEventData, logstore.FieldEventMetadata}, true, true, false},
		{"response_data", "include=response_data", []logstore.Field{logstore.FieldResponseData}, false, false, true},
		{"event and response_data", "include=event&include=response_data", []logstore.Field{logstore.FieldEventMetadata, logstore.FieldResponseData}, true, false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := &includeRecordingLogStore{LogStore: logstore.NewMemLogStore()}
			h := newAPITest(t, withLogStore(store))

			e := ef.AnyPointer(
				ef.WithID("e1"), ef.WithTenantID("t1"),
				ef.WithMetadata(map[string]string{"k": "v"}),
				ef.WithDataMap(map[string]any{"key": "val"}),
			)
			a := attemptForEvent(e, af.WithID("a1"))
			a.ResponseData = map[string]interface{}{"body": "ok"}
			require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{{Event: e, Attempt: a}}))

			resp := h.do(h.withAPIKey(httptest.NewRequest(http.MethodGet, "/api/v1/attempts?"+tc.query, nil)))
			require.Equal(t, http.StatusOK, resp.Code)

			assert.ElementsMatch(t, tc.fields, store.include)

			var body struct {
				Models []map[string]any `json:"models"`
			}
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
			require.Len(t, body.Models, 1)
			m := body.Models[0]

			_, hasResponseData := m["response_data"]
			assert.Equal(t, tc.responseData, hasResponseData)

			event, isObject := m["event"].(map[string]any)
			assert.Equal(t, tc.event, isObject)
			if isObject {
				assert.Equal(t, map[string]any{"k": "v"}, event["metadata"])
				_, hasData := event["data"]
				assert.Equal(t, tc.eventData, hasData)
			}
		})
	}
}
