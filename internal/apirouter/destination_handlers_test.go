package apirouter_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/apirouter"
	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/models"
	"github.com/hookdeck/outpost/internal/tenantstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// validDestination is a minimal valid create-destination payload.
func validDestination() map[string]any {
	return map[string]any{
		"type":   "webhook",
		"topics": []string{"user.created"},
		"config": map[string]string{"url": "https://example.com/hook"},
	}
}

func TestAPI_Destinations(t *testing.T) {
	t.Run("Create", func(t *testing.T) {
		t.Run("api key creates destination", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", validDestination())
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusCreated, resp.Code)

			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, "t1", dest.TenantID)
			assert.Equal(t, "webhook", dest.Type)
			assert.Equal(t, models.Topics{"user.created"}, dest.Topics)

			// Verify in store
			dests, err := h.tenantStore.ListDestination(t.Context(), tenantstore.ListDestinationRequest{TenantID: "t1"})
			require.NoError(t, err)
			assert.Len(t, dests, 1)
		})

		t.Run("jwt creates destination on own tenant", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", validDestination())
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusCreated, resp.Code)
		})

		t.Run("missing type returns 422", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", map[string]any{
				"topics": []string{"user.created"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
		})

		t.Run("missing topics returns 422", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", map[string]any{
				"type": "webhook",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
		})

		t.Run("invalid topic returns 422", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", map[string]any{
				"type":   "webhook",
				"topics": []string{"order.completed"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
		})

		t.Run("import timestamps", func(t *testing.T) {
			t.Run("disabled_at alone defaults created_at to disabled_at", func(t *testing.T) {
				h := newAPITest(t)
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

				disabledAt := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
				payload := validDestination()
				payload["disabled_at"] = disabledAt.Format(time.RFC3339)

				req := h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", payload)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusCreated, resp.Code)
				var dest destregistry.DestinationDisplay
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
				require.NotNil(t, dest.DisabledAt)
				assert.True(t, dest.DisabledAt.Equal(disabledAt))
				assert.True(t, dest.CreatedAt.Equal(disabledAt), "created_at defaults to disabled_at when only disabled_at is provided")
				assert.True(t, dest.UpdatedAt.Equal(disabledAt))
			})

			t.Run("disabled_at preserved on create with explicit created_at", func(t *testing.T) {
				h := newAPITest(t)
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

				createdAt := time.Now().Add(-48 * time.Hour).UTC().Truncate(time.Second)
				disabledAt := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
				payload := validDestination()
				payload["created_at"] = createdAt.Format(time.RFC3339)
				payload["disabled_at"] = disabledAt.Format(time.RFC3339)

				req := h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", payload)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusCreated, resp.Code)
				var dest destregistry.DestinationDisplay
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
				require.NotNil(t, dest.DisabledAt)
				assert.True(t, dest.DisabledAt.Equal(disabledAt), "disabled_at preserved: got %v want %v", dest.DisabledAt, disabledAt)
				assert.True(t, dest.CreatedAt.Equal(createdAt))
			})

			t.Run("created_at and updated_at preserved", func(t *testing.T) {
				h := newAPITest(t)
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

				createdAt := time.Now().Add(-30 * 24 * time.Hour).UTC().Truncate(time.Second)
				updatedAt := time.Now().Add(-1 * time.Hour).UTC().Truncate(time.Second)
				payload := validDestination()
				payload["created_at"] = createdAt.Format(time.RFC3339)
				payload["updated_at"] = updatedAt.Format(time.RFC3339)

				req := h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", payload)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusCreated, resp.Code)
				var dest destregistry.DestinationDisplay
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
				assert.True(t, dest.CreatedAt.Equal(createdAt))
				assert.True(t, dest.UpdatedAt.Equal(updatedAt))
			})

			t.Run("updated_at defaults to created_at when omitted", func(t *testing.T) {
				h := newAPITest(t)
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

				createdAt := time.Now().Add(-30 * 24 * time.Hour).UTC().Truncate(time.Second)
				payload := validDestination()
				payload["created_at"] = createdAt.Format(time.RFC3339)

				req := h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", payload)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusCreated, resp.Code)
				var dest destregistry.DestinationDisplay
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
				assert.True(t, dest.CreatedAt.Equal(createdAt))
				assert.True(t, dest.UpdatedAt.Equal(createdAt))
			})

			t.Run("created_at in future returns 422", func(t *testing.T) {
				h := newAPITest(t)
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

				payload := validDestination()
				payload["created_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)

				req := h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", payload)
				resp := h.do(h.withAPIKey(req))
				require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
			})

			t.Run("updated_at before created_at returns 422", func(t *testing.T) {
				h := newAPITest(t)
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

				createdAt := time.Now().Add(-1 * time.Hour).UTC()
				updatedAt := time.Now().Add(-2 * time.Hour).UTC()
				payload := validDestination()
				payload["created_at"] = createdAt.Format(time.RFC3339)
				payload["updated_at"] = updatedAt.Format(time.RFC3339)

				req := h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", payload)
				resp := h.do(h.withAPIKey(req))
				require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
			})

			t.Run("disabled_at in future returns 422", func(t *testing.T) {
				h := newAPITest(t)
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

				payload := validDestination()
				payload["disabled_at"] = time.Now().Add(time.Hour).UTC().Format(time.RFC3339)

				req := h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", payload)
				resp := h.do(h.withAPIKey(req))
				require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
			})

			t.Run("disabled_at before created_at returns 422", func(t *testing.T) {
				h := newAPITest(t)
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

				createdAt := time.Now().Add(-1 * time.Hour).UTC()
				disabledAt := time.Now().Add(-2 * time.Hour).UTC()
				payload := validDestination()
				payload["created_at"] = createdAt.Format(time.RFC3339)
				payload["disabled_at"] = disabledAt.Format(time.RFC3339)

				req := h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", payload)
				resp := h.do(h.withAPIKey(req))
				require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
			})
		})
	})

	t.Run("Retrieve", func(t *testing.T) {
		t.Run("api key returns destination", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations/d1", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, "d1", dest.ID)
		})

		t.Run("nonexistent destination returns 404", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations/nope", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})

		t.Run("jwt returns destination on own tenant", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations/d1", nil)
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusOK, resp.Code)
		})

		t.Run("destination belonging to other tenant returns 404", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t2")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations/d1", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})
	})

	t.Run("List", func(t *testing.T) {
		t.Run("api key returns all destinations for tenant", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d2"), df.WithTenantID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var dests []destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dests))
			assert.Len(t, dests, 2)
		})

		t.Run("jwt returns destinations on own tenant", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations", nil)
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusOK, resp.Code)

			var dests []destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dests))
			assert.Len(t, dests, 1)
		})

		t.Run("Filtering", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithType("webhook"), df.WithTopics([]string{"user.created"}),
			))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d2"), df.WithTenantID("t1"),
				df.WithType("aws_sqs"), df.WithTopics([]string{"user.deleted"}),
			))

			t.Run("type filter", func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations?type=webhook", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var dests []destregistry.DestinationDisplay
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dests))
				require.Len(t, dests, 1)
				assert.Equal(t, "d1", dests[0].ID)
			})

			t.Run("topics filter", func(t *testing.T) {
				req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations?topics=user.created", nil)
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)

				var dests []destregistry.DestinationDisplay
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dests))
				require.Len(t, dests, 1)
				assert.Equal(t, "d1", dests[0].ID)
			})
		})
	})

	t.Run("Update", func(t *testing.T) {
		t.Run("api key updates destination topics", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"), df.WithTopics([]string{"user.created"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"topics": []string{"user.deleted"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, models.Topics{"user.deleted"}, dest.Topics)
		})

		t.Run("api key updates destination config", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithConfig(map[string]string{"url": "https://old.example.com"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"config": map[string]string{"url": "https://new.example.com"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, "https://new.example.com", dest.Config["url"])
		})

		t.Run("jwt updates destination on own tenant", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"), df.WithTopics([]string{"user.created"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"topics": []string{"user.deleted"},
			})
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusOK, resp.Code)
		})

		t.Run("nonexistent destination returns 404", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/nope", map[string]any{
				"topics": []string{"user.deleted"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})

		t.Run("destination belonging to other tenant returns 404", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t2"), df.WithTopics([]string{"user.created"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"topics": []string{"user.deleted"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})

		t.Run("changing type returns 422", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"), df.WithTopics([]string{"user.created"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"type": "aws_sqs",
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
		})

		t.Run("sending same type is allowed", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"), df.WithTopics([]string{"user.created"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"type":   "webhook",
				"topics": []string{"user.deleted"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
		})

		// ── Merge-patch semantics (RFC 7396) for map fields ──

		t.Run("metadata merge adds key preserving existing", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithMetadata(map[string]string{"env": "prod"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"metadata": map[string]any{"team": "platform"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, models.Metadata{"env": "prod", "team": "platform"}, dest.Metadata)
		})

		t.Run("metadata merge updates existing key", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithMetadata(map[string]string{"env": "prod"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"metadata": map[string]any{"env": "staging"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, models.Metadata{"env": "staging"}, dest.Metadata)
		})

		t.Run("metadata delete key via null", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithMetadata(map[string]string{"env": "prod", "region": "us-east-1"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"metadata": map[string]any{"region": nil},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, models.Metadata{"env": "prod"}, dest.Metadata)
		})

		t.Run("metadata clear entire field via null", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithMetadata(map[string]string{"env": "prod"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"metadata": nil,
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.True(t, dest.Metadata == nil || len(dest.Metadata) == 0)
		})

		t.Run("metadata empty object is no-op", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithMetadata(map[string]string{"env": "prod"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"metadata": map[string]any{},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, models.Metadata{"env": "prod"}, dest.Metadata)
		})

		t.Run("metadata unchanged when omitted", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithMetadata(map[string]string{"env": "prod"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"topics": []string{"user.created"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, models.Metadata{"env": "prod"}, dest.Metadata)
		})

		t.Run("metadata mixed add update delete", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithMetadata(map[string]string{"keep": "v", "remove": "v", "update": "old"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"metadata": map[string]any{"remove": nil, "update": "new", "add": "v"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, models.Metadata{"keep": "v", "update": "new", "add": "v"}, dest.Metadata)
		})

		// ── delivery_metadata merge-patch ──

		t.Run("delivery_metadata merge adds key preserving existing", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithDeliveryMetadata(map[string]string{"source": "outpost"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"delivery_metadata": map[string]any{"version": "1.0"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, models.DeliveryMetadata{"source": "outpost", "version": "1.0"}, dest.DeliveryMetadata)
		})

		t.Run("delivery_metadata delete key via null", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithDeliveryMetadata(map[string]string{"source": "outpost", "version": "1.0"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"delivery_metadata": map[string]any{"version": nil},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, models.DeliveryMetadata{"source": "outpost"}, dest.DeliveryMetadata)
		})

		t.Run("delivery_metadata clear via null", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithDeliveryMetadata(map[string]string{"source": "outpost"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"delivery_metadata": nil,
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.True(t, dest.DeliveryMetadata == nil || len(dest.DeliveryMetadata) == 0)
		})

		t.Run("delivery_metadata empty object is no-op", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithDeliveryMetadata(map[string]string{"source": "outpost"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"delivery_metadata": map[string]any{},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, models.DeliveryMetadata{"source": "outpost"}, dest.DeliveryMetadata)
		})

		t.Run("delivery_metadata unchanged when omitted", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithDeliveryMetadata(map[string]string{"source": "outpost"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"topics": []string{"user.created"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, models.DeliveryMetadata{"source": "outpost"}, dest.DeliveryMetadata)
		})

		// ── config merge-patch ──

		t.Run("config merge adds key preserving existing", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithConfig(map[string]string{"url": "https://example.com/hook"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"config": map[string]any{"custom_header": "X-Custom"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, "https://example.com/hook", dest.Config["url"])
			assert.Equal(t, "X-Custom", dest.Config["custom_header"])
		})

		t.Run("config delete key via null", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithConfig(map[string]string{"url": "https://example.com/hook", "extra": "val"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"config": map[string]any{"extra": nil},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, "https://example.com/hook", dest.Config["url"])
			_, hasExtra := dest.Config["extra"]
			assert.False(t, hasExtra, "extra key should be removed")
		})

		t.Run("config clear via null", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithConfig(map[string]string{"url": "https://example.com/hook"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"config": nil,
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.True(t, dest.Config == nil || len(dest.Config) == 0)
		})

		t.Run("config empty object is no-op", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithConfig(map[string]string{"url": "https://example.com/hook"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"config": map[string]any{},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, "https://example.com/hook", dest.Config["url"])
		})

		t.Run("config unchanged when omitted", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithConfig(map[string]string{"url": "https://example.com/hook"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"topics": []string{"user.created"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, "https://example.com/hook", dest.Config["url"])
		})

		// ── credentials merge-patch ──

		t.Run("credentials merge adds key preserving existing", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithCredentials(map[string]string{"secret": "s1"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"credentials": map[string]any{"previous_secret": "s0"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, "s1", dest.Credentials["secret"])
			assert.Equal(t, "s0", dest.Credentials["previous_secret"])
		})

		t.Run("credentials delete key via null", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithCredentials(map[string]string{"secret": "s1", "previous_secret": "s0"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"credentials": map[string]any{"previous_secret": nil},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, "s1", dest.Credentials["secret"])
			_, hasPrev := dest.Credentials["previous_secret"]
			assert.False(t, hasPrev, "previous_secret should be removed")
		})

		t.Run("credentials clear via null", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithCredentials(map[string]string{"secret": "s1"}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"credentials": nil,
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.True(t, dest.Credentials == nil || len(dest.Credentials) == 0)
		})

		// ── filter replacement semantics ──

		t.Run("filter replaced not merged", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithFilter(models.Filter{"body": map[string]any{"user_id": "usr_123"}}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"filter": map[string]any{"body": map[string]any{"status": "active"}},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Nil(t, dest.Filter["body"].(map[string]any)["user_id"], "old key should not be present")
			assert.Equal(t, "active", dest.Filter["body"].(map[string]any)["status"])
		})

		t.Run("filter cleared with empty object", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithFilter(models.Filter{"body": map[string]any{"user_id": "usr_123"}}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"filter": map[string]any{},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.True(t, dest.Filter == nil || len(dest.Filter) == 0, "filter should be cleared")
		})

		t.Run("filter cleared with null", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithFilter(models.Filter{"body": map[string]any{"user_id": "usr_123"}}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"filter": nil,
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.True(t, dest.Filter == nil || len(dest.Filter) == 0, "filter should be cleared")
		})

		t.Run("filter unchanged when omitted", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(
				df.WithID("d1"), df.WithTenantID("t1"),
				df.WithFilter(models.Filter{"body": map[string]any{"user_id": "usr_123"}}),
			))

			req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"topics": []string{"user.created"},
			})
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Equal(t, "usr_123", dest.Filter["body"].(map[string]any)["user_id"])
		})

		t.Run("disabled_at", func(t *testing.T) {
			t.Run("omitted leaves field unchanged", func(t *testing.T) {
				h := newAPITest(t)
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
				disabledAt := time.Now().Add(-1 * time.Hour).UTC().Truncate(time.Second)
				h.tenantStore.CreateDestination(t.Context(), df.Any(
					df.WithID("d1"), df.WithTenantID("t1"), df.WithDisabledAt(disabledAt),
				))

				req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
					"topics": []string{"user.created"},
				})
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)
				var dest destregistry.DestinationDisplay
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
				require.NotNil(t, dest.DisabledAt)
				assert.True(t, dest.DisabledAt.Equal(disabledAt))
			})

			t.Run("null enables a disabled destination", func(t *testing.T) {
				h := newAPITest(t)
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
				h.tenantStore.CreateDestination(t.Context(), df.Any(
					df.WithID("d1"), df.WithTenantID("t1"),
					df.WithDisabledAt(time.Now().Add(-1*time.Hour)),
				))

				req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", json.RawMessage(`{"disabled_at":null}`))
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)
				var dest destregistry.DestinationDisplay
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
				assert.Nil(t, dest.DisabledAt)
			})

			t.Run("timestamp disables an enabled destination", func(t *testing.T) {
				h := newAPITest(t)
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
				createdAt := time.Now().Add(-2 * time.Hour).UTC().Truncate(time.Second)
				h.tenantStore.CreateDestination(t.Context(), df.Any(
					df.WithID("d1"), df.WithTenantID("t1"), df.WithCreatedAt(createdAt),
				))

				ts := time.Now().Add(-30 * time.Minute).UTC().Truncate(time.Second)
				req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
					"disabled_at": ts.Format(time.RFC3339),
				})
				resp := h.do(h.withAPIKey(req))

				require.Equal(t, http.StatusOK, resp.Code)
				var dest destregistry.DestinationDisplay
				require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
				require.NotNil(t, dest.DisabledAt)
				assert.True(t, dest.DisabledAt.Equal(ts))
			})

			t.Run("future timestamp returns 422", func(t *testing.T) {
				h := newAPITest(t)
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
				h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

				req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
					"disabled_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
				})
				resp := h.do(h.withAPIKey(req))
				require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
			})

			t.Run("timestamp before created_at returns 422", func(t *testing.T) {
				h := newAPITest(t)
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
				createdAt := time.Now().Add(-1 * time.Hour).UTC()
				h.tenantStore.CreateDestination(t.Context(), df.Any(
					df.WithID("d1"), df.WithTenantID("t1"), df.WithCreatedAt(createdAt),
				))

				req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
					"disabled_at": time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339),
				})
				resp := h.do(h.withAPIKey(req))
				require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
			})

			t.Run("malformed timestamp returns 422", func(t *testing.T) {
				h := newAPITest(t)
				h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
				h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

				req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
					"disabled_at": "not-a-timestamp",
				})
				resp := h.do(h.withAPIKey(req))
				require.Equal(t, http.StatusUnprocessableEntity, resp.Code)
			})
		})
	})

	t.Run("Delete", func(t *testing.T) {
		t.Run("api key deletes destination", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/t1/destinations/d1", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var body map[string]any
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
			assert.Equal(t, true, body["success"])

			// Subsequent GET returns 404
			req = httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations/d1", nil)
			resp = h.do(h.withAPIKey(req))
			assert.Equal(t, http.StatusNotFound, resp.Code)
		})

		t.Run("deleted destination returns 404", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))
			h.tenantStore.DeleteDestination(t.Context(), "t1", "d1")

			req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations/d1", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})

		t.Run("jwt deletes destination on own tenant", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/t1/destinations/d1", nil)
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusOK, resp.Code)
		})

		t.Run("destination belonging to other tenant returns 404", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t2")))

			req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/t1/destinations/d1", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})
	})

	t.Run("Enable/Disable", func(t *testing.T) {
		t.Run("api key disables destination", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

			req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/t1/destinations/d1/disable", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.NotNil(t, dest.DisabledAt)
		})

		t.Run("api key enables disabled destination", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

			// Disable first
			req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/t1/destinations/d1/disable", nil)
			h.do(h.withAPIKey(req))

			// Enable
			req = httptest.NewRequest(http.MethodPut, "/api/v1/tenants/t1/destinations/d1/enable", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Nil(t, dest.DisabledAt)
		})

		t.Run("enable already enabled is noop", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

			req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/t1/destinations/d1/enable", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)

			var dest destregistry.DestinationDisplay
			require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &dest))
			assert.Nil(t, dest.DisabledAt)
		})

		t.Run("jwt disable on own tenant", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

			req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/t1/destinations/d1/disable", nil)
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusOK, resp.Code)
		})

		t.Run("enable destination belonging to other tenant returns 404", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t2")))

			req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/t1/destinations/d1/enable", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})

		t.Run("disable destination belonging to other tenant returns 404", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2")))
			h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t2")))

			req := httptest.NewRequest(http.MethodPut, "/api/v1/tenants/t1/destinations/d1/disable", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusNotFound, resp.Code)
		})
	})

	t.Run("jwt other tenant returns 403", func(t *testing.T) {
		h := newAPITest(t)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t2")))
		h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t2")))

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t2/destinations", nil)
		resp := h.do(h.withJWT(req, "t1"))

		require.Equal(t, http.StatusForbidden, resp.Code)
	})

	t.Run("no auth returns 401", func(t *testing.T) {
		h := newAPITest(t)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

		req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/destinations", nil)
		resp := h.do(req)

		require.Equal(t, http.StatusUnauthorized, resp.Code)
	})
}

