package apirouter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/apirouter"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPI_MetricsEvents(t *testing.T) {
	baseStart := time.Now().Add(-1 * time.Hour).UTC().Truncate(time.Second)
	baseEnd := time.Now().UTC().Truncate(time.Second)
	baseQS := "time[start]=" + baseStart.Format(time.RFC3339) +
		"&time[end]=" + baseEnd.Format(time.RFC3339)

	t.Run("happy path with granularity", func(t *testing.T) {
		h := newAPITest(t)

		e1 := ef.AnyPointer(ef.WithTenantID("t1"), ef.WithTopic("user.created"))
		require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
			{Event: e1, Attempt: attemptForEvent(e1)},
		}))

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/events?"+baseQS+"&measures[0]=count&granularity=1h", nil)
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)

		var result apirouter.APIMetricsResponse
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
		assert.NotNil(t, result.Metadata)
		assert.NotNil(t, result.Metadata.Granularity)
		assert.Equal(t, "1h", *result.Metadata.Granularity)
	})

	t.Run("happy path with dimensions", func(t *testing.T) {
		h := newAPITest(t)

		e1 := ef.AnyPointer(ef.WithTenantID("t1"), ef.WithTopic("user.created"))
		e2 := ef.AnyPointer(ef.WithTenantID("t1"), ef.WithTopic("user.updated"))
		require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
			{Event: e1, Attempt: attemptForEvent(e1)},
			{Event: e2, Attempt: attemptForEvent(e2)},
		}))

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/events?"+baseQS+"&measures[0]=count&dimensions[0]=topic", nil)
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)

		var result apirouter.APIMetricsResponse
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
		// Each data point should have a "topic" dimension and "count" metric
		for _, dp := range result.Data {
			assert.Contains(t, dp.Dimensions, "topic")
			assert.Contains(t, dp.Metrics, "count")
		}
	})

	t.Run("no granularity returns aggregate", func(t *testing.T) {
		h := newAPITest(t)

		e1 := ef.AnyPointer(ef.WithTenantID("t1"))
		require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
			{Event: e1, Attempt: attemptForEvent(e1)},
		}))

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/events?"+baseQS+"&measures[0]=count", nil)
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)

		var result apirouter.APIMetricsResponse
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
		assert.Nil(t, result.Metadata.Granularity)
		// Should have data (aggregate)
		if len(result.Data) > 0 {
			assert.Nil(t, result.Data[0].TimeBucket)
		}
	})

	t.Run("tenant isolation with JWT", func(t *testing.T) {
		h := newAPITest(t)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

		e1 := ef.AnyPointer(ef.WithTenantID("t1"))
		e2 := ef.AnyPointer(ef.WithTenantID("t2"))
		require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
			{Event: e1, Attempt: attemptForEvent(e1)},
			{Event: e2, Attempt: attemptForEvent(e2)},
		}))

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/events?"+baseQS+"&measures[0]=count", nil)
		resp := h.do(h.withJWT(req, "t1"))

		require.Equal(t, http.StatusOK, resp.Code)

		var result apirouter.APIMetricsResponse
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
		// Should only contain t1's data
		if len(result.Data) > 0 {
			count, ok := result.Data[0].Metrics["count"]
			assert.True(t, ok)
			// count should reflect only t1's event
			assert.Equal(t, float64(1), count)
		}
	})

	t.Run("admin can use tenant_id dimension", func(t *testing.T) {
		h := newAPITest(t)

		e1 := ef.AnyPointer(ef.WithTenantID("t1"))
		e2 := ef.AnyPointer(ef.WithTenantID("t2"))
		require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
			{Event: e1, Attempt: attemptForEvent(e1)},
			{Event: e2, Attempt: attemptForEvent(e2)},
		}))

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/events?"+baseQS+"&measures[0]=count&dimensions[0]=tenant_id", nil)
		resp := h.do(h.withAPIKey(req))

		// Admin should be allowed to use tenant_id dimension (not rejected like JWT)
		require.Equal(t, http.StatusOK, resp.Code)

		var result apirouter.APIMetricsResponse
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
		for _, dp := range result.Data {
			assert.Contains(t, dp.Dimensions, "tenant_id")
		}
	})

	t.Run("JWT rejected for tenant_id dimension", func(t *testing.T) {
		h := newAPITest(t)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/events?"+baseQS+"&measures[0]=count&dimensions[0]=tenant_id", nil)
		resp := h.do(h.withJWT(req, "t1"))

		assert.Equal(t, http.StatusForbidden, resp.Code)
	})

	t.Run("JWT rejected for tenant_id filter", func(t *testing.T) {
		h := newAPITest(t)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/events?"+baseQS+"&measures[0]=count&filters[tenant_id][0]=t1", nil)
		resp := h.do(h.withJWT(req, "t1"))

		assert.Equal(t, http.StatusForbidden, resp.Code)
	})

	t.Run("missing time returns 400", func(t *testing.T) {
		h := newAPITest(t)

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/events?measures[0]=count", nil)
		resp := h.do(h.withAPIKey(req))

		assert.Equal(t, http.StatusBadRequest, resp.Code)
	})

	t.Run("invalid granularity returns 400", func(t *testing.T) {
		h := newAPITest(t)

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/events?"+baseQS+"&measures[0]=count&granularity=invalid", nil)
		resp := h.do(h.withAPIKey(req))

		assert.Equal(t, http.StatusBadRequest, resp.Code)
	})

	t.Run("unknown measure returns 400", func(t *testing.T) {
		h := newAPITest(t)

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/events?"+baseQS+"&measures[0]=nonexistent", nil)
		resp := h.do(h.withAPIKey(req))

		assert.Equal(t, http.StatusBadRequest, resp.Code)
	})

	t.Run("unknown dimension returns 400", func(t *testing.T) {
		h := newAPITest(t)

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/events?"+baseQS+"&measures[0]=count&dimensions[0]=nonexistent", nil)
		resp := h.do(h.withAPIKey(req))

		assert.Equal(t, http.StatusBadRequest, resp.Code)
	})

	t.Run("start equals end returns 400", func(t *testing.T) {
		h := newAPITest(t)
		sameTime := time.Now().UTC().Truncate(time.Second)
		qs := "time[start]=" + sameTime.Format(time.RFC3339) +
			"&time[end]=" + sameTime.Format(time.RFC3339)

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/events?"+qs+"&measures[0]=count", nil)
		resp := h.do(h.withAPIKey(req))

		assert.Equal(t, http.StatusBadRequest, resp.Code)
	})

	t.Run("start after end returns 400", func(t *testing.T) {
		h := newAPITest(t)
		now := time.Now().UTC().Truncate(time.Second)
		qs := "time[start]=" + now.Format(time.RFC3339) +
			"&time[end]=" + now.Add(-1*time.Hour).Format(time.RFC3339)

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/events?"+qs+"&measures[0]=count", nil)
		resp := h.do(h.withAPIKey(req))

		assert.Equal(t, http.StatusBadRequest, resp.Code)
	})

	t.Run("missing measures returns 400", func(t *testing.T) {
		h := newAPITest(t)

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/events?"+baseQS, nil)
		resp := h.do(h.withAPIKey(req))

		assert.Equal(t, http.StatusBadRequest, resp.Code)
	})

	t.Run("filter by topic", func(t *testing.T) {
		h := newAPITest(t)

		e1 := ef.AnyPointer(ef.WithTenantID("t1"), ef.WithTopic("user.created"))
		e2 := ef.AnyPointer(ef.WithTenantID("t1"), ef.WithTopic("user.updated"))
		require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
			{Event: e1, Attempt: attemptForEvent(e1)},
			{Event: e2, Attempt: attemptForEvent(e2)},
		}))

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/events?"+baseQS+"&measures[0]=count&filters[topic][0]=user.created", nil)
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)

		var result apirouter.APIMetricsResponse
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
		if len(result.Data) > 0 {
			count, ok := result.Data[0].Metrics["count"]
			assert.True(t, ok)
			assert.Equal(t, float64(1), count)
		}
	})

	t.Run("empty results", func(t *testing.T) {
		h := newAPITest(t)

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/events?"+baseQS+"&measures[0]=count", nil)
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)

		var result apirouter.APIMetricsResponse
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
		assert.Empty(t, result.Data)
	})

	t.Run("rate measure returns float", func(t *testing.T) {
		h := newAPITest(t)

		e1 := ef.AnyPointer(ef.WithTenantID("t1"))
		require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
			{Event: e1, Attempt: attemptForEvent(e1)},
		}))

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/events?"+baseQS+"&measures[0]=rate", nil)
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)

		var result apirouter.APIMetricsResponse
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
		if len(result.Data) > 0 {
			rate, ok := result.Data[0].Metrics["rate"]
			assert.True(t, ok)
			rateVal, _ := rate.(float64)
			assert.Greater(t, rateVal, 0.0)
			// count should not appear since user didn't request it
			_, hasCount := result.Data[0].Metrics["count"]
			assert.False(t, hasCount)
		}
	})

	t.Run("rate and count together", func(t *testing.T) {
		h := newAPITest(t)

		e1 := ef.AnyPointer(ef.WithTenantID("t1"))
		require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
			{Event: e1, Attempt: attemptForEvent(e1)},
		}))

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/events?"+baseQS+"&measures[0]=count&measures[1]=rate", nil)
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)

		var result apirouter.APIMetricsResponse
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
		if len(result.Data) > 0 {
			assert.Contains(t, result.Data[0].Metrics, "count")
			assert.Contains(t, result.Data[0].Metrics, "rate")
		}
	})
}

