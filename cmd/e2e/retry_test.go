package e2e_test

import (
	"net/http"

	"github.com/hookdeck/outpost/internal/idgen"
)

func (s *basicSuite) TestRetry_FailedDeliveryAutoRetries() {
	tenant := s.createTenant()
	secret := testSecret
	dest := s.createWebhookDestination(tenant.ID, "*", withSecret(secret))

	s.publish(tenant.ID, "user.created", map[string]any{
		"test": "auto_retry",
	},
		withRetry(),
		withPublishMetadata(map[string]string{"should_err": "true"}),
	)

	// Wait for at least 2 delivery attempts (initial + retry)
	s.waitForNewMockServerEvents(dest.mockID, 2)

	// Wait for attempts to be logged
	attempts := s.waitForNewAttempts(tenant.ID, 2)
	s.Require().GreaterOrEqual(len(attempts), 2, "should have at least 2 attempts from automated retry")

	// Fetch in asc order and verify attempt_number increments
	var resp struct {
		Models []map[string]any `json:"models"`
	}
	status := s.doJSON(http.MethodGet, s.apiURL("/attempts?tenant_id="+tenant.ID+"&dir=asc"), nil, &resp)
	s.Require().Equal(http.StatusOK, status)
	s.Require().GreaterOrEqual(len(resp.Models), 2)

	for i, atm := range resp.Models {
		s.Equal(float64(i), atm["attempt_number"],
			"attempt %d should have attempt_number=%d (automated retry increments)", i, i)
	}
}

func (s *basicSuite) TestRetry_ManualRetryCreatesNewAttempt() {
	tenant := s.createTenant()
	dest := s.createWebhookDestination(tenant.ID, "*", withSecret(testSecret), withResponseStatus(500))

	eventID := idgen.Event()
	s.publish(tenant.ID, "user.created", map[string]any{
		"user_id": "456",
	}, withEventID(eventID))

	// Wait for initial attempt to fail
	s.waitForNewAttempts(tenant.ID, 1)

	// Verify first attempt has attempt_number=0
	var attResp struct {
		Models []map[string]any `json:"models"`
	}
	status := s.doJSON(http.MethodGet, s.apiURL("/attempts?tenant_id="+tenant.ID+"&event_id="+eventID), nil, &attResp)
	s.Require().Equal(http.StatusOK, status)
	s.Require().NotEmpty(attResp.Models)
	s.Equal(float64(0), attResp.Models[0]["attempt_number"])

	// Reconfigure mock to succeed
	dest.SetResponse(s, 200)

	// Manual retry
	retryStatus := s.retryEvent(eventID, dest.ID)
	s.Equal(http.StatusAccepted, retryStatus)

	// Wait for retry attempt
	s.waitForNewAttempts(tenant.ID, 2)

	// Verify: 2 attempts, one manual=true
	var verifyResp struct {
		Models []map[string]any `json:"models"`
	}
	status = s.doJSON(http.MethodGet, s.apiURL("/attempts?tenant_id="+tenant.ID+"&event_id="+eventID+"&dir=asc"), nil, &verifyResp)
	s.Require().Equal(http.StatusOK, status)
	s.Require().Len(verifyResp.Models, 2)

	// Both should have attempt_number=0 (manual retry resets)
	for _, atm := range verifyResp.Models {
		s.Equal(float64(0), atm["attempt_number"])
	}

	// Verify one manual=true
	manualCount := 0
	for _, atm := range verifyResp.Models {
		if manual, ok := atm["manual"].(bool); ok && manual {
			manualCount++
		}
	}
	s.Equal(1, manualCount, "should have exactly one manual retry attempt")
}

func (s *basicSuite) TestRetry_ManualRetryNonExistentEvent() {
	status := s.retryEvent(idgen.Event(), idgen.Destination())
	s.Equal(http.StatusNotFound, status)
}

func (s *basicSuite) TestRetry_ManualRetryOnDisabledDestinationRejected() {
	tenant := s.createTenant()
	dest := s.createWebhookDestination(tenant.ID, "*")

	eventID := idgen.Event()
	s.publish(tenant.ID, "user.created", map[string]any{
		"test": "disabled_retry",
	}, withEventID(eventID))

	// Wait for delivery
	s.waitForNewAttempts(tenant.ID, 1)

	// Disable destination
	s.disableDestination(tenant.ID, dest.ID)

	// Retry should be rejected
	status := s.retryEvent(eventID, dest.ID)
	s.Equal(http.StatusBadRequest, status)
}
