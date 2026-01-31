package e2e_test

import "fmt"

func (s *basicSuite) TestAlerts_ConsecutiveFailuresTriggerAlertCallback() {
	tenant := s.createTenant()
	dest := s.createWebhookDestination(tenant.ID, "*", withSecret(testSecret))

	// Publish 20 failing events
	for i := 0; i < 20; i++ {
		s.publish(tenant.ID, "user.created", map[string]any{
			"index": i,
		}, withPublishMetadata(map[string]string{"should_err": "true"}))
	}

	// Wait for destination to be disabled (sync point for all 20 deliveries)
	s.waitForNewDestinationDisabled(tenant.ID, dest.ID)

	// Verify destination is disabled
	got := s.getDestination(tenant.ID, dest.ID)
	s.NotNil(got.DisabledAt, "destination should be disabled")

	// Wait for 4 alert callbacks to be processed
	s.waitForAlerts(dest.ID, 4)
	alerts := s.alertServer.GetAlertsForDestination(dest.ID)
	s.Require().Len(alerts, 4, "should have 4 alerts")

	expectedCounts := []int{10, 14, 18, 20}
	for i, alert := range alerts {
		s.Equal(fmt.Sprintf("Bearer %s", s.config.APIKey), alert.AuthHeader, "auth header should match")
		s.Equal(expectedCounts[i], alert.Alert.Data.ConsecutiveFailures,
			"alert %d should have %d consecutive failures", i, expectedCounts[i])
	}
}

func (s *basicSuite) TestAlerts_SuccessResetsConsecutiveFailureCounter() {
	tenant := s.createTenant()
	dest := s.createWebhookDestination(tenant.ID, "*", withSecret(testSecret))

	// First batch: 14 failures
	for i := 0; i < 14; i++ {
		s.publish(tenant.ID, "user.created", map[string]any{
			"index": i,
		}, withPublishMetadata(map[string]string{"should_err": "true"}))
	}

	// Wait for first batch to be fully delivered
	s.waitForNewMockServerEvents(dest.mockID, 14)

	// One successful delivery (resets counter)
	s.publish(tenant.ID, "user.created", map[string]any{
		"success": true,
	}, withPublishMetadata(map[string]string{"should_err": "false"}))

	// Wait for success event to be delivered
	s.waitForNewMockServerEvents(dest.mockID, 15)

	// Second batch: 14 more failures
	for i := 0; i < 14; i++ {
		s.publish(tenant.ID, "user.created", map[string]any{
			"index": i,
		}, withPublishMetadata(map[string]string{"should_err": "true"}))
	}

	// Wait for all 29 deliveries
	s.waitForNewMockServerEvents(dest.mockID, 29)

	// Destination should NOT be disabled (only 14 consecutive, threshold is 20)
	got := s.getDestination(tenant.ID, dest.ID)
	s.Nil(got.DisabledAt, "destination should NOT be disabled (counter reset after success)")

	// Wait for 4 alert callbacks: [10, 14] from first batch, [10, 14] from second batch
	s.waitForAlerts(dest.ID, 4)
	alerts := s.alertServer.GetAlertsForDestination(dest.ID)
	s.Require().Len(alerts, 4, "should have 4 alerts")

	expectedCounts := []int{10, 14, 10, 14}
	for i, alert := range alerts {
		s.Equal(fmt.Sprintf("Bearer %s", s.config.APIKey), alert.AuthHeader, "auth header should match")
		s.Equal(expectedCounts[i], alert.Alert.Data.ConsecutiveFailures,
			"alert %d should have %d consecutive failures", i, expectedCounts[i])
	}
}

