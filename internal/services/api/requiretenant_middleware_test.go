package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequireTenantMiddleware(t *testing.T) {
	t.Parallel()

	const apiKey = ""
	router, _, redisClient := setupTestRouter(t, apiKey, "")

	t.Run("should reject requests without a tenant", func(t *testing.T) {
		t.Parallel()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/invalid_tenant_id/destinations", nil)
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("should allow requests with a valid tenant", func(t *testing.T) {
		t.Parallel()

		tenant := models.Tenant{
			ID: uuid.New().String(),
		}
		entityStore := setupTestEntityStore(t, redisClient, nil)
		err := entityStore.UpsertTenant(context.Background(), tenant)
		require.Nil(t, err)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", baseAPIPath+"/"+tenant.ID+"/destinations", nil)
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}
