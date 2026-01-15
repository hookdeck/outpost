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
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/deliveries", nil)
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
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/deliveries", nil)
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
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/deliveries?expand=event", nil)
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
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/deliveries?expand=event.data", nil)
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
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/deliveries?destination_id="+destinationID, nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["data"].([]interface{})
		assert.Len(t, data, 1)
	})

	t.Run("should filter by non-existent destination_id", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/deliveries?destination_id=nonexistent", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["data"].([]interface{})
		assert.Len(t, data, 0)
	})

	t.Run("should return 404 for non-existent tenant", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/nonexistent/deliveries", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should exclude response_data by default", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/deliveries", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["data"].([]interface{})
		require.Len(t, data, 1)

		firstDelivery := data[0].(map[string]interface{})
		assert.Nil(t, firstDelivery["response_data"])
	})

	t.Run("should include response_data with expand=response_data", func(t *testing.T) {
		// Seed a delivery with response_data
		eventID := idgen.Event()
		deliveryID := idgen.Delivery()
		eventTime := time.Now().Add(-30 * time.Minute).Truncate(time.Millisecond)
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
		delivery.ResponseData = map[string]interface{}{
			"body":   "OK",
			"status": float64(200),
		}

		de := &models.DeliveryEvent{
			ID:            fmt.Sprintf("%s_%s", eventID, deliveryID),
			DestinationID: destinationID,
			Event:         *event,
			Delivery:      delivery,
		}

		require.NoError(t, result.logStore.InsertManyDeliveryEvent(context.Background(), []*models.DeliveryEvent{de}))

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/deliveries?expand=response_data", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["data"].([]interface{})
		// Find the delivery we just created
		var foundDelivery map[string]interface{}
		for _, d := range data {
			del := d.(map[string]interface{})
			if del["id"] == deliveryID {
				foundDelivery = del
				break
			}
		}
		require.NotNil(t, foundDelivery, "delivery not found in response")
		require.NotNil(t, foundDelivery["response_data"], "response_data should be included")
		respData := foundDelivery["response_data"].(map[string]interface{})
		assert.Equal(t, "OK", respData["body"])
		assert.Equal(t, float64(200), respData["status"])
	})

	t.Run("should support comma-separated expand param", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/deliveries?expand=event,response_data", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["data"].([]interface{})
		require.GreaterOrEqual(t, len(data), 1)

		firstDelivery := data[0].(map[string]interface{})
		// event should be expanded (object, not string)
		event := firstDelivery["event"].(map[string]interface{})
		assert.NotNil(t, event["id"])
		assert.NotNil(t, event["topic"])
	})

	t.Run("should return validation error for invalid sort_by", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/deliveries?sort_by=invalid", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	})

	t.Run("should return validation error for invalid sort_order", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/deliveries?sort_order=invalid", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	})

	t.Run("should accept valid sort_by and sort_order params", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/deliveries?sort_by=event_time&sort_order=asc", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("should return validation error for invalid event_start time", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/deliveries?event_start=invalid", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	})

	t.Run("should return validation error for invalid event_end time", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/deliveries?event_end=invalid", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	})

	t.Run("should accept valid event_start and event_end params", func(t *testing.T) {
		now := time.Now().UTC()
		eventStart := now.Add(-2 * time.Hour).Format(time.RFC3339)
		eventEnd := now.Format(time.RFC3339)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/deliveries?event_start="+eventStart+"&event_end="+eventEnd, nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
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
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/deliveries/"+deliveryID, nil)
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
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/deliveries/"+deliveryID+"?expand=event", nil)
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
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/deliveries/"+deliveryID+"?expand=event.data", nil)
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
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/deliveries/nonexistent", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should return 404 for non-existent tenant", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/nonexistent/deliveries/"+deliveryID, nil)
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
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/events/"+eventID, nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		assert.Equal(t, eventID, response["id"])
		assert.Equal(t, "payment.processed", response["topic"])
		assert.Equal(t, "stripe", response["metadata"].(map[string]interface{})["source"])
		assert.Equal(t, 100.50, response["data"].(map[string]interface{})["amount"])
		// tenant_id is not included in API response (tenant-scoped via URL)
		assert.Nil(t, response["tenant_id"])
	})

	t.Run("should return 404 for non-existent event", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/events/nonexistent", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should return 404 for non-existent tenant", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/nonexistent/events/"+eventID, nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestListEvents(t *testing.T) {
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

	t.Run("should return empty list when no events", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/events", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["data"].([]interface{})
		assert.Len(t, data, 0)
	})

	t.Run("should list events", func(t *testing.T) {
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
			testutil.EventFactory.WithData(map[string]interface{}{
				"user_id": "123",
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

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/events", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["data"].([]interface{})
		assert.Len(t, data, 1)

		firstEvent := data[0].(map[string]interface{})
		assert.Equal(t, eventID, firstEvent["id"])
		assert.Equal(t, "user.created", firstEvent["topic"])
		assert.NotNil(t, firstEvent["data"])
	})

	t.Run("should filter by destination_id", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/events?destination_id="+destinationID, nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["data"].([]interface{})
		assert.GreaterOrEqual(t, len(data), 1)
	})

	t.Run("should filter by non-existent destination_id", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/events?destination_id=nonexistent", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["data"].([]interface{})
		assert.Len(t, data, 0)
	})

	t.Run("should filter by topic", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/events?topic=user.created", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["data"].([]interface{})
		assert.GreaterOrEqual(t, len(data), 1)
		for _, item := range data {
			event := item.(map[string]interface{})
			assert.Equal(t, "user.created", event["topic"])
		}
	})

	t.Run("should return 404 for non-existent tenant", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/nonexistent/events", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should return validation error for invalid start time", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/events?start=invalid", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	})

	t.Run("should return validation error for invalid end time", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/events?end=invalid", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	})

	t.Run("should return validation error for invalid sort_order", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/events?sort_order=invalid", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	})

	t.Run("should accept valid sort_order param", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/events?sort_order=asc", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}
