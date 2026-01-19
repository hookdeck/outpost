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

// PaginationInfo represents pagination metadata for list responses.
// This aligns with Hookdeck's pagination response format.
type PaginationInfo struct {
	OrderBy string  `json:"order_by"`
	Dir     string  `json:"dir"`
	Limit   int     `json:"limit"`
	Next    *string `json:"next"`
	Prev    *string `json:"prev"`
}

// CursorToPtr converts an empty cursor string to nil, or returns a pointer to the string.
// Hookdeck returns null for empty cursors instead of empty string.
func CursorToPtr(cursor string) *string {
	if cursor == "" {
		return nil
	}
	return &cursor
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
// Supports all Hookdeck query operators for dates: gte, gt, lte, lt, any.
type DateFilterResult struct {
	GTE *time.Time // Greater than or equal
	GT  *time.Time // Greater than
	LTE *time.Time // Less than or equal
	LT  *time.Time // Less than
	Any bool       // Field is not null (any value present)
}

// ParseDateFilter parses bracket notation date filters (e.g., field[gte], field[lte], field[gt], field[lt], field[any]).
// Returns the parsed dates and any validation error response.
// Accepts both RFC3339 format (2024-01-01T00:00:00Z) and date-only format (2024-01-01).
func ParseDateFilter(c *gin.Context, fieldName string) (*DateFilterResult, *ErrorResponse) {
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

	// Parse [gt] - greater than
	gtKey := fieldName + "[gt]"
	if gtStr := c.Query(gtKey); gtStr != "" {
		t, err := parseDateTime(gtStr)
		if err != nil {
			return nil, dateFormatError(gtKey)
		}
		result.GT = &t
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

	// Parse [lt] - less than
	ltKey := fieldName + "[lt]"
	if ltStr := c.Query(ltKey); ltStr != "" {
		t, err := parseDateTime(ltStr)
		if err != nil {
			return nil, dateFormatError(ltKey)
		}
		result.LT = &t
	}

	// Parse [any] - field is not null
	anyKey := fieldName + "[any]"
	if anyStr := c.Query(anyKey); anyStr != "" {
		result.Any = anyStr == "true" || anyStr == "1"
	}

	return result, nil
}

// dateFormatError returns a validation error for invalid date format.
func dateFormatError(key string) *ErrorResponse {
	return &ErrorResponse{
		Code:    http.StatusUnprocessableEntity,
		Message: "validation error",
		Data: map[string]string{
			"query." + key: "invalid format, expected RFC3339 or YYYY-MM-DD",
		},
	}
}

// StringFilterResult holds the parsed string filter values.
// Supports Hookdeck query operators for strings: contains, any.
type StringFilterResult struct {
	Contains *string // Contains substring
	Any      bool    // Field is not null (any value present)
}

// ParseStringFilter parses bracket notation string filters (e.g., field[contains], field[any]).
// Returns the parsed filter values and any validation error response.
func ParseStringFilter(c *gin.Context, fieldName string) *StringFilterResult {
	result := &StringFilterResult{}

	// Parse [contains] - contains substring
	containsKey := fieldName + "[contains]"
	if containsStr := c.Query(containsKey); containsStr != "" {
		result.Contains = &containsStr
	}

	// Parse [any] - field is not null
	anyKey := fieldName + "[any]"
	if anyStr := c.Query(anyKey); anyStr != "" {
		result.Any = anyStr == "true" || anyStr == "1"
	}

	return result
}

// NumberFilterResult holds the parsed number filter values.
// Supports Hookdeck query operators for numbers: gte, gt, lte, lt, any.
type NumberFilterResult struct {
	GTE *float64 // Greater than or equal
	GT  *float64 // Greater than
	LTE *float64 // Less than or equal
	LT  *float64 // Less than
	Any bool     // Field is not null (any value present)
}

// ParseNumberFilter parses bracket notation number filters (e.g., field[gte], field[lte]).
// Returns the parsed numbers and any validation error response.
func ParseNumberFilter(c *gin.Context, fieldName string) (*NumberFilterResult, *ErrorResponse) {
	result := &NumberFilterResult{}

	// Parse [gte] - greater than or equal
	gteKey := fieldName + "[gte]"
	if gteStr := c.Query(gteKey); gteStr != "" {
		n, err := parseNumber(gteStr)
		if err != nil {
			return nil, numberFormatError(gteKey)
		}
		result.GTE = &n
	}

	// Parse [gt] - greater than
	gtKey := fieldName + "[gt]"
	if gtStr := c.Query(gtKey); gtStr != "" {
		n, err := parseNumber(gtStr)
		if err != nil {
			return nil, numberFormatError(gtKey)
		}
		result.GT = &n
	}

	// Parse [lte] - less than or equal
	lteKey := fieldName + "[lte]"
	if lteStr := c.Query(lteKey); lteStr != "" {
		n, err := parseNumber(lteStr)
		if err != nil {
			return nil, numberFormatError(lteKey)
		}
		result.LTE = &n
	}

	// Parse [lt] - less than
	ltKey := fieldName + "[lt]"
	if ltStr := c.Query(ltKey); ltStr != "" {
		n, err := parseNumber(ltStr)
		if err != nil {
			return nil, numberFormatError(ltKey)
		}
		result.LT = &n
	}

	// Parse [any] - field is not null
	anyKey := fieldName + "[any]"
	if anyStr := c.Query(anyKey); anyStr != "" {
		result.Any = anyStr == "true" || anyStr == "1"
	}

	return result, nil
}

// numberFormatError returns a validation error for invalid number format.
func numberFormatError(key string) *ErrorResponse {
	return &ErrorResponse{
		Code:    http.StatusUnprocessableEntity,
		Message: "validation error",
		Data: map[string]string{
			"query." + key: "invalid format, expected a number",
		},
	}
}

// parseNumber attempts to parse a number string.
func parseNumber(s string) (float64, error) {
	var n float64
	_, err := fmt.Sscanf(s, "%f", &n)
	return n, err
}

// parseDateTime attempts to parse a date string in RFC3339 or date-only (YYYY-MM-DD) format.
func parseDateTime(s string) (time.Time, error) {
	// Try RFC3339 first
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	// Try date-only format
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("invalid date format")
}
