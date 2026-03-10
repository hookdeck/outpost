package apirouter

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/logstore"
)

// logMetricsStore is the subset of logstore.LogStore needed by metrics handlers.
type logMetricsStore interface {
	QueryEventMetrics(ctx context.Context, req logstore.MetricsRequest) (*logstore.EventMetricsResponse, error)
	QueryAttemptMetrics(ctx context.Context, req logstore.MetricsRequest) (*logstore.AttemptMetricsResponse, error)
}

type MetricsHandlers struct {
	logger       *logging.Logger
	metricsStore logMetricsStore
}

func NewMetricsHandlers(logger *logging.Logger, metricsStore logMetricsStore) *MetricsHandlers {
	return &MetricsHandlers{
		logger:       logger,
		metricsStore: metricsStore,
	}
}

// --- Allowlists ---

type stringSet map[string]struct{}

func newStringSet(vals ...string) stringSet {
	s := make(stringSet, len(vals))
	for _, v := range vals {
		s[v] = struct{}{}
	}
	return s
}

func (s stringSet) contains(v string) bool {
	_, ok := s[v]
	return ok
}

var (
	eventMeasures   = newStringSet("count", "rate")
	eventDimensions = newStringSet("tenant_id", "topic", "destination_id")
	eventFilters    = newStringSet("tenant_id", "topic", "destination_id")

	attemptMeasures   = newStringSet("count", "successful_count", "failed_count", "error_rate", "first_attempt_count", "retry_count", "manual_retry_count", "avg_attempt_number", "rate", "successful_rate", "failed_rate")
	attemptDimensions = newStringSet("tenant_id", "destination_id", "topic", "status", "code", "manual", "attempt_number")
	attemptFilters    = newStringSet("tenant_id", "destination_id", "topic", "status", "code", "manual", "attempt_number")
)

// --- API response types ---

type APIMetricsDataPoint struct {
	TimeBucket *time.Time     `json:"time_bucket"`
	Dimensions map[string]any `json:"dimensions"`
	Metrics    map[string]any `json:"metrics"`
}

type APIMetricsResponse struct {
	Data     []APIMetricsDataPoint `json:"data"`
	Metadata APIMetricsMetadata    `json:"metadata"`
}

type APIMetricsMetadata struct {
	Granularity *string `json:"granularity"`
	QueryTimeMs int64   `json:"query_time_ms"`
	RowCount    int     `json:"row_count"`
	RowLimit    int     `json:"row_limit"`
	Truncated   bool    `json:"truncated"`
}

// --- Query param parsing ---

var granularityRegex = regexp.MustCompile(`^(\d+)([smhdwM])$`)

func parseGranularity(raw string) (*logstore.Granularity, error) {
	if raw == "" {
		return nil, nil
	}
	m := granularityRegex.FindStringSubmatch(raw)
	if m == nil {
		return nil, fmt.Errorf("invalid granularity %q: must match <number><unit> where unit is one of s,m,h,d,w,M", raw)
	}
	val, _ := strconv.Atoi(m[1])
	if val <= 0 {
		return nil, fmt.Errorf("invalid granularity %q: value must be > 0", raw)
	}
	return &logstore.Granularity{Value: val, Unit: m[2]}, nil
}

func parseMetricsRequest(c *gin.Context, allowedMeasures, allowedDimensions, allowedFilters stringSet) (*logstore.MetricsRequest, error) {
	// time[start] and time[end] are required
	startStr := c.Query("time[start]")
	endStr := c.Query("time[end]")
	if startStr == "" || endStr == "" {
		return nil, fmt.Errorf("time[start] and time[end] are required")
	}

	start, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		return nil, fmt.Errorf("invalid time[start]: %w", err)
	}
	end, err := time.Parse(time.RFC3339, endStr)
	if err != nil {
		return nil, fmt.Errorf("invalid time[end]: %w", err)
	}

	// granularity (optional)
	gran, err := parseGranularity(c.Query("granularity"))
	if err != nil {
		return nil, err
	}

	// measures[] (required)
	measures := ParseArrayQueryParam(c, "measures")
	if len(measures) == 0 {
		return nil, fmt.Errorf("at least one measures[] is required")
	}
	for _, m := range measures {
		if !allowedMeasures.contains(m) {
			return nil, fmt.Errorf("unknown measure %q", m)
		}
	}

	// dimensions[] (optional)
	dimensions := ParseArrayQueryParam(c, "dimensions")
	for _, d := range dimensions {
		if !allowedDimensions.contains(d) {
			return nil, fmt.Errorf("unknown dimension %q", d)
		}
	}

	// filters[key]=val
	filters := make(map[string][]string)
	for key := range allowedFilters {
		vals := ParseArrayQueryParam(c, "filters["+key+"]")
		if len(vals) > 0 {
			filters[key] = vals
		}
	}

	return &logstore.MetricsRequest{
		TimeRange: logstore.TimeRange{
			Start: start,
			End:   end,
		},
		Granularity: gran,
		Measures:    measures,
		Dimensions:  dimensions,
		Filters:     filters,
	}, nil
}

