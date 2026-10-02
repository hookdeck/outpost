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
