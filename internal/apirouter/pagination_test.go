package apirouter_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/apirouter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func TestCursorToPtr(t *testing.T) {
	t.Run("empty string returns nil", func(t *testing.T) {
		result := apirouter.CursorToPtr("")
		assert.Nil(t, result)
	})

	t.Run("non-empty string returns pointer", func(t *testing.T) {
		result := apirouter.CursorToPtr("abc123")
		require.NotNil(t, result)
		assert.Equal(t, "abc123", *result)
	})
}

func TestParseDir(t *testing.T) {
	tests := []struct {
		name        string
		queryDir    string
		wantDir     string
		wantErrCode int
	}{
		{
			name:     "empty defaults to desc",
			queryDir: "",
			wantDir:  "desc",
		},
		{
			name:     "asc is valid",
			queryDir: "asc",
			wantDir:  "asc",
		},
		{
			name:     "desc is valid",
			queryDir: "desc",
			wantDir:  "desc",
		},
		{
			name:        "invalid value returns error",
			queryDir:    "invalid",
			wantErrCode: http.StatusUnprocessableEntity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := createTestContext(map[string]string{"dir": tt.queryDir})

			dir, errResp := apirouter.ParseDir(c)

			if tt.wantErrCode != 0 {
				require.NotNil(t, errResp)
				assert.Equal(t, tt.wantErrCode, errResp.Code)
			} else {
				assert.Nil(t, errResp)
				assert.Equal(t, tt.wantDir, dir)
			}
		})
	}
}

func TestParseOrderBy(t *testing.T) {
	allowedValues := []string{"created_at", "event_time"}

	tests := []struct {
		name         string
		queryOrderBy string
		defaultValue string
		wantOrderBy  string
		wantErrCode  int
	}{
		{
			name:         "empty returns default",
			queryOrderBy: "",
			defaultValue: "created_at",
			wantOrderBy:  "created_at",
		},
		{
			name:         "valid value returns value",
			queryOrderBy: "event_time",
			defaultValue: "created_at",
			wantOrderBy:  "event_time",
		},
		{
			name:         "invalid value returns error",
			queryOrderBy: "invalid_field",
			defaultValue: "created_at",
			wantErrCode:  http.StatusUnprocessableEntity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := createTestContext(map[string]string{"order_by": tt.queryOrderBy})

			orderBy, errResp := apirouter.ParseOrderBy(c, allowedValues, tt.defaultValue)

			if tt.wantErrCode != 0 {
				require.NotNil(t, errResp)
				assert.Equal(t, tt.wantErrCode, errResp.Code)
			} else {
				assert.Nil(t, errResp)
				assert.Equal(t, tt.wantOrderBy, orderBy)
			}
		})
	}
}

func TestParseDateFilter(t *testing.T) {
	tests := []struct {
		name        string
		fieldName   string
		queryParams map[string]string
		wantGTE     *time.Time
		wantLTE     *time.Time
		wantErrCode int
	}{
		{
			name:        "no params returns empty result",
			fieldName:   "event_time",
			queryParams: map[string]string{},
			wantGTE:     nil,
			wantLTE:     nil,
		},
		{
			name:      "RFC3339 gte param",
			fieldName: "event_time",
			queryParams: map[string]string{
				"event_time[gte]": "2024-01-15T10:30:00Z",
			},
			wantGTE: timePtr(time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)),
			wantLTE: nil,
		},
		{
			name:      "RFC3339 lte param",
			fieldName: "event_time",
			queryParams: map[string]string{
				"event_time[lte]": "2024-01-31T23:59:59Z",
			},
			wantGTE: nil,
			wantLTE: timePtr(time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)),
		},
		{
			name:      "both gte and lte params",
			fieldName: "created_at",
			queryParams: map[string]string{
				"created_at[gte]": "2024-01-01T00:00:00Z",
				"created_at[lte]": "2024-01-31T23:59:59Z",
			},
			wantGTE: timePtr(time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)),
			wantLTE: timePtr(time.Date(2024, 1, 31, 23, 59, 59, 0, time.UTC)),
		},
		{
			name:      "date-only format",
			fieldName: "delivery_time",
			queryParams: map[string]string{
				"delivery_time[gte]": "2024-01-15",
			},
			wantGTE: timePtr(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)),
			wantLTE: nil,
		},
		{
			name:      "invalid gte format returns error",
			fieldName: "event_time",
			queryParams: map[string]string{
				"event_time[gte]": "not-a-date",
			},
			wantErrCode: http.StatusUnprocessableEntity,
		},
		{
			name:      "invalid lte format returns error",
			fieldName: "event_time",
			queryParams: map[string]string{
				"event_time[lte]": "invalid",
			},
			wantErrCode: http.StatusUnprocessableEntity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := createTestContext(tt.queryParams)

			result, errResp := apirouter.ParseDateFilter(c, tt.fieldName)

			if tt.wantErrCode != 0 {
				require.NotNil(t, errResp)
				assert.Equal(t, tt.wantErrCode, errResp.Code)
			} else {
				require.Nil(t, errResp)
				require.NotNil(t, result)

				if tt.wantGTE != nil {
					require.NotNil(t, result.GTE)
					assert.True(t, tt.wantGTE.Equal(*result.GTE), "GTE mismatch: want %v, got %v", tt.wantGTE, result.GTE)
				} else {
					assert.Nil(t, result.GTE)
				}

				if tt.wantLTE != nil {
					require.NotNil(t, result.LTE)
					assert.True(t, tt.wantLTE.Equal(*result.LTE), "LTE mismatch: want %v, got %v", tt.wantLTE, result.LTE)
				} else {
					assert.Nil(t, result.LTE)
				}
			}
		})
	}
}

func createTestContext(queryParams map[string]string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	values := url.Values{}
	for k, v := range queryParams {
		if v != "" {
			values.Set(k, v)
		}
	}

	c.Request = httptest.NewRequest(http.MethodGet, "/?"+values.Encode(), nil)
	return c, w
}

func timePtr(t time.Time) *time.Time {
	return &t
}
