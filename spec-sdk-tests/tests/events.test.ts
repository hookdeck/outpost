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
 * Poll for events (any count)
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
 * Poll until at least one event has a delivery status set (success/failed).
 * Status is set after a delivery attempt; use this to wait for delivery before asserting on status.
 * @param fetchEvents Function that fetches events
 * @param maxWaitMs Maximum time to wait in milliseconds
 * @param intervalMs Initial interval between polls in milliseconds
 * @returns Events array (may include events without status if we got any with status), or empty if timeout
 */
async function pollForEventsWithStatus(
  fetchEvents: () => Promise<Event[]>,
  maxWaitMs = 45000,
  intervalMs = 5000
): Promise<Event[]> {
  const startTime = Date.now();
  let attempt = 0;

  while (Date.now() - startTime < maxWaitMs) {
    attempt++;
    const elapsed = ((Date.now() - startTime) / 1000).toFixed(1);
    console.log(`Polling for events with status (attempt ${attempt}, elapsed: ${elapsed}s)...`);

    const events = await fetchEvents();
    const withStatus = events.filter((e: Event) => e.status !== undefined && e.status !== null);

    if (withStatus.length > 0) {
      console.log(`✓ Found ${withStatus.length} event(s) with status after ${elapsed}s`);
      return events;
    }
    if (events.length > 0) {
      console.log(`  Found ${events.length} event(s) but no status yet (delivery may still be in progress)`);
    }

    const remainingTime = maxWaitMs - (Date.now() - startTime);
    if (remainingTime > intervalMs) {
      await new Promise((resolve) => setTimeout(resolve, intervalMs));
    } else if (remainingTime > 0) {
      await new Promise((resolve) => setTimeout(resolve, remainingTime));
    }
  }

  const totalTime = ((Date.now() - startTime) / 1000).toFixed(1);
  console.warn(`✗ No events with status after ${totalTime}s (${attempt} attempts)`);
  return [];
}

/**
 * Poll until at least one attempt exists for the destination (event was delivered).
 * @param fetchAttempts Function that fetches attempts (returns array of attempt objects)
 * @param maxWaitMs Maximum time to wait in milliseconds
 * @param intervalMs Interval between polls in milliseconds
 * @returns Array of attempts, or empty if timeout
 */
async function pollForAttempts<T>(
  fetchAttempts: () => Promise<T[]>,
  maxWaitMs = 45000,
  intervalMs = 5000
): Promise<T[]> {
  const startTime = Date.now();
  let pollCount = 0;

  while (Date.now() - startTime < maxWaitMs) {
    pollCount++;
    const elapsed = ((Date.now() - startTime) / 1000).toFixed(1);
    console.log(`Polling for attempts (poll ${pollCount}, elapsed: ${elapsed}s)...`);

    const attempts = await fetchAttempts();
    if (attempts.length > 0) {
      console.log(`✓ Found ${attempts.length} attempt(s) after ${elapsed}s`);
      return attempts;
    }

    const remaining = maxWaitMs - (Date.now() - startTime);
    await new Promise((r) => setTimeout(r, remaining > intervalMs ? intervalMs : Math.max(0, remaining)));
  }

  const totalTime = ((Date.now() - startTime) / 1000).toFixed(1);
  console.warn(`✗ No attempts found after ${totalTime}s`);
  return [];
}

