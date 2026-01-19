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
			name:     "empty returns empty (no default)",
			queryDir: "",
			wantDir:  "",
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
	allowedValues := []string{"created_at", "time"}

	tests := []struct {
		name         string
		queryOrderBy string
		wantOrderBy  string
		wantErrCode  int
	}{
		{
			name:         "empty returns empty (no default)",
			queryOrderBy: "",
			wantOrderBy:  "",
		},
		{
			name:         "valid value returns value",
			queryOrderBy: "time",
			wantOrderBy:  "time",
		},
		{
			name:         "another valid value",
			queryOrderBy: "created_at",
			wantOrderBy:  "created_at",
		},
		{
			name:         "invalid value returns error",
			queryOrderBy: "invalid_field",
			wantErrCode:  http.StatusUnprocessableEntity,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := createTestContext(map[string]string{"order_by": tt.queryOrderBy})

			orderBy, errResp := apirouter.ParseOrderBy(c, allowedValues)

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
			fieldName:   "time",
			queryParams: map[string]string{},
		},
		{
			name:      "RFC3339 gte param",
			fieldName: "time",
			queryParams: map[string]string{
				"time[gte]": "2024-01-15T10:30:00Z",
			},
			wantGTE: timePtr(time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)),
		},
		{
			name:      "RFC3339 lte param",
			fieldName: "time",
			queryParams: map[string]string{
				"time[lte]": "2024-01-31T23:59:59Z",
			},
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
			fieldName: "time",
			queryParams: map[string]string{
				"time[gte]": "2024-01-15",
			},
			wantGTE: timePtr(time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)),
		},
		{
			name:      "invalid gte format returns error",
			fieldName: "time",
			queryParams: map[string]string{
				"time[gte]": "not-a-date",
			},
			wantErrCode: http.StatusUnprocessableEntity,
		},
		{
			name:      "invalid lte format returns error",
			fieldName: "time",
			queryParams: map[string]string{
				"time[lte]": "invalid",
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
				return
			}

			require.Nil(t, errResp)
			require.NotNil(t, result)

			assertTimePtr(t, "GTE", tt.wantGTE, result.GTE)
			assertTimePtr(t, "LTE", tt.wantLTE, result.LTE)
		})
	}
}

func TestParseDateFilter_UnsupportedOperators(t *testing.T) {
	unsupportedOps := []string{"gt", "lt", "any"}

	for _, op := range unsupportedOps {
		t.Run(op+" returns 400", func(t *testing.T) {
			c, _ := createTestContext(map[string]string{
				"time[" + op + "]": "2024-01-01T00:00:00Z",
			})

			_, errResp := apirouter.ParseDateFilter(c, "time")

			require.NotNil(t, errResp)
			assert.Equal(t, http.StatusBadRequest, errResp.Code)
			assert.Contains(t, errResp.Message, "not supported")
		})
	}
}

func TestParseArrayParam(t *testing.T) {
	tests := []struct {
		name        string
		fieldName   string
		queryString string
		want        []string
	}{
		{
			name:        "no params returns empty slice",
			fieldName:   "item",
			queryString: "",
			want:        nil,
		},
		{
			name:        "indexed format",
			fieldName:   "item",
			queryString: "item[0]=hello&item[1]=world",
			want:        []string{"hello", "world"},
		},
		{
			name:        "indexed format out of order",
			fieldName:   "item",
			queryString: "item[2]=c&item[0]=a&item[1]=b",
			want:        []string{"a", "b", "c"},
		},
		{
			name:        "unindexed format",
			fieldName:   "tag",
			queryString: "tag[]=foo&tag[]=bar",
			want:        []string{"foo", "bar"},
		},
		{
			name:        "sparse indices",
			fieldName:   "item",
			queryString: "item[0]=first&item[5]=sixth",
			want:        []string{"first", "sixth"},
		},
		{
			name:        "ignores non-matching params",
			fieldName:   "item",
			queryString: "item[0]=hello&other=value&item[1]=world",
			want:        []string{"hello", "world"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, _ := createTestContextWithQuery(tt.queryString)

			result := apirouter.ParseArrayParam(c, tt.fieldName)

			assert.Equal(t, tt.want, result)
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

func createTestContextWithQuery(queryString string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	path := "/"
	if queryString != "" {
		path = "/?" + queryString
	}
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	return c, w
}

func timePtr(t time.Time) *time.Time {
	return &t
}

func assertTimePtr(t *testing.T, name string, want, got *time.Time) {
	t.Helper()
	if want != nil {
		require.NotNil(t, got, "%s should not be nil", name)
		assert.True(t, want.Equal(*got), "%s mismatch: want %v, got %v", name, want, got)
	} else {
		assert.Nil(t, got, "%s should be nil", name)
	}
}