func TestAPI_SubscriptionUpdated(t *testing.T) {
	t.Run("create destination emits subscription update", func(t *testing.T) {
		h := newAPITest(t)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

		req := h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", validDestination())
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusCreated, resp.Code)
		require.Len(t, h.subscriptionEmitter.calls, 1)

		call := h.subscriptionEmitter.calls[0]
		assert.Equal(t, "tenant.subscription.updated", call.topic)
		assert.Equal(t, "t1", call.tenantID)

		data := call.data.(apirouter.TenantSubscriptionUpdatedData)
		assert.Equal(t, 1, data.DestinationsCount)
		assert.Equal(t, 0, data.PreviousDestinationsCount)
	})

	t.Run("delete destination emits subscription update", func(t *testing.T) {
		h := newAPITest(t)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
		h.tenantStore.CreateDestination(t.Context(), df.Any(df.WithID("d1"), df.WithTenantID("t1")))

		req := httptest.NewRequest(http.MethodDelete, "/api/v1/tenants/t1/destinations/d1", nil)
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)
		require.Len(t, h.subscriptionEmitter.calls, 1)

		data := h.subscriptionEmitter.calls[0].data.(apirouter.TenantSubscriptionUpdatedData)
		assert.Equal(t, 0, data.DestinationsCount)
		assert.Equal(t, 1, data.PreviousDestinationsCount)
	})

	t.Run("update destination topics emits subscription update", func(t *testing.T) {
		h := newAPITest(t)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
		h.tenantStore.CreateDestination(t.Context(), df.Any(
			df.WithID("d1"), df.WithTenantID("t1"), df.WithTopics([]string{"user.created"}),
		))

		req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
			"topics": []string{"user.deleted"},
		})
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)
		require.Len(t, h.subscriptionEmitter.calls, 1)

		data := h.subscriptionEmitter.calls[0].data.(apirouter.TenantSubscriptionUpdatedData)
		assert.Contains(t, data.Topics, "user.deleted")
		assert.Contains(t, data.PreviousTopics, "user.created")
	})

	t.Run("update destination topics without changing tenant topics does not emit", func(t *testing.T) {
		h := newAPITest(t)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
		// Destination A subscribes to *, so tenant topics are always *
		h.tenantStore.CreateDestination(t.Context(), df.Any(
			df.WithID("d1"), df.WithTenantID("t1"), df.WithTopics([]string{"*"}),
		))
		h.tenantStore.CreateDestination(t.Context(), df.Any(
			df.WithID("d2"), df.WithTenantID("t1"), df.WithTopics([]string{"user.created"}),
		))

		// Update d2's topics — tenant topics still * because d1
		req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d2", map[string]any{
			"topics": []string{"user.deleted"},
		})
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)
		assert.Empty(t, h.subscriptionEmitter.calls, "No emit when tenant-level topics unchanged")
	})

	t.Run("update destination config without topic change does not emit", func(t *testing.T) {
		h := newAPITest(t)
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
		h.tenantStore.CreateDestination(t.Context(), df.Any(
			df.WithID("d1"), df.WithTenantID("t1"),
			df.WithConfig(map[string]string{"url": "https://old.example.com"}),
		))

		req := h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
			"config": map[string]string{"url": "https://new.example.com"},
		})
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusOK, resp.Code)
		assert.Empty(t, h.subscriptionEmitter.calls, "No emit when topics/count unchanged")
	})

	t.Run("nil emitter does not panic", func(t *testing.T) {
		h := newAPITest(t, withSubscriptionEmitter(nil))
		h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

		req := h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", validDestination())
		resp := h.do(h.withAPIKey(req))

		require.Equal(t, http.StatusCreated, resp.Code)
	})
}

