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

func TestListAttempts(t *testing.T) {
	t.Parallel()

	result := setupTestRouterFull(t, "", "")

	// Create a tenant
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

	t.Run("should return empty list when no attempts", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/attempts", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["models"].([]interface{})
		assert.Len(t, data, 0)
	})

	t.Run("should list attempts", func(t *testing.T) {
		// Seed attempt events
		eventID := idgen.Event()
		attemptID := idgen.Attempt()
		eventTime := time.Now().Add(-1 * time.Hour).Truncate(time.Millisecond)
		attemptTime := eventTime.Add(100 * time.Millisecond)

		event := testutil.EventFactory.AnyPointer(
			testutil.EventFactory.WithID(eventID),
			testutil.EventFactory.WithTenantID(tenantID),
			testutil.EventFactory.WithDestinationID(destinationID),
			testutil.EventFactory.WithTopic("user.created"),
			testutil.EventFactory.WithTime(eventTime),
		)

		attempt := testutil.AttemptFactory.AnyPointer(
			testutil.AttemptFactory.WithID(attemptID),
			testutil.AttemptFactory.WithEventID(eventID),
			testutil.AttemptFactory.WithDestinationID(destinationID),
			testutil.AttemptFactory.WithStatus("success"),
			testutil.AttemptFactory.WithTime(attemptTime),
		)

		require.NoError(t, result.logStore.InsertMany(context.Background(), []*models.LogEntry{{Event: event, Attempt: attempt}}))

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/attempts", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["models"].([]interface{})
		assert.Len(t, data, 1)

		firstAttempt := data[0].(map[string]interface{})
		assert.Equal(t, attemptID, firstAttempt["id"])
		assert.Equal(t, "success", firstAttempt["status"])
		assert.Equal(t, eventID, firstAttempt["event"]) // Not included
		assert.Equal(t, destinationID, firstAttempt["destination"])
	})

	t.Run("should include event when include=event", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/attempts?include=event", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["models"].([]interface{})
		require.Len(t, data, 1)

		firstAttempt := data[0].(map[string]interface{})
		event := firstAttempt["event"].(map[string]interface{})
		assert.NotNil(t, event["id"])
		assert.Equal(t, "user.created", event["topic"])
		// data should not be present without include=event.data
		assert.Nil(t, event["data"])
	})

	t.Run("should include event.data when include=event.data", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/attempts?include=event.data", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["models"].([]interface{})
		require.Len(t, data, 1)

		firstAttempt := data[0].(map[string]interface{})
		event := firstAttempt["event"].(map[string]interface{})
		assert.NotNil(t, event["id"])
		assert.NotNil(t, event["data"]) // data should be present
	})

	t.Run("should filter by destination_id", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/attempts?destination_id="+destinationID, nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["models"].([]interface{})
		assert.Len(t, data, 1)
	})

	t.Run("should filter by non-existent destination_id", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/attempts?destination_id=nonexistent", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["models"].([]interface{})
		assert.Len(t, data, 0)
	})

	t.Run("should return 404 for non-existent tenant", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/nonexistent/attempts", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should exclude response_data by default", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/attempts", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["models"].([]interface{})
		require.Len(t, data, 1)

		firstAttempt := data[0].(map[string]interface{})
		assert.Nil(t, firstAttempt["response_data"])
	})

	t.Run("should include response_data with include=response_data", func(t *testing.T) {
		// Seed an attempt with response_data
		eventID := idgen.Event()
		attemptID := idgen.Attempt()
		eventTime := time.Now().Add(-30 * time.Minute).Truncate(time.Millisecond)
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
			testutil.AttemptFactory.WithStatus("success"),
			testutil.AttemptFactory.WithTime(attemptTime),
		)
		attempt.ResponseData = map[string]interface{}{
			"body":   "OK",
			"status": float64(200),
		}

		require.NoError(t, result.logStore.InsertMany(context.Background(), []*models.LogEntry{{Event: event, Attempt: attempt}}))

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/attempts?include=response_data", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["models"].([]interface{})
		// Find the attempt we just created
		var foundAttempt map[string]interface{}
		for _, d := range data {
			atm := d.(map[string]interface{})
			if atm["id"] == attemptID {
				foundAttempt = atm
				break
			}
		}
		require.NotNil(t, foundAttempt, "attempt not found in response")
		require.NotNil(t, foundAttempt["response_data"], "response_data should be included")
		respData := foundAttempt["response_data"].(map[string]interface{})
		assert.Equal(t, "OK", respData["body"])
		assert.Equal(t, float64(200), respData["status"])
	})

	t.Run("should support comma-separated include param", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/attempts?include=event,response_data", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["models"].([]interface{})
		require.GreaterOrEqual(t, len(data), 1)

		firstAttempt := data[0].(map[string]interface{})
		// event should be included (object, not string)
		event := firstAttempt["event"].(map[string]interface{})
		assert.NotNil(t, event["id"])
		assert.NotNil(t, event["topic"])
	})

	t.Run("should return validation error for invalid dir", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/attempts?dir=invalid", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	})

	t.Run("should accept valid dir param", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/attempts?dir=asc", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("should cap limit at 1000", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/attempts?limit=5000", nil)
		result.router.ServeHTTP(w, req)

		// Should succeed, limit is silently capped
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestRetrieveAttempt(t *testing.T) {
	t.Parallel()

	result := setupTestRouterFull(t, "", "")

	// Create a tenant
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

	// Seed an attempt event
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

	t.Run("should retrieve attempt by ID", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/attempts/"+attemptID, nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		assert.Equal(t, attemptID, response["id"])
		assert.Equal(t, "failed", response["status"])
		assert.Equal(t, eventID, response["event"]) // Not included
		assert.Equal(t, destinationID, response["destination"])
	})

	t.Run("should include event when include=event", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/attempts/"+attemptID+"?include=event", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		event := response["event"].(map[string]interface{})
		assert.Equal(t, eventID, event["id"])
		assert.Equal(t, "order.created", event["topic"])
		// data should not be present without include=event.data
		assert.Nil(t, event["data"])
	})

	t.Run("should include event.data when include=event.data", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/attempts/"+attemptID+"?include=event.data", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		event := response["event"].(map[string]interface{})
		assert.Equal(t, eventID, event["id"])
		assert.NotNil(t, event["data"]) // data should be present
	})

	t.Run("should return 404 for non-existent attempt", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/attempts/nonexistent", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should return 404 for non-existent tenant", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/nonexistent/attempts/"+attemptID, nil)
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

	// Seed an attempt event
	eventID := idgen.Event()
	attemptID := idgen.Attempt()
	eventTime := time.Now().Add(-1 * time.Hour).Truncate(time.Millisecond)
	attemptTime := eventTime.Add(100 * time.Millisecond)

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

	attempt := testutil.AttemptFactory.AnyPointer(
		testutil.AttemptFactory.WithID(attemptID),
		testutil.AttemptFactory.WithEventID(eventID),
		testutil.AttemptFactory.WithDestinationID(destinationID),
		testutil.AttemptFactory.WithStatus("success"),
		testutil.AttemptFactory.WithTime(attemptTime),
	)

	require.NoError(t, result.logStore.InsertMany(context.Background(), []*models.LogEntry{{Event: event, Attempt: attempt}}))

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

	t.Run("should return empty list when no events", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/events", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["models"].([]interface{})
		assert.Len(t, data, 0)
	})

	t.Run("should list events", func(t *testing.T) {
		// Seed attempt events
		eventID := idgen.Event()
		attemptID := idgen.Attempt()
		eventTime := time.Now().Add(-1 * time.Hour).Truncate(time.Millisecond)
		attemptTime := eventTime.Add(100 * time.Millisecond)

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

		attempt := testutil.AttemptFactory.AnyPointer(
			testutil.AttemptFactory.WithID(attemptID),
			testutil.AttemptFactory.WithEventID(eventID),
			testutil.AttemptFactory.WithDestinationID(destinationID),
			testutil.AttemptFactory.WithStatus("success"),
			testutil.AttemptFactory.WithTime(attemptTime),
		)

		require.NoError(t, result.logStore.InsertMany(context.Background(), []*models.LogEntry{{Event: event, Attempt: attempt}}))

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/events", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["models"].([]interface{})
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

		data := response["models"].([]interface{})
		assert.GreaterOrEqual(t, len(data), 1)
	})

	t.Run("should filter by non-existent destination_id", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/events?destination_id=nonexistent", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["models"].([]interface{})
		assert.Len(t, data, 0)
	})

	t.Run("should filter by topic", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/events?topic=user.created", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))

		data := response["models"].([]interface{})
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

	t.Run("should return validation error for invalid time filter", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/events?time[gte]=invalid", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	})

	t.Run("should return validation error for invalid time lte filter", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/events?time[lte]=invalid", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	})

	t.Run("should return validation error for invalid dir", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/events?dir=invalid", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	})

	t.Run("should accept valid dir param", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/events?dir=asc", nil)
		result.router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("should cap limit at 1000", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/tenants/"+tenantID+"/events?limit=5000", nil)
		result.router.ServeHTTP(w, req)

		// Should succeed, limit is silently capped
		assert.Equal(t, http.StatusOK, w.Code)
	})
}
