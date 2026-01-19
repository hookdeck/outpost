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

func TestLegacyRetryByEventDestination(t *testing.T) {
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

	t.Run("should retry via legacy endpoint and return deprecation header", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", baseAPIPath+"/tenants/"+tenantID+"/destinations/"+destinationID+"/events/"+eventID+"/retry", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusAccepted, w.Code)
		assert.Equal(t, "true", w.Header().Get("Deprecation"))
		assert.Contains(t, w.Header().Get("X-Deprecated-Message"), "POST /:tenantID/deliveries/:deliveryID/retry")

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
		assert.Equal(t, true, response["success"])
	})

	t.Run("should return 404 for non-existent event", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", baseAPIPath+"/tenants/"+tenantID+"/destinations/"+destinationID+"/events/nonexistent/retry", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should return 404 for non-existent destination", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", baseAPIPath+"/tenants/"+tenantID+"/destinations/nonexistent/events/"+eventID+"/retry", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should return 400 when destination is disabled", func(t *testing.T) {
		// Create a disabled destination
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

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", baseAPIPath+"/tenants/"+tenantID+"/destinations/"+disabledDestinationID+"/events/"+eventID+"/retry", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestLegacyListEventsByDestination(t *testing.T) {
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

	// Seed delivery events
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
		testutil.DeliveryFactory.WithStatus("success"),
		testutil.DeliveryFactory.WithTime(deliveryTime),
	)

	de := &models.DeliveryEvent{
		ID:            fmt.Sprintf("%s_%s", eventID, deliveryID),
		DestinationID: destinationID,
		Event:         *event,
		Delivery:      delivery,
	}

	require.NoError(t, result.logStore.InsertManyDeliveryEvent(context.Background(), []*models.DeliveryEvent{de}))

	t.Run("should list events for destination with deprecation header", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/destinations/"+destinationID+"/events", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "true", w.Header().Get("Deprecation"))

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		events := response["data"].([]interface{})
		assert.Len(t, events, 1)

		firstEvent := events[0].(map[string]interface{})
		assert.Equal(t, eventID, firstEvent["id"])
		assert.Equal(t, "order.created", firstEvent["topic"])
	})
}

func TestLegacyRetrieveEventByDestination(t *testing.T) {
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
		testutil.DeliveryFactory.WithStatus("success"),
		testutil.DeliveryFactory.WithTime(deliveryTime),
	)

	de := &models.DeliveryEvent{
		ID:            fmt.Sprintf("%s_%s", eventID, deliveryID),
		DestinationID: destinationID,
		Event:         *event,
		Delivery:      delivery,
	}

	require.NoError(t, result.logStore.InsertManyDeliveryEvent(context.Background(), []*models.DeliveryEvent{de}))

	t.Run("should retrieve event by destination with deprecation header", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/destinations/"+destinationID+"/events/"+eventID, nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "true", w.Header().Get("Deprecation"))

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		assert.Equal(t, eventID, response["id"])
		assert.Equal(t, "order.created", response["topic"])
	})

	t.Run("should return 404 for non-existent event", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/destinations/"+destinationID+"/events/nonexistent", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestLegacyListDeliveriesByEvent(t *testing.T) {
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
		testutil.DeliveryFactory.WithStatus("success"),
		testutil.DeliveryFactory.WithTime(deliveryTime),
	)

	de := &models.DeliveryEvent{
		ID:            fmt.Sprintf("%s_%s", eventID, deliveryID),
		DestinationID: destinationID,
		Event:         *event,
		Delivery:      delivery,
	}

	require.NoError(t, result.logStore.InsertManyDeliveryEvent(context.Background(), []*models.DeliveryEvent{de}))

	t.Run("should list deliveries for event with deprecation header", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/events/"+eventID+"/deliveries", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "true", w.Header().Get("Deprecation"))

		// Old format returns bare array, not {data: [...]}
		var deliveries []map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &deliveries))

		assert.Len(t, deliveries, 1)
		assert.Equal(t, deliveryID, deliveries[0]["id"])
		assert.Equal(t, "success", deliveries[0]["status"])
	})

	t.Run("should return empty list for non-existent event", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/events/nonexistent/deliveries", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		// Old format returns bare array
		var deliveries []map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &deliveries))

		assert.Len(t, deliveries, 0)
	})
}
