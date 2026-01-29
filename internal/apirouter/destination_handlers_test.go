package apirouter_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestDestinationCreateHandler(t *testing.T) {
	t.Parallel()

	router, _, redisClient := setupTestRouter(t, "", "")
	tenantStore := setupTestTenantStore(t, redisClient)

	t.Run("should set updated_at equal to created_at on creation", func(t *testing.T) {
		t.Parallel()

		// Setup - create tenant first
		tenantID := idgen.String()
		tenant := models.Tenant{
			ID:        tenantID,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		err := tenantStore.UpsertTenant(context.Background(), tenant)
		if err != nil {
			t.Fatal(err)
		}

		// Create destination request
		body := map[string]any{
			"type":   "webhook",
			"topics": []string{"*"},
			"config": map[string]string{
				"url": "https://example.com/webhook",
			},
		}
		bodyBytes, _ := json.Marshal(body)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", baseAPIPath+"/tenants/"+tenantID+"/destinations", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)

		var response map[string]any
		json.Unmarshal(w.Body.Bytes(), &response)

		assert.Equal(t, http.StatusCreated, w.Code)
		assert.NotEqual(t, "", response["created_at"])
		assert.NotEqual(t, "", response["updated_at"])
		assert.Equal(t, response["created_at"], response["updated_at"])

		// Cleanup
		if destID, ok := response["id"].(string); ok {
			tenantStore.DeleteDestination(context.Background(), tenantID, destID)
		}
		tenantStore.DeleteTenant(context.Background(), tenantID)
	})
}