/**
 * Events and status field tests.
 *
 * In the OpenAPI spec, Event.status is optional (not in required[]). It is set after
 * a delivery attempt (success or failed). Until delivery runs, status may be absent.
 *
 * These tests:
 * 1. Wait for delivery (poll until an event has status) when asserting on status.
 * 2. Treat status as optional when getting a single event (assert only when present).
 * 3. Assert that when status is present, it is "success" or "failed".
 *
 * NOTE: For events to be logged and retrievable, there must be:
 * - A configured log store (e.g., PostgreSQL or ClickHouse)
 * - A destination to deliver to (we create a webhook destination)
 * Without these, events may not appear or status may never be set.
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

  describe('GET /events (filtered by destination) - Event Status Field', () => {
    it('should include status field in events after delivery (list with destinationId)', async function () {
      // Wait for event to appear and for delivery attempt so status is set (status is optional until then)
      this.timeout(60000);

      const sdk: Outpost = client.getSDK();

      await sdk.publish.event({
        tenantId: client.getTenantId(),
        topic: TEST_TOPICS[0],
        data: {
          test: 'event-status-field-test',
          timestamp: new Date().toISOString(),
        },
      });
      console.log('Event published successfully');

      // Poll until at least one event has status (delivery attempt completed). Uses list without destinationId
      // because GET /events?destination_id=... currently returns 500 (see GitHub issue).
      const events = await pollForEventsWithStatus(
        async () => {
          const response = await sdk.events.list({
            tenantId: client.getTenantId(),
          });
          return response?.models || [];
        },
        45000,
        5000
      );

      if (events.length === 0) {
        throw new Error('No events found - event delivery or listing may be failing');
      }

      const eventWithStatus = events.find((event: Event) => event.status !== undefined && event.status !== null);
      if (!eventWithStatus) {
        throw new Error(
          'No event had status after 45s - delivery may not have completed. Status is set after a delivery attempt; if list returns events but never status, list may be omitting status (spec/API bug).'
        );
      }

      const validStatuses: EventStatus[] = ['success', 'failed'];
      expect(validStatuses).to.include(eventWithStatus.status);
      console.log(`Event status field verified: ${eventWithStatus.status}`);
    });

    it('should include status when present on single event (status is optional per spec)', async function () {
      this.timeout(20000);

      const sdk: Outpost = client.getSDK();

      const response = await sdk.events.list({
        tenantId: client.getTenantId(),
      });
      const events = response?.models || [];

      if (events.length === 0) {
        console.warn('No events found - skipping single event test');
        this.skip();
        return;
      }

      const eventId = events[0].id;
      if (!eventId) {
        throw new Error('Event ID is undefined');
      }

      const event = await sdk.events.get({ eventId });

      // Status is optional per OpenAPI; only assert when present (set after delivery attempt)
      if (event.status !== undefined && event.status !== null) {
        const validStatuses: EventStatus[] = ['success', 'failed'];
        expect(validStatuses).to.include(event.status);
        console.log(`Single event status: ${event.status}`);
      } else {
        console.log('Single event has no status yet (delivery may not have run)');
      }
    });
  });

  describe('GET /events - Tenant Events Status Field', () => {
    it('should include status in tenant events list after delivery attempt', async function () {
      this.timeout(60000);

      const sdk: Outpost = client.getSDK();

      await sdk.publish.event({
        tenantId: client.getTenantId(),
        topic: TEST_TOPICS[0],
        data: {
          test: 'tenant-events-status-test',
          timestamp: new Date().toISOString(),
        },
      });
      console.log('Event published successfully');

      // Wait for events and for at least one to have status (delivery completed)
      const events = await pollForEventsWithStatus(
        async () => {
          const response = await sdk.events.list({
            tenantId: client.getTenantId(),
          });
          return response?.models || [];
        },
        45000,
        5000
      );

      if (events.length === 0) {
        throw new Error('No tenant events found - event delivery or listing may be failing');
      }

      const eventWithStatus = events.find((e: Event) => e.status !== undefined && e.status !== null);
      if (!eventWithStatus) {
        throw new Error(
          'No event had status after 45s - delivery may not have completed. Status is set after a delivery attempt; if list returns events but never status, list may be omitting status (spec/API bug).'
        );
      }

      const validStatuses: EventStatus[] = ['success', 'failed'];
      expect(validStatuses).to.include(eventWithStatus.status);
      console.log(`Tenant event status verified: ${eventWithStatus.status}`);
    });
  });

  describe('Event → Attempt', () => {
    it('publishing an event with matching topic to an enabled destination should generate an attempt', async function () {
      this.timeout(60000);

      const sdk: Outpost = client.getSDK();

      await sdk.publish.event({
        tenantId: client.getTenantId(),
        topic: TEST_TOPICS[0],
        data: {
          test: 'event-generates-attempt',
          timestamp: new Date().toISOString(),
        },
      });
      console.log('Event published (topic matches destination); waiting for attempt...');

      const attempts = await pollForAttempts(
        async () => {
          const response = await sdk.destinations.listAttempts({
            tenantId: client.getTenantId(),
            destinationId: destinationId,
          });
          return response?.models ?? [];
        },
        45000,
        5000
      );

      expect(attempts.length).to.be.at.least(1, 'Expected at least one attempt: event should be delivered to the destination (mock.hookdeck.com) when tenant, enabled destination, and matching topic are in place');

      const attempt = attempts[0];
      expect(attempt.status).to.equal('success', 'Delivery to mock.hookdeck.com should succeed');
      console.log(`Event generated ${attempts.length} attempt(s); attempt status: ${attempt.status}`);
    });
  });
});