// isJWTCaller returns true when the request was authenticated via JWT (tenant role).
func isJWTCaller(c *gin.Context) bool {
	return mustRoleFromContext(c) == RoleTenant
}

// --- Handlers ---

// MetricsEvents handles GET /metrics/events
func (h *MetricsHandlers) MetricsEvents(c *gin.Context) {
	tenantIDs, ok := resolveTenantIDsFilter(c)
	if !ok {
		return
	}

	// JWT callers cannot use tenant_id as dimension or filter
	if isJWTCaller(c) {
		if err := rejectTenantIDAccess(c); err != nil {
			return
		}
	}

	req, err := parseMetricsRequest(c, eventMeasures, eventDimensions, eventFilters)
	if err != nil {
		AbortWithError(c, http.StatusBadRequest, NewErrBadRequest(err))
		return
	}
	if len(tenantIDs) > 0 {
		req.TenantID = tenantIDs[0]
	}

	resp, err := h.metricsStore.QueryEventMetrics(c.Request.Context(), *req)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	apiData := make([]APIMetricsDataPoint, len(resp.Data))
	for i, dp := range resp.Data {
		apiData[i] = eventDataPointToAPI(dp, req.Measures, req.Dimensions)
	}

	c.JSON(http.StatusOK, buildAPIMetricsResponse(apiData, resp.Metadata, req.Granularity))
}

// MetricsAttempts handles GET /metrics/attempts
func (h *MetricsHandlers) MetricsAttempts(c *gin.Context) {
	tenantIDs, ok := resolveTenantIDsFilter(c)
	if !ok {
		return
	}

	if isJWTCaller(c) {
		if err := rejectTenantIDAccess(c); err != nil {
			return
		}
	}

	req, err := parseMetricsRequest(c, attemptMeasures, attemptDimensions, attemptFilters)
	if err != nil {
		AbortWithError(c, http.StatusBadRequest, NewErrBadRequest(err))
		return
	}
	if len(tenantIDs) > 0 {
		req.TenantID = tenantIDs[0]
	}

	resp, err := h.metricsStore.QueryAttemptMetrics(c.Request.Context(), *req)
	if err != nil {
		AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
		return
	}

	apiData := make([]APIMetricsDataPoint, len(resp.Data))
	for i, dp := range resp.Data {
		apiData[i] = attemptDataPointToAPI(dp, req.Measures, req.Dimensions)
	}

	c.JSON(http.StatusOK, buildAPIMetricsResponse(apiData, resp.Metadata, req.Granularity))
}

// rejectTenantIDAccess aborts with 403 if the request includes tenant_id as a dimension or filter.
func rejectTenantIDAccess(c *gin.Context) error {
	for _, d := range ParseArrayQueryParam(c, "dimensions") {
		if d == "tenant_id" {
			AbortWithError(c, http.StatusForbidden, ErrorResponse{
				Code:    http.StatusForbidden,
				Message: "tenant_id dimension is not allowed for tenant-scoped requests",
			})
			return fmt.Errorf("forbidden")
		}
	}
	if vals := ParseArrayQueryParam(c, "filters[tenant_id]"); len(vals) > 0 {
		AbortWithError(c, http.StatusForbidden, ErrorResponse{
			Code:    http.StatusForbidden,
			Message: "tenant_id filter is not allowed for tenant-scoped requests",
		})
		return fmt.Errorf("forbidden")
	}
	return nil
}

