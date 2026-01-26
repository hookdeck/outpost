package apirouter_test

import (
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

func TestRetryDelivery(t *testing.T) {
	t.Parallel()

	result := setupTestRouterFull(t, "", "")

	// Create a tenant and destination
	tenantID := idgen.String()
	destinationID := idgen.Destination()
	require.NoError(t, result.entityStore.UpsertTenant(context.Background(), models.Tenant{
		ID:        tenantID,
		CreatedAt: time.Now(),
	}))
	require.NoError(t, result.entityStore.UpsertDestination(context.Background(), models.Destination{
		ID:        destinationID,
		TenantID:  tenantID,
		Type:      "webhook",
		Topics:    []string{"*"},
		CreatedAt: time.Now(),
	}))

	// Seed a delivery event
	eventID := idgen.Event()
	deliveryID := idgen.Attempt()
	eventTime := time.Now().Add(-1 * time.Hour).Truncate(time.Millisecond)
	deliveryTime := eventTime.Add(100 * time.Millisecond)

	event := testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithID(eventID),
		testutil.EventFactory.WithTenantID(tenantID),
		testutil.EventFactory.WithDestinationID(destinationID),
		testutil.EventFactory.WithTopic("order.created"),
		testutil.EventFactory.WithTime(eventTime),
	)

	attempt := testutil.AttemptFactory.AnyPointer(
		testutil.AttemptFactory.WithID(deliveryID),
		testutil.AttemptFactory.WithEventID(eventID),
		testutil.AttemptFactory.WithDestinationID(destinationID),
		testutil.AttemptFactory.WithStatus("failed"),
		testutil.AttemptFactory.WithTime(deliveryTime),
	)

	require.NoError(t, result.logStore.InsertMany(context.Background(), []*models.LogEntry{{Event: event, Attempt: attempt}}))

	t.Run("should retry delivery successfully with full event data", func(t *testing.T) {
		// Subscribe to deliveryMQ to capture published task
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		subscription, err := result.deliveryMQ.Subscribe(ctx)
		require.NoError(t, err)

		// Trigger manual retry
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", baseAPIPath+"/tenants/"+tenantID+"/attempts/"+deliveryID+"/retry", nil)
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

	t.Run("should return 404 for non-existent delivery", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", baseAPIPath+"/tenants/"+tenantID+"/attempts/nonexistent/retry", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should return 404 for non-existent tenant", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", baseAPIPath+"/tenants/nonexistent/attempts/"+deliveryID+"/retry", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should return 400 when destination is disabled", func(t *testing.T) {
		// Create a new destination that's disabled
		disabledDestinationID := idgen.Destination()
		disabledAt := time.Now()
		require.NoError(t, result.entityStore.UpsertDestination(context.Background(), models.Destination{
			ID:         disabledDestinationID,
			TenantID:   tenantID,
			Type:       "webhook",
			Topics:     []string{"*"},
			CreatedAt:  time.Now(),
			DisabledAt: &disabledAt,
		}))

		// Create a delivery for the disabled destination
		disabledEventID := idgen.Event()
		disabledDeliveryID := idgen.Attempt()

		disabledEvent := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithID(disabledEventID),
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithDestinationID(disabledDestinationID),
			testutil.EventFactory.WithTopic("order.created"),
			testutil.EventFactory.WithTime(eventTime),
		)

		disabledAttempt := testutil.AttemptFactory.AnyPointer(
			testutil.AttemptFactory.WithID(disabledDeliveryID),
			testutil.AttemptFactory.WithEventID(disabledEventID),
			testutil.AttemptFactory.WithDestinationID(disabledDestinationID),
			testutil.AttemptFactory.WithStatus("failed"),
			testutil.AttemptFactory.WithTime(deliveryTime),
		)

		require.NoError(t, result.logStore.InsertMany(context.Background(), []*models.LogEntry{{Event: disabledEvent, Attempt: disabledAttempt}}))

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", baseAPIPath+"/tenants/"+tenantID+"/attempts/"+disabledDeliveryID+"/retry", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		assert.Equal(t, "Destination is disabled", response["message"])
	})
}
