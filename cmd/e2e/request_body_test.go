package e2e_test

import (
	"net/http"
	"strings"

	"github.com/hookdeck/outpost/internal/idgen"
)

func (s *basicSuite) TestRequestBody_LargeBodiesAreAccepted() {
	for _, size := range []int{11 * 1024, 300 * 1024} {
		value := strings.Repeat("a", size)
		tenantID := idgen.String()

		var tenant struct {
			Metadata map[string]string `json:"metadata"`
		}
		status := s.doJSON(http.MethodPut, s.apiURL("/tenants/"+tenantID), map[string]any{
			"metadata": map[string]string{"blob": value},
		}, &tenant)
		s.Require().Equal(http.StatusCreated, status, "tenant upsert with a %d byte value", size)
		s.Equal(value, tenant.Metadata["blob"])

		var dest struct {
			ID       string            `json:"id"`
			Metadata map[string]string `json:"metadata"`
		}
		status = s.doJSON(http.MethodPost, s.apiURL("/tenants/"+tenantID+"/destinations"), map[string]any{
			"type":     "webhook",
			"topics":   []string{"*"},
			"config":   map[string]string{"url": "https://example.com/webhook"},
			"metadata": map[string]string{"blob": value},
		}, &dest)
		s.Require().Equal(http.StatusCreated, status, "destination create with a %d byte value", size)
		s.Equal(value, dest.Metadata["blob"])

		updated := value[1:] + "b"
		status = s.doJSON(http.MethodPatch, s.apiURL("/tenants/"+tenantID+"/destinations/"+dest.ID), map[string]any{
			"metadata": map[string]string{"blob": updated},
		}, &dest)
		s.Require().Equal(http.StatusOK, status, "destination update with a %d byte value", size)
		s.Equal(updated, dest.Metadata["blob"])
	}
}

func (s *basicSuite) TestRequestBody_OverLimitIsRejected() {
	tenant := s.createTenant()
	dest := s.createWebhookDestination(tenant.ID, "*")
	admin := s.adminAuth()
	metadata := map[string]string{"blob": strings.Repeat("a", 1<<20)}

	tooLarge := func(name, method, path, auth string, body any) errorCase {
		return errorCase{
			name: name, method: method, path: path, auth: auth, body: body,
			status: http.StatusRequestEntityTooLarge, message: "request body too large",
		}
	}

	s.runErrorCases([]errorCase{
		tooLarge("tenant upsert", http.MethodPut, "/tenants/"+tenant.ID, admin, map[string]any{"metadata": metadata}),
		tooLarge("destination create", http.MethodPost, "/tenants/"+tenant.ID+"/destinations", admin, map[string]any{
			"type":     "webhook",
			"topics":   []string{"*"},
			"config":   map[string]string{"url": "https://example.com/webhook"},
			"metadata": metadata,
		}),
		tooLarge("destination update", http.MethodPatch, "/tenants/"+tenant.ID+"/destinations/"+dest.ID, admin, map[string]any{"metadata": metadata}),
		tooLarge("retry", http.MethodPost, "/retry", admin, map[string]any{"event_id": "evt_missing", "destination_id": dest.ID, "metadata": metadata}),
		tooLarge("tenant JWT", http.MethodPatch, "/tenants/"+tenant.ID+"/destinations/"+dest.ID, s.tenantAuth(tenant.ID), map[string]any{"metadata": metadata}),
		tooLarge("no auth header", http.MethodPut, "/tenants/"+tenant.ID, "", map[string]any{"metadata": metadata}),
	})
}
