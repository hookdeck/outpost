import { describe, it, before, after } from 'mocha';
import { expect } from 'chai';
import { SdkClient, createSdkClient } from '../utils/sdk-client';
import { createWebhookDestination } from '../factories/destination.factory';
import type {
  Event,
  EventStatus,
} from '../../sdks/outpost-typescript/dist/commonjs/models/components';
import type { Outpost } from '../../sdks/outpost-typescript/dist/commonjs';
/* eslint-disable no-console */
/* eslint-disable no-undef */

// Get configured test topics from environment (required)
if (!process.env.TEST_TOPICS) {
  throw new Error('TEST_TOPICS environment variable is required. Please set it in .env file.');
}
const TEST_TOPICS = process.env.TEST_TOPICS.split(',').map((t) => t.trim());

/**
 * Poll for events with exponential backoff
 * @param fetchEvents Function that fetches events
 * @param maxWaitMs Maximum time to wait in milliseconds
 * @param intervalMs Initial interval between polls in milliseconds
 * @returns Events array or empty array if timeout
 */
async function pollForEvents(
  fetchEvents: () => Promise<Event[]>,
  maxWaitMs = 30000,
  intervalMs = 5000
): Promise<Event[]> {
  const startTime = Date.now();
  let attempt = 0;

  while (Date.now() - startTime < maxWaitMs) {
    attempt++;
    const elapsed = ((Date.now() - startTime) / 1000).toFixed(1);
    console.log(`Polling for events (attempt ${attempt}, elapsed: ${elapsed}s)...`);

    const events = await fetchEvents();

    if (events.length > 0) {
      console.log(`✓ Found ${events.length} event(s) after ${elapsed}s`);
      return events;
    }

    // Wait before next poll
    const remainingTime = maxWaitMs - (Date.now() - startTime);
    if (remainingTime > intervalMs) {
      await new Promise((resolve) => setTimeout(resolve, intervalMs));
    } else if (remainingTime > 0) {
      // Wait for remaining time on last attempt
      await new Promise((resolve) => setTimeout(resolve, remainingTime));
    }
  }

  const totalTime = ((Date.now() - startTime) / 1000).toFixed(1);
  console.warn(`✗ No events found after ${totalTime}s (${attempt} attempts)`);
  return [];
}

/**
 * Tests for PR #491: https://github.com/hookdeck/outpost/pull/491
 *
 * This PR fixes issue #490 where the Event schema was missing the `status` field
 * that is returned by the API. The API returns events with a `status` field
 * (enum: "success" | "failed") but this field was not defined in the OpenAPI spec,
 * causing SDK clients to not have access to this important field.
 *
 * These tests verify that:
 * 1. Events returned from the API include the `status` field
 * 2. The `status` field has valid values ("success" or "failed")
 * 3. The SDK properly types and exposes the `status` field
 *
 * NOTE: For events to be logged and retrievable, there must be:
 * 1. A configured log store (e.g., PostgreSQL or ClickHouse)
 * 2. A subscriber actively consuming from the destination
 * Without these, events may not appear in the event lists.
 */
