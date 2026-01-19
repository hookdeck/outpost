package apirouter

import (
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// SeekPagination represents cursor-based pagination metadata for list responses.
type SeekPagination struct {
	OrderBy string  `json:"order_by"`
	Dir     string  `json:"dir"`
	Limit   int     `json:"limit"`
	Next    *string `json:"next"`
	Prev    *string `json:"prev"`
}

// CursorToPtr converts an empty cursor string to nil, or returns a pointer to the string.
// Returns null for empty cursors instead of empty string.
func CursorToPtr(cursor string) *string {
	if cursor == "" {
		return nil
	}
	return &cursor
}

// CursorParams holds the parsed cursor values for pagination.
type CursorParams struct {
	Next string
	Prev string
}

// ParseCursors parses the "next" and "prev" query parameters.
// Returns an error if both are provided (mutually exclusive).
func ParseCursors(c *gin.Context) (CursorParams, *ErrorResponse) {
	next := c.Query("next")
	prev := c.Query("prev")

	if next != "" && prev != "" {
		return CursorParams{}, &ErrorResponse{
			Code:    http.StatusBadRequest,
			Message: "cannot specify both 'next' and 'prev' cursors",
		}
	}

	return CursorParams{Next: next, Prev: prev}, nil
}

// ParseDir parses the "dir" query parameter for sort direction.
// Returns the direction (empty string if not provided) and any validation error response.
// Valid values are "asc" and "desc". Caller/store should apply default if empty.
func ParseDir(c *gin.Context) (string, *ErrorResponse) {
	dir := c.Query("dir")
	if dir == "" {
		return "", nil
	}
	if dir != "asc" && dir != "desc" {
		return "", &ErrorResponse{
			Code:    http.StatusUnprocessableEntity,
			Message: "validation error",
			Data: map[string]string{
				"query.dir": "must be 'asc' or 'desc'",
			},
		}
	}
	return dir, nil
}

// ParseOrderBy parses the "order_by" query parameter and validates against allowed values.
// Returns the order_by value (empty string if not provided) and any validation error response.
// Caller/store should apply default if empty.
func ParseOrderBy(c *gin.Context, allowedValues []string) (string, *ErrorResponse) {
	orderBy := c.Query("order_by")
	if orderBy == "" {
		return "", nil
	}

	if slices.Contains(allowedValues, orderBy) {
		return orderBy, nil
	}

	return "", &ErrorResponse{
		Code:    http.StatusUnprocessableEntity,
		Message: "validation error",
		Data: map[string]string{
			"query.order_by": fmt.Sprintf("must be one of: %v", allowedValues),
		},
	}
}

// ParseArrayParam parses bracket notation array parameters (e.g., field[0]=a&field[1]=b).
// Returns the parsed array in index order. Supports both numeric indices [0], [1] and
// simple repeated params field[]=a&field[]=b.
func ParseArrayParam(c *gin.Context, fieldName string) []string {
	queryMap := c.Request.URL.Query()
	prefix := fieldName + "["

	// Collect all matching params
	type indexedValue struct {
		index int
		value string
	}
	var values []indexedValue
	var unindexed []string

	for key, vals := range queryMap {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		if len(vals) == 0 {
			continue
		}

		// Extract the part between [ and ]
		suffix := key[len(prefix):]
		if !strings.HasSuffix(suffix, "]") {
			continue
		}
		indexStr := suffix[:len(suffix)-1]

		if indexStr == "" {
			// field[]=value format - collect all values
			unindexed = append(unindexed, vals...)
		} else {
			// field[0]=value format - parse index
			idx, err := strconv.Atoi(indexStr)
			if err != nil {
				continue // skip non-numeric indices
			}
			values = append(values, indexedValue{index: idx, value: vals[0]})
		}
	}

	// If we have indexed values, sort by index and return
	if len(values) > 0 {
		sort.Slice(values, func(i, j int) bool {
			return values[i].index < values[j].index
		})
		result := make([]string, len(values))
		for i, v := range values {
			result[i] = v.value
		}
		return result
	}

	// Return unindexed values if any
	return unindexed
}

// DateFilterResult holds the parsed date filter values.
type DateFilterResult struct {
	GTE *time.Time // Greater than or equal (>=)
	LTE *time.Time // Less than or equal (<=)
	GT  *time.Time // Greater than (>)
	LT  *time.Time // Less than (<)
}

// unsupportedDateOps lists operators we recognize but don't support yet.
// When stores add support, move these to supported and update ParseDateFilter.
var unsupportedDateOps = []string{"any"}

// ParseDateFilter parses bracket notation date filters (e.g., field[gte], field[lte], field[gt], field[lt]).
// Returns 400 for unsupported operators (any).
// Accepts both RFC3339 format (2024-01-01T00:00:00Z) and date-only format (2024-01-01).
func ParseDateFilter(c *gin.Context, fieldName string) (*DateFilterResult, *ErrorResponse) {
	// Check for unsupported operators first
	for _, op := range unsupportedDateOps {
		key := fieldName + "[" + op + "]"
		if c.Query(key) != "" {
			return nil, &ErrorResponse{
				Code:    http.StatusBadRequest,
				Message: fmt.Sprintf("operator '%s' is not supported, use 'gte', 'lte', 'gt', or 'lt'", op),
			}
		}
	}

	result := &DateFilterResult{}

	// Parse [gte] - greater than or equal
	gteKey := fieldName + "[gte]"
	if gteStr := c.Query(gteKey); gteStr != "" {
		t, err := parseDateTime(gteStr)
		if err != nil {
			return nil, dateFormatError(gteKey)
		}
		result.GTE = &t
	}

	// Parse [lte] - less than or equal
	lteKey := fieldName + "[lte]"
	if lteStr := c.Query(lteKey); lteStr != "" {
		t, err := parseDateTime(lteStr)
		if err != nil {
			return nil, dateFormatError(lteKey)
		}
		result.LTE = &t
	}

	// Parse [gt] - greater than
	gtKey := fieldName + "[gt]"
	if gtStr := c.Query(gtKey); gtStr != "" {
		t, err := parseDateTime(gtStr)
		if err != nil {
			return nil, dateFormatError(gtKey)
		}
		result.GT = &t
	}

	// Parse [lt] - less than
	ltKey := fieldName + "[lt]"
	if ltStr := c.Query(ltKey); ltStr != "" {
		t, err := parseDateTime(ltStr)
		if err != nil {
			return nil, dateFormatError(ltKey)
		}
		result.LT = &t
	}

	return result, nil
}

// dateFormatError returns a validation error for invalid date format.
func dateFormatError(key string) *ErrorResponse {
	return &ErrorResponse{
		Code:    http.StatusUnprocessableEntity,
		Message: "validation error",
		Data: map[string]string{
			"query." + key: "invalid format, expected RFC3339 (e.g. 2024-01-15T09:30:00Z or 2024-01-15T09:30:00.123Z) or YYYY-MM-DD",
		},
	}
}

// Note: String and number filters are not yet supported by the stores.
// These types are defined for future use when stores add support.
// Attempting to use them will return 400 errors from the handlers.

// parseDateTime attempts to parse a date string in RFC3339, RFC3339Nano, or date-only (YYYY-MM-DD) format.
func parseDateTime(s string) (time.Time, error) {
	// Try RFC3339Nano first (handles milliseconds/microseconds like 2024-01-01T00:00:00.123Z)
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	// Try RFC3339 (no fractional seconds)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	// Try date-only format
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid date format")
}
