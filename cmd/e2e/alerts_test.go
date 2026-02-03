package e2e_test

import "fmt"

func (s *basicSuite) TestAlerts_ConsecutiveFailuresTriggerAlertCallback() {
	tenant := s.createTenant()
	dest := s.createWebhookDestination(tenant.ID, "*", withSecret(testSecret))

	// Publish 20 failing events
	for i := range 20 {
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
		// Auth header assertion
		s.Equal(fmt.Sprintf("Bearer %s", s.config.APIKey), alert.AuthHeader, "auth header should match")

		// Topic assertion
		s.Equal("alert.consecutive_failure", alert.Alert.Topic, "alert topic should be alert.consecutive_failure")

		// TenantID assertion
		s.NotEmpty(alert.Alert.Data.TenantID, "alert should have tenant_id")
		s.Equal(tenant.ID, alert.Alert.Data.TenantID, "alert tenant_id should match")

		// Destination assertions
		s.Require().NotNil(alert.Alert.Data.Destination, "alert should have destination")
		s.Equal(dest.ID, alert.Alert.Data.Destination.ID, "alert destination ID should match")
		s.Equal(tenant.ID, alert.Alert.Data.Destination.TenantID, "alert destination tenant_id should match")
		s.Equal("webhook", alert.Alert.Data.Destination.Type, "alert destination type should be webhook")

		// Event assertions
		s.NotEmpty(alert.Alert.Data.Event.ID, "alert event should have ID")
		s.Equal("user.created", alert.Alert.Data.Event.Topic, "alert event topic should match")
		s.NotNil(alert.Alert.Data.Event.Data, "alert event should have data")

		// ConsecutiveFailures assertions
		s.Equal(expectedCounts[i], alert.Alert.Data.ConsecutiveFailures,
			"alert %d should have %d consecutive failures", i, expectedCounts[i])
		s.Equal(20, alert.Alert.Data.MaxConsecutiveFailures, "max consecutive failures should be 20")

		// WillDisable assertion (should be true for last alert only)
		if i == len(alerts)-1 {
			s.True(alert.Alert.Data.WillDisable, "last alert should have will_disable=true")
		}

		// AttemptResponse assertion
		s.NotNil(alert.Alert.Data.AttemptResponse, "alert should have attempt_response")
	}
}

func (s *basicSuite) TestAlerts_SuccessResetsConsecutiveFailureCounter() {
	tenant := s.createTenant()
	dest := s.createWebhookDestination(tenant.ID, "*", withSecret(testSecret))

	// First batch: 14 failures
	for i := range 14 {
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
	for i := range 14 {
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
		// Auth header assertion
		s.Equal(fmt.Sprintf("Bearer %s", s.config.APIKey), alert.AuthHeader, "auth header should match")

		// Topic assertion
		s.Equal("alert.consecutive_failure", alert.Alert.Topic, "alert topic should be alert.consecutive_failure")

		// TenantID assertion
		s.NotEmpty(alert.Alert.Data.TenantID, "alert should have tenant_id")
		s.Equal(tenant.ID, alert.Alert.Data.TenantID, "alert tenant_id should match")

		// Destination assertions
		s.Require().NotNil(alert.Alert.Data.Destination, "alert should have destination")
		s.Equal(dest.ID, alert.Alert.Data.Destination.ID, "alert destination ID should match")
		s.Equal(tenant.ID, alert.Alert.Data.Destination.TenantID, "alert destination tenant_id should match")
		s.Equal("webhook", alert.Alert.Data.Destination.Type, "alert destination type should be webhook")

		// Event assertions
		s.NotEmpty(alert.Alert.Data.Event.ID, "alert event should have ID")
		s.Equal("user.created", alert.Alert.Data.Event.Topic, "alert event topic should match")
		s.NotNil(alert.Alert.Data.Event.Data, "alert event should have data")

		// ConsecutiveFailures assertions
		s.Equal(expectedCounts[i], alert.Alert.Data.ConsecutiveFailures,
			"alert %d should have %d consecutive failures", i, expectedCounts[i])
		s.Equal(20, alert.Alert.Data.MaxConsecutiveFailures, "max consecutive failures should be 20")

		// WillDisable assertion (none should have will_disable=true since counter resets)
		s.False(alert.Alert.Data.WillDisable, "alert %d should have will_disable=false (counter resets)", i)

		// AttemptResponse assertion
		s.NotNil(alert.Alert.Data.AttemptResponse, "alert should have attempt_response")
	}
}