describe('Events - Status Field Tests (PR #491)', () => {
  let client: SdkClient;
  let destinationId: string;

  before(async function () {
    // Increase timeout for setup
    this.timeout(30000);

    client = createSdkClient();

    // Create tenant if it doesn't exist (idempotent operation)
    try {
      await client.upsertTenant();
      console.log('Test tenant created/verified');
    } catch (error) {
      console.warn('Failed to create tenant (may already exist):', error);
    }

    // Create a webhook destination for testing
    // Using mock.hookdeck.com which accepts webhooks for testing purposes
    try {
      const destinationData = createWebhookDestination({
        topics: [TEST_TOPICS[0]],
        config: {
          url: 'https://mock.hookdeck.com/webhook/outpost-test',
        },
      });
      const destination = await client.createDestination(destinationData);
      destinationId = destination.id;
      console.log(`Created test destination: ${destinationId}`);
    } catch (error) {
      console.error('Failed to create destination:', error);
      throw error;
    }
  });

  after(async () => {
    // Cleanup: delete the test destination
    if (destinationId) {
      try {
        await client.deleteDestination(destinationId);
        console.log('Test destination deleted');
      } catch (error) {
        console.warn('Failed to delete destination:', error);
      }
    }

    // Cleanup: delete the test tenant
    try {
      await client.deleteTenant();
      console.log('Test tenant deleted');
    } catch (error) {
      console.warn('Failed to delete tenant:', error);
    }
  });

  describe('GET /api/v1/{tenant_id}/destinations/{destination_id}/events - Event Status Field', () => {
    it('should include status field in events returned from listByDestination', async function () {
      // Increase timeout for this test as it involves publishing and waiting for event delivery
      this.timeout(45000);

      // Get the underlying SDK to access the events and publish methods
      const sdk: Outpost = client.getSDK();

      // Publish an event - it will be routed to the destination by topic matching
      await sdk.publish.event({
        tenantId: client.getTenantId(),
        topic: TEST_TOPICS[0],
        data: {
          test: 'event-status-field-test',
          timestamp: new Date().toISOString(),
        },
      });
      console.log('Event published successfully');

      // Poll for events with 5s intervals, max 30s wait
      const events = await pollForEvents(
        async () => {
          const response = await sdk.events.listByDestination({
            tenantId: client.getTenantId(),
            destinationId: destinationId,
          });
          return response?.result?.data || [];
        },
        30000,
        5000
      );

      if (events.length === 0) {
        throw new Error('No events found after 30 seconds - event delivery may be failing');
      }

      // Verify that at least one event has the status field
      const eventWithStatus = events.find((event: Event) => event.status !== undefined);

      expect(eventWithStatus).to.exist;
      expect(eventWithStatus!.status).to.exist;

      // Verify the status field has a valid value
      const validStatuses: EventStatus[] = ['success', 'failed'];
      expect(validStatuses).to.include(eventWithStatus!.status);

      console.log(`Event status field verified: ${eventWithStatus!.status}`);
    });

    it('should include status field when getting a single event', async function () {
      // Increase timeout for this test (no need to publish, just retrieve)
      this.timeout(20000);

      const sdk: Outpost = client.getSDK();

      // First, list events to get an event ID
      const response = await sdk.events.listByDestination({
        tenantId: client.getTenantId(),
        destinationId: destinationId,
      });

      // The SDK wraps the API response in a 'result' object
      const events = response?.result?.data || [];

      if (events.length === 0) {
        console.warn('No events found - skipping single event test');
        this.skip();
        return;
      }

      const eventId = events[0].id;
      if (!eventId) {
        throw new Error('Event ID is undefined');
      }

      console.log(`Getting event by ID: ${eventId}`);

      // Get the specific event
      const event = await sdk.events.getByDestination({
        tenantId: client.getTenantId(),
        destinationId: destinationId,
        eventId: eventId,
      });

      // Verify the status field exists
      expect(event.status).to.exist;
      const validStatuses: EventStatus[] = ['success', 'failed'];
      expect(validStatuses).to.include(event.status);

      console.log(`Single event status field verified: ${event.status}`);
    });
  });

  describe('GET /api/v1/{tenant_id}/events - Tenant Events Status Field', () => {
    it('should include status field in events returned from tenant events list', async function () {
      // Increase timeout for this test as it involves publishing and waiting for event delivery
      this.timeout(45000);

      const sdk: Outpost = client.getSDK();

      // Publish an event - it will be routed to the destination by topic matching
      await sdk.publish.event({
        tenantId: client.getTenantId(),
        topic: TEST_TOPICS[0],
        data: {
          test: 'tenant-events-status-test',
          timestamp: new Date().toISOString(),
        },
      });
      console.log('Event published successfully');

      // Poll for events with 5s intervals, max 30s wait
      const events = await pollForEvents(
        async () => {
          const response = await sdk.events.list({
            tenantId: client.getTenantId(),
          });
          return response?.result?.data || [];
        },
        30000,
        5000
      );

      if (events.length === 0) {
        throw new Error('No tenant events found after 30 seconds - event delivery may be failing');
      }

      // Verify that at least one event has the status field
      const eventWithStatus = events.find((event: Event) => event.status !== undefined);

      expect(eventWithStatus).to.exist;
      expect(eventWithStatus!.status).to.exist;
      const validStatuses: EventStatus[] = ['success', 'failed'];
      expect(validStatuses).to.include(eventWithStatus!.status);

      console.log(`Tenant event status field verified: ${eventWithStatus!.status}`);
    });
  });
});
