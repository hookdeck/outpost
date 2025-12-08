package apirouter_test

import (
	"context"
	"encoding/json"
	"fmt"
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
	deliveryID := idgen.Delivery()
	eventTime := time.Now().Add(-1 * time.Hour).Truncate(time.Millisecond)
	deliveryTime := eventTime.Add(100 * time.Millisecond)

	event := testutil.EventFactory.AnyPointer(
		testutil.EventFactory.WithID(eventID),
		testutil.EventFactory.WithTenantID(tenantID),
		testutil.EventFactory.WithDestinationID(destinationID),
		testutil.EventFactory.WithTopic("order.created"),
		testutil.EventFactory.WithTime(eventTime),
	)

	delivery := testutil.DeliveryFactory.AnyPointer(
		testutil.DeliveryFactory.WithID(deliveryID),
		testutil.DeliveryFactory.WithEventID(eventID),
		testutil.DeliveryFactory.WithDestinationID(destinationID),
		testutil.DeliveryFactory.WithStatus("failed"),
		testutil.DeliveryFactory.WithTime(deliveryTime),
	)

	de := &models.DeliveryEvent{
		ID:            fmt.Sprintf("%s_%s", eventID, deliveryID),
		DestinationID: destinationID,
		Event:         *event,
		Delivery:      delivery,
	}

	require.NoError(t, result.logStore.InsertManyDeliveryEvent(context.Background(), []*models.DeliveryEvent{de}))

	t.Run("should retry delivery successfully", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", baseAPIPath+"/"+tenantID+"/deliveries/"+deliveryID+"/retry", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		assert.Equal(t, true, response["success"])
	})

	t.Run("should return 404 for non-existent delivery", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", baseAPIPath+"/"+tenantID+"/deliveries/nonexistent/retry", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should return 404 for non-existent tenant", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", baseAPIPath+"/nonexistent/deliveries/"+deliveryID+"/retry", nil)
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
		disabledDeliveryID := idgen.Delivery()

		disabledEvent := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithID(disabledEventID),
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithDestinationID(disabledDestinationID),
			testutil.EventFactory.WithTopic("order.created"),
			testutil.EventFactory.WithTime(eventTime),
		)

		disabledDelivery := testutil.DeliveryFactory.AnyPointer(
			testutil.DeliveryFactory.WithID(disabledDeliveryID),
			testutil.DeliveryFactory.WithEventID(disabledEventID),
			testutil.DeliveryFactory.WithDestinationID(disabledDestinationID),
			testutil.DeliveryFactory.WithStatus("failed"),
			testutil.DeliveryFactory.WithTime(deliveryTime),
		)

		disabledDE := &models.DeliveryEvent{
			ID:            fmt.Sprintf("%s_%s", disabledEventID, disabledDeliveryID),
			DestinationID: disabledDestinationID,
			Event:         *disabledEvent,
			Delivery:      disabledDelivery,
		}

		require.NoError(t, result.logStore.InsertManyDeliveryEvent(context.Background(), []*models.DeliveryEvent{disabledDE}))

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", baseAPIPath+"/"+tenantID+"/deliveries/"+disabledDeliveryID+"/retry", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		assert.Equal(t, "Destination is disabled", response["message"])
	})
}