func TestAPI_MetricsAttempts(t *testing.T) {
	baseStart := time.Now().Add(-1 * time.Hour).UTC().Truncate(time.Second)
	baseEnd := time.Now().UTC().Truncate(time.Second)
	baseQS := "time[start]=" + baseStart.Format(time.RFC3339) +
		"&time[end]=" + baseEnd.Format(time.RFC3339)

	t.Run("happy path with multiple measures", func(t *testing.T) {
		h := newAPITest(t)

		e1 := ef.AnyPointer(ef.WithTenantID("t1"))
		a1 := attemptForEvent(e1, af.WithStatus("successful"))
		require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
			{Event: e1, Attempt: a1},
		}))

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/attempts?"+baseQS+"&measures[0]=count&measures[1]=successful_count&measures[2]=error_rate", nil)
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)

		var result apirouter.APIMetricsResponse
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
		if len(result.Data) > 0 {
			assert.Contains(t, result.Data[0].Metrics, "count")
			assert.Contains(t, result.Data[0].Metrics, "successful_count")
			assert.Contains(t, result.Data[0].Metrics, "error_rate")
		}
	})

	t.Run("with granularity and dimensions", func(t *testing.T) {
		h := newAPITest(t)

		e1 := ef.AnyPointer(ef.WithTenantID("t1"))
		a1 := attemptForEvent(e1, af.WithStatus("successful"))
		a2 := attemptForEvent(e1, af.WithStatus("failed"))
		require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
			{Event: e1, Attempt: a1},
			{Event: e1, Attempt: a2},
		}))

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/attempts?"+baseQS+"&measures[0]=count&granularity=1h&dimensions[0]=status", nil)
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)

		var result apirouter.APIMetricsResponse
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
		for _, dp := range result.Data {
			assert.Contains(t, dp.Dimensions, "status")
			assert.Contains(t, dp.Metrics, "count")
		}
	})

	t.Run("tenant isolation with JWT", func(t *testing.T) {
		h := newAPITest(t)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

		e1 := ef.AnyPointer(ef.WithTenantID("t1"))
		e2 := ef.AnyPointer(ef.WithTenantID("t2"))
		require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
			{Event: e1, Attempt: attemptForEvent(e1)},
			{Event: e2, Attempt: attemptForEvent(e2)},
		}))

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/attempts?"+baseQS+"&measures[0]=count", nil)
		resp := h.do(h.withJWT(req, "t1"))

		require.Equal(t, http.StatusOK, resp.Code)

		var result apirouter.APIMetricsResponse
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
		if len(result.Data) > 0 {
			count, ok := result.Data[0].Metrics["count"]
			assert.True(t, ok)
			assert.Equal(t, float64(1), count)
		}
	})

	t.Run("JWT rejected for tenant_id dimension", func(t *testing.T) {
		h := newAPITest(t)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/attempts?"+baseQS+"&measures[0]=count&dimensions[0]=tenant_id", nil)
		resp := h.do(h.withJWT(req, "t1"))

		assert.Equal(t, http.StatusForbidden, resp.Code)
	})

	t.Run("filter by status", func(t *testing.T) {
		h := newAPITest(t)

		e1 := ef.AnyPointer(ef.WithTenantID("t1"))
		a1 := attemptForEvent(e1, af.WithStatus("successful"))
		a2 := attemptForEvent(e1, af.WithStatus("failed"))
		require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
			{Event: e1, Attempt: a1},
			{Event: e1, Attempt: a2},
		}))

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/attempts?"+baseQS+"&measures[0]=count&filters[status][0]=successful", nil)
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)

		var result apirouter.APIMetricsResponse
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
		if len(result.Data) > 0 {
			count, ok := result.Data[0].Metrics["count"]
			assert.True(t, ok)
			assert.Equal(t, float64(1), count)
		}
	})

	t.Run("start equals end returns 400", func(t *testing.T) {
		h := newAPITest(t)
		sameTime := time.Now().UTC().Truncate(time.Second)
		qs := "time[start]=" + sameTime.Format(time.RFC3339) +
			"&time[end]=" + sameTime.Format(time.RFC3339)

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/attempts?"+qs+"&measures[0]=count", nil)
		resp := h.do(h.withAPIKey(req))

		assert.Equal(t, http.StatusBadRequest, resp.Code)
	})

	t.Run("unknown attempt measure returns 400", func(t *testing.T) {
		h := newAPITest(t)

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/attempts?"+baseQS+"&measures[0]=nonexistent", nil)
		resp := h.do(h.withAPIKey(req))

		assert.Equal(t, http.StatusBadRequest, resp.Code)
	})

	t.Run("empty results", func(t *testing.T) {
		h := newAPITest(t)

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/attempts?"+baseQS+"&measures[0]=count", nil)
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)

		var result apirouter.APIMetricsResponse
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
		assert.Empty(t, result.Data)
	})

	t.Run("rate measures without counts", func(t *testing.T) {
		h := newAPITest(t)

		e1 := ef.AnyPointer(ef.WithTenantID("t1"))
		a1 := attemptForEvent(e1, af.WithStatus("successful"))
		a2 := attemptForEvent(e1, af.WithStatus("failed"))
		require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
			{Event: e1, Attempt: a1},
			{Event: e1, Attempt: a2},
		}))

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/attempts?"+baseQS+"&measures[0]=rate&measures[1]=successful_rate&measures[2]=failed_rate", nil)
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)

		var result apirouter.APIMetricsResponse
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
		if len(result.Data) > 0 {
			dp := result.Data[0]
			assert.Contains(t, dp.Metrics, "rate")
			assert.Contains(t, dp.Metrics, "successful_rate")
			assert.Contains(t, dp.Metrics, "failed_rate")
			// Injected dependencies should not appear
			assert.NotContains(t, dp.Metrics, "count")
			assert.NotContains(t, dp.Metrics, "successful_count")
			assert.NotContains(t, dp.Metrics, "failed_count")
		}
	})

	t.Run("rate with granularity", func(t *testing.T) {
		h := newAPITest(t)

		e1 := ef.AnyPointer(ef.WithTenantID("t1"))
		require.NoError(t, h.logStore.InsertMany(t.Context(), []*models.LogEntry{
			{Event: e1, Attempt: attemptForEvent(e1)},
		}))

		req := httptest.NewRequest(http.MethodGet,
			"/api/v1/metrics/attempts?"+baseQS+"&measures[0]=count&measures[1]=rate&granularity=1h", nil)
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)

		var result apirouter.APIMetricsResponse
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &result))
		for _, dp := range result.Data {
			assert.Contains(t, dp.Metrics, "count")
			assert.Contains(t, dp.Metrics, "rate")
		}
	})
}
