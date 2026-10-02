package apirouter_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/apirouter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Request bodies larger than the logger's buffer must reach the handlers whole.
func TestAPI_RequestBodyOverLogBuffer(t *testing.T) {
	sizes := []int{
		apirouter.MaxRequestBodySize - 512,
		apirouter.MaxRequestBodySize + 1,
		apirouter.MaxRequestBodySize + 512,
		300 * 1024,
	}

	for _, size := range sizes {
		value := strings.Repeat("a", size)

		t.Run(fmt.Sprintf("tenant upsert %d bytes", size), func(t *testing.T) {
			h := newAPITest(t)

			req := h.jsonReq(http.MethodPut, "/api/v1/tenants/t1", map[string]any{
				"metadata": map[string]string{"blob": value},
			})
			resp := h.do(h.withAPIKey(req))
			require.Equal(t, http.StatusCreated, resp.Code, resp.Body.String())

			tenant, err := h.tenantStore.RetrieveTenant(t.Context(), "t1")
			require.NoError(t, err)
			assert.Equal(t, value, tenant.Metadata["blob"])
		})

		t.Run(fmt.Sprintf("destination create and update %d bytes", size), func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))

			body := validDestination()
			body["id"] = "d1"
			body["credentials"] = map[string]string{"certificate": value}
			resp := h.do(h.withAPIKey(h.jsonReq(http.MethodPost, "/api/v1/tenants/t1/destinations", body)))
			require.Equal(t, http.StatusCreated, resp.Code, resp.Body.String())

			dest, err := h.tenantStore.RetrieveDestination(t.Context(), "t1", "d1")
			require.NoError(t, err)
			assert.Equal(t, value, dest.Credentials["certificate"])

			updated := value[1:] + "b"
			resp = h.do(h.withAPIKey(h.jsonReq(http.MethodPatch, "/api/v1/tenants/t1/destinations/d1", map[string]any{
				"credentials": map[string]string{"certificate": updated},
			})))
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

			dest, err = h.tenantStore.RetrieveDestination(t.Context(), "t1", "d1")
			require.NoError(t, err)
			assert.Equal(t, updated, dest.Credentials["certificate"])
		})

		t.Run(fmt.Sprintf("destination create over HTTP with a chunked body %d bytes", size), func(t *testing.T) {
			h := newAPITest(t)
			h.tenantStore.UpsertTenant(t.Context(), tf.Any(tf.WithID("t1")))
			server := httptest.NewServer(h.router)
			defer server.Close()

			body := validDestination()
			body["id"] = "d1"
			body["credentials"] = map[string]string{"certificate": value}
			bodyBytes, err := json.Marshal(body)
			require.NoError(t, err)

			// A reader of unknown length makes the client send a chunked body.
			req, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/tenants/t1/destinations", io.MultiReader(strings.NewReader(string(bodyBytes))))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(h.withAPIKey(req))
			require.NoError(t, err)
			defer resp.Body.Close()
			respBody, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			require.Equal(t, http.StatusCreated, resp.StatusCode, string(respBody))

			dest, err := h.tenantStore.RetrieveDestination(t.Context(), "t1", "d1")
			require.NoError(t, err)
			assert.Equal(t, value, dest.Credentials["certificate"])
		})
	}
}