// --- Response transformation ---

func buildAPIMetricsResponse(data []APIMetricsDataPoint, meta logstore.MetricsMetadata, reqGranularity *logstore.Granularity) APIMetricsResponse {
	var gran *string
	if meta.Granularity != "" {
		gran = &meta.Granularity
	} else if reqGranularity != nil {
		s := fmt.Sprintf("%d%s", reqGranularity.Value, reqGranularity.Unit)
		gran = &s
	}
	return APIMetricsResponse{
		Data: data,
		Metadata: APIMetricsMetadata{
			Granularity: gran,
			QueryTimeMs: meta.QueryTimeMs,
			RowCount:    meta.RowCount,
			RowLimit:    meta.RowLimit,
			Truncated:   meta.Truncated,
		},
	}
}

func eventDataPointToAPI(dp logstore.EventMetricsDataPoint, measures, dimensions []string) APIMetricsDataPoint {
	metrics := make(map[string]any, len(measures))
	for _, m := range measures {
		switch m {
		case "count":
			metrics["count"] = derefInt(dp.Count)
		case "rate":
			metrics["rate"] = derefFloat64(dp.Rate)
		}
	}

	dims := make(map[string]any, len(dimensions))
	for _, d := range dimensions {
		switch d {
		case "tenant_id":
			dims["tenant_id"] = derefString(dp.TenantID)
		case "topic":
			dims["topic"] = derefString(dp.Topic)
		case "destination_id":
			dims["destination_id"] = derefString(dp.DestinationID)
		}
	}

	return APIMetricsDataPoint{
		TimeBucket: dp.TimeBucket,
		Dimensions: dims,
		Metrics:    metrics,
	}
}

func attemptDataPointToAPI(dp logstore.AttemptMetricsDataPoint, measures, dimensions []string) APIMetricsDataPoint {
	metrics := make(map[string]any, len(measures))
	for _, m := range measures {
		switch m {
		case "count":
			metrics["count"] = derefInt(dp.Count)
		case "successful_count":
			metrics["successful_count"] = derefInt(dp.SuccessfulCount)
		case "failed_count":
			metrics["failed_count"] = derefInt(dp.FailedCount)
		case "error_rate":
			metrics["error_rate"] = derefFloat64(dp.ErrorRate)
		case "first_attempt_count":
			metrics["first_attempt_count"] = derefInt(dp.FirstAttemptCount)
		case "retry_count":
			metrics["retry_count"] = derefInt(dp.RetryCount)
		case "manual_retry_count":
			metrics["manual_retry_count"] = derefInt(dp.ManualRetryCount)
		case "avg_attempt_number":
			metrics["avg_attempt_number"] = derefFloat64(dp.AvgAttemptNumber)
		case "rate":
			metrics["rate"] = derefFloat64(dp.Rate)
		case "successful_rate":
			metrics["successful_rate"] = derefFloat64(dp.SuccessfulRate)
		case "failed_rate":
			metrics["failed_rate"] = derefFloat64(dp.FailedRate)
		}
	}

	dims := make(map[string]any, len(dimensions))
	for _, d := range dimensions {
		switch d {
		case "tenant_id":
			dims["tenant_id"] = derefString(dp.TenantID)
		case "destination_id":
			dims["destination_id"] = derefString(dp.DestinationID)
		case "topic":
			dims["topic"] = derefString(dp.Topic)
		case "status":
			dims["status"] = derefString(dp.Status)
		case "code":
			dims["code"] = derefString(dp.Code)
		case "manual":
			dims["manual"] = derefBool(dp.Manual)
		case "attempt_number":
			dims["attempt_number"] = derefInt(dp.AttemptNumber)
		}
	}

	return APIMetricsDataPoint{
		TimeBucket: dp.TimeBucket,
		Dimensions: dims,
		Metrics:    metrics,
	}
}

// --- Deref helpers ---

func derefInt(p *int) int {
	if p != nil {
		return *p
	}
	return 0
}

func derefFloat64(p *float64) float64 {
	if p != nil {
		return *p
	}
	return 0
}

func derefString(p *string) string {
	if p != nil {
		return *p
	}
	return ""
}

func derefBool(p *bool) bool {
	if p != nil {
		return *p
	}
	return false
}
