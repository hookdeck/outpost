package apirouter

import (
	"fmt"
	"net/http"
	"slices"
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
// Returns the direction (defaulting to "desc") and any validation error response.
// Valid values are "asc" and "desc".
func ParseDir(c *gin.Context) (string, *ErrorResponse) {
	dir := c.Query("dir")
	if dir == "" {
		return "desc", nil
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
// Returns the order_by value (defaulting to defaultValue) and any validation error response.
func ParseOrderBy(c *gin.Context, allowedValues []string, defaultValue string) (string, *ErrorResponse) {
	orderBy := c.Query("order_by")
	if orderBy == "" {
		return defaultValue, nil
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

// DateFilterResult holds the parsed date filter values.
type DateFilterResult struct {
	GTE *time.Time
	LTE *time.Time
}

// ParseDateFilter parses bracket notation date filters (e.g., field[gte], field[lte]).
// Returns the parsed dates and any validation error response.
// Accepts both RFC3339 format (2024-01-01T00:00:00Z) and date-only format (2024-01-01).
func ParseDateFilter(c *gin.Context, fieldName string) (*DateFilterResult, *ErrorResponse) {
	result := &DateFilterResult{}

	gteKey := fieldName + "[gte]"
	if gteStr := c.Query(gteKey); gteStr != "" {
		t, err := parseDateTime(gteStr)
		if err != nil {
			return nil, &ErrorResponse{
				Code:    http.StatusUnprocessableEntity,
				Message: "validation error",
				Data: map[string]string{
					"query." + gteKey: "invalid format, expected RFC3339 or YYYY-MM-DD",
				},
			}
		}
		result.GTE = &t
	}

	lteKey := fieldName + "[lte]"
	if lteStr := c.Query(lteKey); lteStr != "" {
		t, err := parseDateTime(lteStr)
		if err != nil {
			return nil, &ErrorResponse{
				Code:    http.StatusUnprocessableEntity,
				Message: "validation error",
				Data: map[string]string{
					"query." + lteKey: "invalid format, expected RFC3339 or YYYY-MM-DD",
				},
			}
		}
		result.LTE = &t
	}

	return result, nil
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
