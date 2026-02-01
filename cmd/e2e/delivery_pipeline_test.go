package e2e_test

import (
	"time"

	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/util/testutil"
)

func (s *basicSuite) TestDeliveryPipeline_PublishDeliversToWebhook() {
	tenant := s.createTenant()
	dest := s.createWebhookDestination(tenant.ID, "*", withSecret(testSecret))

	s.publish(tenant.ID, "user.created", map[string]any{
		"event_id": "delivery_test_1",
	})

	// Verify mock server received the event
	events := s.waitForNewMockServerEvents(dest.mockID, 1)
	s.Require().Len(events, 1)
	s.True(events[0].Success, "delivery should succeed")
	s.True(events[0].Verified, "signature should be verified")
	s.Equal("delivery_test_1", events[0].Payload["event_id"])

	// Verify attempt was logged
	attempts := s.waitForNewAttempts(tenant.ID, 1)
	s.Require().GreaterOrEqual(len(attempts), 1)
	first := attempts[0]
	s.NotEmpty(first["id"])
	s.Equal(dest.ID, first["destination_id"])
	s.NotEmpty(first["status"])
}

func (s *basicSuite) TestDeliveryPipeline_PublishRespectsDataFilter() {
	tenant := s.createTenant()
	dest := s.createWebhookDestination(tenant.ID, "*",
		withSecret(testSecret),
		withFilter(map[string]any{
			"data": map[string]any{
				"amount": map[string]any{
					"$gte": 100,
				},
			},
		}),
	)

	// Publish matching event (amount >= 100)
	s.publish(tenant.ID, "user.created", map[string]any{
		"event_id": "filter_match",
		"amount":   150,
	})

	events := s.waitForNewMockServerEvents(dest.mockID, 1)
	s.Require().Len(events, 1)
	s.True(events[0].Success)
	s.True(events[0].Verified)
	s.Equal("filter_match", events[0].Payload["event_id"])

	// Clear events, then publish non-matching (amount < 100)
	s.clearMockServerEvents(dest.mockID)

	s.publish(tenant.ID, "user.created", map[string]any{
		"event_id": "filter_no_match",
		"amount":   50,
	})

	// Publish another matching event to prove the pipeline is active
	// (rather than just being slow).
	s.publish(tenant.ID, "user.created", map[string]any{
		"event_id": "filter_proof",
		"amount":   200,
	})

	events = s.waitForNewMockServerEvents(dest.mockID, 1)
	s.Require().Len(events, 1)
	s.Equal("filter_proof", events[0].Payload["event_id"],
		"only the matching event should be delivered; non-matching event was filtered")
}

func (s *basicSuite) TestDeliveryPipeline_DisabledDestinationSkipsDelivery() {
	tenant := s.createTenant()
	dest := s.createWebhookDestination(tenant.ID, "*", withSecret(testSecret))

	// Disable the destination
	s.disableDestination(tenant.ID, dest.ID)

	// Publish — should NOT be delivered
	s.publish(tenant.ID, "user.created", map[string]any{
		"event_id": "disabled_test",
	})

	s.assertNoDelivery(dest.mockID, 500*time.Millisecond)
}

func (s *basicSuite) TestDeliveryPipeline_MultipleDestinationsEachReceiveDelivery() {
	tenant := s.createTenant()
	dest1 := s.createWebhookDestination(tenant.ID, "*", withSecret(testSecret))
	dest2 := s.createWebhookDestination(tenant.ID, "*", withSecret(testSecret))

	s.publish(tenant.ID, "user.created", map[string]any{
		"event_id": "multi_dest_test",
	})

	// Both destinations should receive the event
	events1 := s.waitForNewMockServerEvents(dest1.mockID, 1)
	events2 := s.waitForNewMockServerEvents(dest2.mockID, 1)

	s.Require().Len(events1, 1)
	s.Require().Len(events2, 1)
	s.Equal("multi_dest_test", events1[0].Payload["event_id"])
	s.Equal("multi_dest_test", events2[0].Payload["event_id"])
}

func (s *basicSuite) TestDeliveryPipeline_DuplicateEventPublishReturnsDuplicate() {
	tenant := s.createTenant()
	s.createWebhookDestination(tenant.ID, "*")

	eventID := idgen.Event()

	resp1 := s.publish(tenant.ID, "user.created", map[string]any{
		"event_id": "dup_test",
	}, withEventID(eventID))
	s.False(resp1.Duplicate, "first publish should not be duplicate")

	resp2 := s.publish(tenant.ID, "user.created", map[string]any{
		"event_id": "dup_test",
	}, withEventID(eventID))
	s.True(resp2.Duplicate, "second publish with same ID should be duplicate")
}

func (s *basicSuite) TestDeliveryPipeline_TopicRoutesOnlyToMatchingDestinations() {
	topicA := testutil.TestTopics[0] // "user.created"
	topicB := testutil.TestTopics[1] // "user.deleted"

	tenant := s.createTenant()
	destA := s.createWebhookDestination(tenant.ID, topicA, withSecret(testSecret))
	destB := s.createWebhookDestination(tenant.ID, topicB, withSecret(testSecret))

	// Publish an event for each topic
	s.publish(tenant.ID, topicB, map[string]any{
		"event_id": "topic_b_1",
	})
	s.publish(tenant.ID, topicA, map[string]any{
		"event_id": "topic_a_1",
	})

	// Each destination should receive exactly its matching event.
	// Since topicA was published after topicB, by the time it arrives
	// the pipeline has already routed the topicB event.
	eventsA := s.waitForNewMockServerEvents(destA.mockID, 1)
	eventsB := s.waitForNewMockServerEvents(destB.mockID, 1)

	s.Require().Len(eventsA, 1)
	s.Equal("topic_a_1", eventsA[0].Payload["event_id"],
		"%s destination should only receive %s events", topicA, topicA)

	s.Require().Len(eventsB, 1)
	s.Equal("topic_b_1", eventsB[0].Payload["event_id"],
		"%s destination should only receive %s events", topicB, topicB)
}

func (s *basicSuite) TestDeliveryPipeline_EnableAfterDisableResumesDelivery() {
	tenant := s.createTenant()
	dest := s.createWebhookDestination(tenant.ID, "*")

	// Disable the destination
	s.disableDestination(tenant.ID, dest.ID)

	// Publish — should NOT be delivered
	s.publish(tenant.ID, "user.created", map[string]any{
		"event_id": "pre_enable",
	})
	s.assertNoDelivery(dest.mockID, 500*time.Millisecond)

	// Re-enable
	s.enableDestination(tenant.ID, dest.ID)

	// Publish — should be delivered
	s.publish(tenant.ID, "user.created", map[string]any{
		"event_id": "post_enable",
	})

	events := s.waitForNewMockServerEvents(dest.mockID, 1)
	s.Require().Len(events, 1)
	s.Equal("post_enable", events[0].Payload["event_id"])
}
