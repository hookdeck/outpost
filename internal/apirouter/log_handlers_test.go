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

func TestListDeliveries(t *testing.T) {
	t.Parallel()

	result := setupTestRouterFull(t, "", "")

	// Create a tenant
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

	t.Run("should return empty list when no deliveries", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/"+tenantID+"/deliveries", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["data"].([]interface{})
		assert.Len(t, data, 0)
	})

	t.Run("should list deliveries", func(t *testing.T) {
		// Seed delivery events
		eventID := idgen.Event()
		deliveryID := idgen.Delivery()
		eventTime := time.Now().Add(-1 * time.Hour).Truncate(time.Millisecond)
		deliveryTime := eventTime.Add(100 * time.Millisecond)

		event := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithID(eventID),
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithDestinationID(destinationID),
			testutil.EventFactory.WithTopic("user.created"),
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

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/"+tenantID+"/deliveries", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["data"].([]interface{})
		assert.Len(t, data, 1)

		firstDelivery := data[0].(map[string]interface{})
		assert.Equal(t, deliveryID, firstDelivery["id"])
		assert.Equal(t, "success", firstDelivery["status"])
		assert.Equal(t, eventID, firstDelivery["event"]) // Not expanded
		assert.Equal(t, destinationID, firstDelivery["destination"])
	})

	t.Run("should expand event when expand=event", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/"+tenantID+"/deliveries?expand=event", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["data"].([]interface{})
		require.Len(t, data, 1)

		firstDelivery := data[0].(map[string]interface{})
		event := firstDelivery["event"].(map[string]interface{})
		assert.NotNil(t, event["id"])
		assert.Equal(t, "user.created", event["topic"])
		// data should not be present without expand=event.data
		assert.Nil(t, event["data"])
	})

	t.Run("should expand event.data when expand=event.data", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/"+tenantID+"/deliveries?expand=event.data", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["data"].([]interface{})
		require.Len(t, data, 1)

		firstDelivery := data[0].(map[string]interface{})
		event := firstDelivery["event"].(map[string]interface{})
		assert.NotNil(t, event["id"])
		assert.NotNil(t, event["data"]) // data should be present
	})

	t.Run("should filter by destination_id", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/"+tenantID+"/deliveries?destination_id="+destinationID, nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["data"].([]interface{})
		assert.Len(t, data, 1)
	})

	t.Run("should filter by non-existent destination_id", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/"+tenantID+"/deliveries?destination_id=nonexistent", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["data"].([]interface{})
		assert.Len(t, data, 0)
	})

	t.Run("should return 404 for non-existent tenant", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/nonexistent/deliveries", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestRetrieveDelivery(t *testing.T) {
	t.Parallel()

	result := setupTestRouterFull(t, "", "")

	// Create a tenant
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

	t.Run("should retrieve delivery by ID", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/"+tenantID+"/deliveries/"+deliveryID, nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		assert.Equal(t, deliveryID, response["id"])
		assert.Equal(t, "failed", response["status"])
		assert.Equal(t, eventID, response["event"]) // Not expanded
		assert.Equal(t, destinationID, response["destination"])
	})

	t.Run("should expand event when expand=event", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/"+tenantID+"/deliveries/"+deliveryID+"?expand=event", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		event := response["event"].(map[string]interface{})
		assert.Equal(t, eventID, event["id"])
		assert.Equal(t, "order.created", event["topic"])
		// data should not be present without expand=event.data
		assert.Nil(t, event["data"])
	})

	t.Run("should expand event.data when expand=event.data", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/"+tenantID+"/deliveries/"+deliveryID+"?expand=event.data", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		event := response["event"].(map[string]interface{})
		assert.Equal(t, eventID, event["id"])
		assert.NotNil(t, event["data"]) // data should be present
	})

	t.Run("should return 404 for non-existent delivery", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/"+tenantID+"/deliveries/nonexistent", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should return 404 for non-existent tenant", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/nonexistent/deliveries/"+deliveryID, nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestRetrieveEvent(t *testing.T) {
	t.Parallel()

	result := setupTestRouterFull(t, "", "")

	// Create a tenant
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
		testutil.EventFactory.WithTopic("payment.processed"),
		testutil.EventFactory.WithTime(eventTime),
		testutil.EventFactory.WithData(map[string]interface{}{
			"amount": 100.50,
		}),
		testutil.EventFactory.WithMetadata(map[string]string{
			"source": "stripe",
		}),
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

	t.Run("should retrieve event by ID", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/"+tenantID+"/events/"+eventID, nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		assert.Equal(t, eventID, response["id"])
		assert.Equal(t, tenantID, response["tenant_id"])
		assert.Equal(t, "payment.processed", response["topic"])
		assert.Equal(t, "stripe", response["metadata"].(map[string]interface{})["source"])
		assert.Equal(t, 100.50, response["data"].(map[string]interface{})["amount"])
	})

	t.Run("should return 404 for non-existent event", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/"+tenantID+"/events/nonexistent", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should return 404 for non-existent tenant", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/nonexistent/events/"+eventID, nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}
