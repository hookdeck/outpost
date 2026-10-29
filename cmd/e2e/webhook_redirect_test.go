package e2e_test

import (
	"fmt"
	"net/http"
	"time"
)

func (s *basicSuite) TestWebhook_RedirectFailsAttemptAndRetries() {
	tenant := s.createTenant()
	target := s.createWebhookDestination(tenant.ID, "user.deleted")
	source := s.createWebhookDestination(tenant.ID, "user.created", withSecret(testSecret))
	location := fmt.Sprintf("%s/webhook/%s", s.mockServerURL(), target.mockID)

	s.publish(tenant.ID, "user.created", map[string]any{
		"test": "redirect",
	},
		withRetry(),
		withPublishMetadata(map[string]string{
			"respond_status":   "302",
			"respond_location": location,
		}),
	)

	s.waitForNewMockServerEvents(source.mockID, 2)
	s.waitForNewAttempts(tenant.ID, 2)

	var resp struct {
		Models []map[string]any `json:"models"`
	}
	status := s.doJSON(http.MethodGet, s.apiURL("/attempts?tenant_id="+tenant.ID+"&dir=asc&include=response_data"), nil, &resp)
	s.Require().Equal(http.StatusOK, status)
	s.Require().GreaterOrEqual(len(resp.Models), 2)

	for i, atm := range resp.Models {
		s.Equal("failed", atm["status"])
		s.Equal("302", atm["code"])
		s.Equal(float64(i+1), atm["attempt_number"])
		responseData, _ := atm["response_data"].(map[string]any)
		s.Equal(location, responseData["location"])
	}

	s.assertNoDelivery(target.mockID, 500*time.Millisecond)
}
