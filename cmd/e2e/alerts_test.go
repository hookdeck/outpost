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
	alerts := s.alertServer.GetAlertsForDestinationByTopic(dest.ID, "alert.consecutive_failure")
	s.Require().Len(alerts, 4, "should have 4 alerts")

	expectedCounts := []int{10, 14, 18, 20}
	for i, alert := range alerts {
		// Parse alert data
		data, err := alert.ParseConsecutiveFailureData()
		s.Require().NoError(err, "failed to parse consecutive failure data")

		// Auth header assertion
		s.Equal(fmt.Sprintf("Bearer %s", s.config.APIKey), alert.AuthHeader, "auth header should match")

		// Topic assertion
		s.Equal("alert.consecutive_failure", alert.Alert.Topic, "alert topic should be alert.consecutive_failure")

		// TenantID assertion
		s.NotEmpty(data.TenantID, "alert should have tenant_id")
		s.Equal(tenant.ID, data.TenantID, "alert tenant_id should match")

		// Destination assertions
		s.Require().NotNil(data.Destination, "alert should have destination")
		s.Equal(dest.ID, data.Destination.ID, "alert destination ID should match")
		s.Equal(tenant.ID, data.Destination.TenantID, "alert destination tenant_id should match")
		s.Equal("webhook", data.Destination.Type, "alert destination type should be webhook")

		// Event assertions
		s.NotEmpty(data.Event.ID, "alert event should have ID")
		s.Equal("user.created", data.Event.Topic, "alert event topic should match")
		s.NotNil(data.Event.Data, "alert event should have data")

		// ConsecutiveFailures assertions
		s.Equal(expectedCounts[i], data.ConsecutiveFailures,
			"alert %d should have %d consecutive failures", i, expectedCounts[i])
		s.Equal(20, data.MaxConsecutiveFailures, "max consecutive failures should be 20")

		// WillDisable assertion (should be true for last alert only)
		if i == len(alerts)-1 {
			s.True(data.WillDisable, "last alert should have will_disable=true")
		}

		// AttemptResponse assertion
		s.NotNil(data.AttemptResponse, "alert should have attempt_response")
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
	alerts := s.alertServer.GetAlertsForDestinationByTopic(dest.ID, "alert.consecutive_failure")
	s.Require().Len(alerts, 4, "should have 4 alerts")

	expectedCounts := []int{10, 14, 10, 14}
	for i, alert := range alerts {
		// Parse alert data
		data, err := alert.ParseConsecutiveFailureData()
		s.Require().NoError(err, "failed to parse consecutive failure data")

		// Auth header assertion
		s.Equal(fmt.Sprintf("Bearer %s", s.config.APIKey), alert.AuthHeader, "auth header should match")

		// Topic assertion
		s.Equal("alert.consecutive_failure", alert.Alert.Topic, "alert topic should be alert.consecutive_failure")

		// TenantID assertion
		s.NotEmpty(data.TenantID, "alert should have tenant_id")
		s.Equal(tenant.ID, data.TenantID, "alert tenant_id should match")

		// Destination assertions
		s.Require().NotNil(data.Destination, "alert should have destination")
		s.Equal(dest.ID, data.Destination.ID, "alert destination ID should match")
		s.Equal(tenant.ID, data.Destination.TenantID, "alert destination tenant_id should match")
		s.Equal("webhook", data.Destination.Type, "alert destination type should be webhook")

		// Event assertions
		s.NotEmpty(data.Event.ID, "alert event should have ID")
		s.Equal("user.created", data.Event.Topic, "alert event topic should match")
		s.NotNil(data.Event.Data, "alert event should have data")

		// ConsecutiveFailures assertions
		s.Equal(expectedCounts[i], data.ConsecutiveFailures,
			"alert %d should have %d consecutive failures", i, expectedCounts[i])
		s.Equal(20, data.MaxConsecutiveFailures, "max consecutive failures should be 20")

		// WillDisable assertion (none should have will_disable=true since counter resets)
		s.False(data.WillDisable, "alert %d should have will_disable=false (counter resets)", i)

		// AttemptResponse assertion
		s.NotNil(data.AttemptResponse, "alert should have attempt_response")
	}
}

func (s *basicSuite) TestAlerts_DestinationDisabledCallback() {
	tenant := s.createTenant()
	dest := s.createWebhookDestination(tenant.ID, "*", withSecret(testSecret))

	// Publish 20 failing events to trigger auto-disable
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

	// Wait for the destination.disabled alert callback
	s.waitForAlertsByTopic(dest.ID, "alert.destination.disabled", 1)
	alerts := s.alertServer.GetAlertsForDestinationByTopic(dest.ID, "alert.destination.disabled")
	s.Require().Len(alerts, 1, "should have 1 destination.disabled alert")

	alert := alerts[0]
	data, err := alert.ParseDestinationDisabledData()
	s.Require().NoError(err, "failed to parse destination disabled data")

	// Auth header assertion
	s.Equal(fmt.Sprintf("Bearer %s", s.config.APIKey), alert.AuthHeader, "auth header should match")

	// Topic assertion
	s.Equal("alert.destination.disabled", alert.Alert.Topic, "alert topic should be alert.destination.disabled")

	// TenantID assertion
	s.NotEmpty(data.TenantID, "alert should have tenant_id")
	s.Equal(tenant.ID, data.TenantID, "alert tenant_id should match")

	// Destination assertions
	s.Require().NotNil(data.Destination, "alert should have destination")
	s.Equal(dest.ID, data.Destination.ID, "alert destination ID should match")
	s.Equal(tenant.ID, data.Destination.TenantID, "alert destination tenant_id should match")
	s.Equal("webhook", data.Destination.Type, "alert destination type should be webhook")
	s.NotNil(data.Destination.DisabledAt, "destination should have disabled_at set")

	// DisabledAt assertion
	s.False(data.DisabledAt.IsZero(), "disabled_at should not be zero")

	// TriggeringEvent assertions (optional but expected)
	if data.TriggeringEvent != nil {
		s.NotEmpty(data.TriggeringEvent.ID, "triggering event should have ID")
		s.Equal("user.created", data.TriggeringEvent.Topic, "triggering event topic should match")
	}

	// ConsecutiveFailures assertions
	s.Equal(20, data.ConsecutiveFailures, "consecutive_failures should be 20")
	s.Equal(20, data.MaxConsecutiveFailures, "max_consecutive_failures should be 20")

	// AttemptResponse assertion
	s.NotNil(data.AttemptResponse, "alert should have attempt_response")
}
