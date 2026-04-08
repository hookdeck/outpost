package e2e_test

import (
	"encoding/json"

	opeventsmock "github.com/hookdeck/outpost/cmd/e2e/opevents"
)

func (s *basicSuite) TestOpEvents_ConsecutiveFailuresAndDisable() {
	tenant := s.createTenant()
	dest := s.createWebhookDestination(tenant.ID, "*", withSecret(testSecret))

	// Publish 20 failing events (autoDisableFailureCount defaults to 20)
	for i := 0; i < 20; i++ {
		s.publish(tenant.ID, "user.created", map[string]any{
			"index": i,
		}, withPublishMetadata(map[string]string{"should_err": "true"}))
	}

	// Wait for destination to be disabled (sync point for all 20 deliveries)
	s.waitForNewDestinationDisabled(tenant.ID, dest.ID)

	// Verify consecutive_failure alerts received at thresholds
	cfEvents := s.waitForOpEvents("alert.destination.consecutive_failure", 4)
	s.Require().GreaterOrEqual(len(cfEvents), 4, "should have at least 4 cf events (50%, 70%, 90%, 100%)")

	// Verify envelope shape on first event
	first := cfEvents[0]
	s.NotEmpty(first.Event.ID, "event ID should be set")
	s.Equal("alert.destination.consecutive_failure", first.Event.Topic)
	s.NotZero(first.Event.Time, "time should be set")
	s.Equal(tenant.ID, first.Event.TenantID)

	// Verify HMAC signature
	s.True(opeventsmock.VerifySignature(first, "test-opevents-secret"), "HMAC signature should be valid")

	// Verify data shape
	var cfData struct {
		TenantID    string `json:"tenant_id"`
		Destination struct {
			ID string `json:"id"`
		} `json:"destination"`
		ConsecutiveFailures struct {
			Current   int `json:"current"`
			Max       int `json:"max"`
			Threshold int `json:"threshold"`
		} `json:"consecutive_failures"`
	}
	s.Require().NoError(json.Unmarshal(first.Event.Data, &cfData))
	s.Equal(tenant.ID, cfData.TenantID)
	s.Equal(dest.ID, cfData.Destination.ID)
	s.Greater(cfData.ConsecutiveFailures.Current, 0)

	// Verify disabled alert received
	disabledEvents := s.waitForOpEvents("alert.destination.disabled", 1)
	s.Require().GreaterOrEqual(len(disabledEvents), 1)

	var disabledData struct {
		TenantID    string `json:"tenant_id"`
		Destination struct {
			ID string `json:"id"`
		} `json:"destination"`
		Reason string `json:"reason"`
	}
	s.Require().NoError(json.Unmarshal(disabledEvents[0].Event.Data, &disabledData))
	s.Equal(dest.ID, disabledData.Destination.ID)
	s.Equal("consecutive_failure", disabledData.Reason)
}

func (s *basicSuite) TestOpEvents_ExhaustedRetries() {
	tenant := s.createTenant()
	dest := s.createWebhookDestination(tenant.ID, "*", withSecret(testSecret))

	// Publish a failing event with retry enabled
	// RetryMaxLimit=3 in e2e config, so 4 attempts total (1 initial + 3 retries)
	s.publish(tenant.ID, "user.created", map[string]any{
		"test": "exhausted",
	},
		withRetry(),
		withPublishMetadata(map[string]string{"should_err": "true"}),
	)

	// Wait for all attempts (initial + 3 retries = 4 mock server events)
	s.waitForNewMockServerEvents(dest.mockID, 4)

	// Wait for exhausted_retries event
	erEvents := s.waitForOpEvents("alert.attempt.exhausted_retries", 1)
	s.Require().GreaterOrEqual(len(erEvents), 1)

	// Verify envelope
	event := erEvents[0]
	s.Equal("alert.attempt.exhausted_retries", event.Event.Topic)
	s.Equal(tenant.ID, event.Event.TenantID)
	s.True(opeventsmock.VerifySignature(event, "test-opevents-secret"))

	// Verify data
	var erData struct {
		TenantID    string `json:"tenant_id"`
		Destination struct {
			ID string `json:"id"`
		} `json:"destination"`
		Attempt struct {
			AttemptNumber int `json:"attempt_number"`
		} `json:"attempt"`
	}
	s.Require().NoError(json.Unmarshal(event.Event.Data, &erData))
	s.Equal(dest.ID, erData.Destination.ID)
	s.Equal(4, erData.Attempt.AttemptNumber)
}

func (s *basicSuite) TestOpEvents_SubscriptionUpdated() {
	tenant := s.createTenant()

	// Create destination — should emit subscription update (count 0 → 1, topics change)
	dest := s.createWebhookDestination(tenant.ID, "user.created", withSecret(testSecret))

	subEvents := s.waitForOpEvents("tenant.subscription.updated", 1)
	s.Require().GreaterOrEqual(len(subEvents), 1)

	// Verify envelope
	event := subEvents[0]
	s.Equal("tenant.subscription.updated", event.Event.Topic)
	s.Equal(tenant.ID, event.Event.TenantID)
	s.True(opeventsmock.VerifySignature(event, "test-opevents-secret"))

	// Verify data — count went from 0 to 1
	var createData struct {
		TenantID                  string   `json:"tenant_id"`
		Topics                    []string `json:"topics"`
		PreviousTopics            []string `json:"previous_topics"`
		DestinationsCount         int      `json:"destinations_count"`
		PreviousDestinationsCount int      `json:"previous_destinations_count"`
	}
	s.Require().NoError(json.Unmarshal(event.Event.Data, &createData))
	s.Equal(tenant.ID, createData.TenantID)
	s.Equal(1, createData.DestinationsCount)
	s.Equal(0, createData.PreviousDestinationsCount)
	s.Contains(createData.Topics, "user.created")

	// Reset and update destination topics
	s.opeventsServer.Reset()

	status := s.doJSON("PATCH", s.apiURL("/tenants/"+tenant.ID+"/destinations/"+dest.ID), map[string]any{
		"topics": []string{"user.deleted"},
	}, nil)
	s.Require().Equal(200, status)

	subEvents = s.waitForOpEvents("tenant.subscription.updated", 1)
	s.Require().GreaterOrEqual(len(subEvents), 1)

	var updateData struct {
		Topics         []string `json:"topics"`
		PreviousTopics []string `json:"previous_topics"`
	}
	s.Require().NoError(json.Unmarshal(subEvents[0].Event.Data, &updateData))
	s.Contains(updateData.Topics, "user.deleted")
	s.Contains(updateData.PreviousTopics, "user.created")

	// Reset and delete destination — count goes from 1 to 0
	s.opeventsServer.Reset()

	status = s.doJSON("DELETE", s.apiURL("/tenants/"+tenant.ID+"/destinations/"+dest.ID), nil, nil)
	s.Require().Equal(200, status)

	subEvents = s.waitForOpEvents("tenant.subscription.updated", 1)
	s.Require().GreaterOrEqual(len(subEvents), 1)

	var deleteData struct {
		DestinationsCount         int `json:"destinations_count"`
		PreviousDestinationsCount int `json:"previous_destinations_count"`
	}
	s.Require().NoError(json.Unmarshal(subEvents[0].Event.Data, &deleteData))
	s.Equal(0, deleteData.DestinationsCount)
	s.Equal(1, deleteData.PreviousDestinationsCount)
}
