package apirouter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/logstore"
	"github.com/hookdeck/outpost/internal/logstore/driver"
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
	attemptDimensions = newStringSet("tenant_id", "destination_id", "destination_type", "topic", "status", "code", "manual", "attempt_number")
	attemptFilters    = newStringSet("tenant_id", "destination_id", "destination_type", "topic", "status", "code", "manual", "attempt_number")
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

// granularityMaxValues defines the maximum allowed value for each granularity
// unit, per the metrics API spec.
var granularityMaxValues = map[string]int{
	"s": 60,
	"m": 60,
	"h": 24,
	"d": 31,
	"w": 4,
	"M": 12,
}

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
	unit := m[2]
	if maxVal, ok := granularityMaxValues[unit]; ok && val > maxVal {
		return nil, fmt.Errorf("invalid granularity %q: %s value must be between 1 and %d", raw, unit, maxVal)
	}
	return &logstore.Granularity{Value: val, Unit: unit}, nil
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
	// JWT callers cannot use tenant_id as dimension
	if isJWTCaller(c) {
		if rejectTenantIDDimension(c) {
			return
		}
	}

	req, err := parseMetricsRequest(c, eventMeasures, eventDimensions, eventFilters)
	if err != nil {
		AbortWithError(c, http.StatusBadRequest, NewErrBadRequest(err))
		return
	}

	// JWT callers: validate/inject tenant_id filter
	if isJWTCaller(c) {
		if enforceJWTTenantFilter(c, req) {
			return
		}
	}

	resp, err := h.metricsStore.QueryEventMetrics(c.Request.Context(), *req)
	if err != nil {
		abortWithMetricsError(c, err)
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
	// JWT callers cannot use tenant_id as dimension
	if isJWTCaller(c) {
		if rejectTenantIDDimension(c) {
			return
		}
	}

	req, err := parseMetricsRequest(c, attemptMeasures, attemptDimensions, attemptFilters)
	if err != nil {
		AbortWithError(c, http.StatusBadRequest, NewErrBadRequest(err))
		return
	}

	// JWT callers: validate/inject tenant_id filter
	if isJWTCaller(c) {
		if enforceJWTTenantFilter(c, req) {
			return
		}
	}

	resp, err := h.metricsStore.QueryAttemptMetrics(c.Request.Context(), *req)
	if err != nil {
		abortWithMetricsError(c, err)
		return
	}

	apiData := make([]APIMetricsDataPoint, len(resp.Data))
	for i, dp := range resp.Data {
		apiData[i] = attemptDataPointToAPI(dp, req.Measures, req.Dimensions)
	}

	c.JSON(http.StatusOK, buildAPIMetricsResponse(apiData, resp.Metadata, req.Granularity))
}

// rejectTenantIDDimension aborts with 403 if the request includes tenant_id as a dimension.
// Returns true if the request was aborted.
func rejectTenantIDDimension(c *gin.Context) bool {
	for _, d := range ParseArrayQueryParam(c, "dimensions") {
		if d == "tenant_id" {
			AbortWithError(c, http.StatusForbidden, ErrorResponse{
				Code:    http.StatusForbidden,
				Message: "tenant_id dimension is not allowed for tenant-scoped requests",
			})
			return true
		}
	}
	return false
}

// enforceJWTTenantFilter validates filters[tenant_id] for JWT callers.
// If absent, injects the JWT tenant. If present but mismatched, aborts 403.
// Returns true if the request was aborted.
func enforceJWTTenantFilter(c *gin.Context, req *logstore.MetricsRequest) bool {
	jwtTenantID := tenantIDFromContext(c)
	if filterTenants, ok := req.Filters["tenant_id"]; ok {
		if len(filterTenants) != 1 || filterTenants[0] != jwtTenantID {
			AbortWithError(c, http.StatusForbidden, ErrorResponse{
				Code:    http.StatusForbidden,
				Message: "filters[tenant_id] does not match authenticated tenant",
			})
			return true
		}
	} else {
		if req.Filters == nil {
			req.Filters = make(map[string][]string)
		}
		req.Filters["tenant_id"] = []string{jwtTenantID}
	}
	return false
}

// abortWithMetricsError returns 400 for resource-limit and validation errors, 500 otherwise.
func abortWithMetricsError(c *gin.Context, err error) {
	if errors.Is(err, driver.ErrInvalidTimeRange) {
		AbortWithError(c, http.StatusBadRequest, NewErrBadRequest(err))
		return
	}
	if errors.Is(err, driver.ErrResourceLimit) {
		AbortWithError(c, http.StatusBadRequest, ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "query too broad: try fewer dimensions, more filters, or a shorter time range",
		})
		return
	}
	AbortWithError(c, http.StatusInternalServerError, NewErrInternalServer(err))
}

// --- Response transformation ---

func buildAPIMetricsResponse(data []APIMetricsDataPoint, meta logstore.MetricsMetadata, reqGranularity *logstore.Granularity) APIMetricsResponse {
	var gran *string
	if reqGranularity != nil {
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
		case "destination_type":
			dims["destination_type"] = derefString(dp.DestinationType)
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
