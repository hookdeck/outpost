package apirouter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func retryBody(eventID, destinationID string) *bytes.Buffer {
	body, _ := json.Marshal(map[string]string{
		"event_id":       eventID,
		"destination_id": destinationID,
	})
	return bytes.NewBuffer(body)
}

func TestRetry(t *testing.T) {
	t.Parallel()

	result := setupTestRouterFull(t, "", "")

	// Create a tenant and destination
	tenantID := idgen.String()
	destinationID := idgen.Destination()
	require.NoError(t, result.tenantStore.UpsertTenant(context.Background(), models.Tenant{
		ID:        tenantID,
		CreatedAt: time.Now(),
	}))
	require.NoError(t, result.tenantStore.UpsertDestination(context.Background(), models.Destination{
		ID:        destinationID,
		TenantID:  tenantID,
		Type:      "webhook",
		Topics:    []string{"*"},
		CreatedAt: time.Now(),
	}))

	// Seed an event
	eventID := idgen.Event()
	attemptID := idgen.Attempt()
	eventTime := time.Now().Add(-1 * time.Hour).Truncate(time.Millisecond)
	attemptTime := eventTime.Add(100 * time.Millisecond)

	event := testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithID(eventID),
		testutil.EventFactory.WithTenantID(tenantID),
		testutil.EventFactory.WithDestinationID(destinationID),
		testutil.EventFactory.WithTopic("order.created"),
		testutil.EventFactory.WithTime(eventTime),
	)

	attempt := testutil.AttemptFactory.AnyPointer(
		testutil.AttemptFactory.WithID(attemptID),
		testutil.AttemptFactory.WithEventID(eventID),
		testutil.AttemptFactory.WithDestinationID(destinationID),
		testutil.AttemptFactory.WithStatus("failed"),
		testutil.AttemptFactory.WithTime(attemptTime),
	)

	require.NoError(t, result.logStore.InsertMany(context.Background(), []*models.LogEntry{{Event: event, Attempt: attempt}}))

	t.Run("should retry successfully with full event data", func(t *testing.T) {
		// Subscribe to deliveryMQ to capture published task
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		subscription, err := result.deliveryMQ.Subscribe(ctx)
		require.NoError(t, err)

		// Trigger manual retry
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", baseAPIPath+"/retry", retryBody(eventID, destinationID))
		req.Header.Set("Content-Type", "application/json")
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		assert.Equal(t, true, response["success"])

		// Verify published task has full event data
		msg, err := subscription.Receive(ctx)
		require.NoError(t, err)

		var task models.DeliveryTask
		require.NoError(t, json.Unmarshal(msg.Body, &task))

		assert.Equal(t, eventID, task.Event.ID)
		assert.Equal(t, tenantID, task.Event.TenantID)
		assert.Equal(t, destinationID, task.Event.DestinationID)
		assert.Equal(t, "order.created", task.Event.Topic)
		assert.False(t, task.Event.Time.IsZero(), "event time should be set")
		assert.Equal(t, eventTime.UTC(), task.Event.Time.UTC())
		assert.Equal(t, event.Data, task.Event.Data, "event data should match original")
		assert.True(t, task.Manual, "should be marked as manual retry")

		msg.Ack()
	})

	t.Run("should return 404 for non-existent event", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", baseAPIPath+"/retry", retryBody("nonexistent", destinationID))
		req.Header.Set("Content-Type", "application/json")
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should return 404 for non-existent destination", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", baseAPIPath+"/retry", retryBody(eventID, "nonexistent"))
		req.Header.Set("Content-Type", "application/json")
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should return 400 when missing event_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"destination_id": destinationID})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", baseAPIPath+"/retry", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("should return 400 when missing destination_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"event_id": eventID})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", baseAPIPath+"/retry", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("should return 400 when destination is disabled", func(t *testing.T) {
		// Create a new destination that's disabled
		disabledDestinationID := idgen.Destination()
		disabledAt := time.Now()
		require.NoError(t, result.tenantStore.UpsertDestination(context.Background(), models.Destination{
			ID:         disabledDestinationID,
			TenantID:   tenantID,
			Type:       "webhook",
			Topics:     []string{"*"},
			CreatedAt:  time.Now(),
			DisabledAt: &disabledAt,
		}))

		// Create an event for the disabled destination
		disabledEventID := idgen.Event()
		disabledAttemptID := idgen.Attempt()

		disabledEvent := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithID(disabledEventID),
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithDestinationID(disabledDestinationID),
			testutil.EventFactory.WithTopic("order.created"),
			testutil.EventFactory.WithTime(eventTime),
		)

		disabledAttempt := testutil.AttemptFactory.AnyPointer(
			testutil.AttemptFactory.WithID(disabledAttemptID),
			testutil.AttemptFactory.WithEventID(disabledEventID),
			testutil.AttemptFactory.WithDestinationID(disabledDestinationID),
			testutil.AttemptFactory.WithStatus("failed"),
			testutil.AttemptFactory.WithTime(attemptTime),
		)

		require.NoError(t, result.logStore.InsertMany(context.Background(), []*models.LogEntry{{Event: disabledEvent, Attempt: disabledAttempt}}))

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", baseAPIPath+"/retry", retryBody(disabledEventID, disabledDestinationID))
		req.Header.Set("Content-Type", "application/json")
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		assert.Equal(t, "Destination is disabled", response["message"])
	})

	t.Run("should return 400 when destination does not match event", func(t *testing.T) {
		// Create a destination that only matches "order.created" topic
		mismatchDestinationID := idgen.Destination()
		require.NoError(t, result.tenantStore.UpsertDestination(context.Background(), models.Destination{
			ID:        mismatchDestinationID,
			TenantID:  tenantID,
			Type:      "webhook",
			Topics:    []string{"order.created"},
			CreatedAt: time.Now(),
		}))

		// Create an event with a different topic
		mismatchEventID := idgen.Event()
		mismatchAttemptID := idgen.Attempt()

		mismatchEvent := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithID(mismatchEventID),
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithDestinationID(mismatchDestinationID),
			testutil.EventFactory.WithTopic("user.updated"),
			testutil.EventFactory.WithTime(eventTime),
		)

		mismatchAttempt := testutil.AttemptFactory.AnyPointer(
			testutil.AttemptFactory.WithID(mismatchAttemptID),
			testutil.AttemptFactory.WithEventID(mismatchEventID),
			testutil.AttemptFactory.WithDestinationID(mismatchDestinationID),
			testutil.AttemptFactory.WithStatus("failed"),
			testutil.AttemptFactory.WithTime(attemptTime),
		)

		require.NoError(t, result.logStore.InsertMany(context.Background(), []*models.LogEntry{{Event: mismatchEvent, Attempt: mismatchAttempt}}))

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", baseAPIPath+"/retry", retryBody(mismatchEventID, mismatchDestinationID))
		req.Header.Set("Content-Type", "application/json")
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		assert.Equal(t, "destination does not match event", response["message"])
	})
}