// TestAPI_DestinationTypes tests the /destination-types endpoints.
// Note: response body is a passthrough from the registry stub (returns nil);
// not validated here. 404 path not testable without enhancing the stub.
func TestAPI_DestinationTypes(t *testing.T) {
	t.Run("List", func(t *testing.T) {
		t.Run("api key returns 200", func(t *testing.T) {
			h := newAPITest(t)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/destination-types", nil)
			resp := h.do(h.withAPIKey(req))

			require.Equal(t, http.StatusOK, resp.Code)
		})

		t.Run("jwt returns 200", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/destination-types", nil)
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusOK, resp.Code)
		})

		t.Run("no auth returns 401", func(t *testing.T) {
			h := newAPITest(t)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/destination-types", nil)
			resp := h.do(req)

			require.Equal(t, http.StatusUnauthorized, resp.Code)
		})
	})

	t.Run("Retrieve", func(t *testing.T) {
		t.Run("api key returns 200", func(t *testing.T) {
			h := newAPITest(t)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/destination-types/webhook", nil)
			resp := h.do(h.withAPIKey(req))

			// The stub returns (nil, nil) for RetrieveProviderMetadata,
			// so the handler returns 200 with null body.
			require.Equal(t, http.StatusOK, resp.Code)
		})

		t.Run("jwt returns 200", func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/destination-types/webhook", nil)
			resp := h.do(h.withJWT(req, "t1"))

			require.Equal(t, http.StatusOK, resp.Code)
		})

		t.Run("no auth returns 401", func(t *testing.T) {
			h := newAPITest(t)

			req := httptest.NewRequest(http.MethodGet, "/api/v1/destination-types/webhook", nil)
			resp := h.do(req)

			require.Equal(t, http.StatusUnauthorized, resp.Code)
		})
	})
}
