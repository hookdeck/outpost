package apirouter

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/stretchr/testify/assert"
)

// A create racing a tenant delete is refused by the store; the tenant is gone,
// so it's a 404 like any request to a deleted tenant, not a server error.
func TestHandleUpsertDestinationError_TenantDeleted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)

	(&DestinationHandlers{}).handleUpsertDestinationError(c, fmt.Errorf("create destination: %w", tenantstore.ErrTenantDeleted))

	assert.Equal(t, http.StatusNotFound, c.Writer.Status())
	assert.True(t, c.IsAborted())
	if assert.Len(t, c.Errors, 1) {
		assert.Equal(t, NewErrNotFound("tenant"), c.Errors[0].Err)
	}
}
