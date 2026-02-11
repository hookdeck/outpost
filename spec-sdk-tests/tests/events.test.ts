import { describe, it, before, after } from 'mocha';
import { expect } from 'chai';
import { SdkClient, createSdkClient } from '../utils/sdk-client';
import { createWebhookDestination } from '../factories/destination.factory';
import type { Event } from '../../sdks/outpost-typescript/dist/commonjs/models/components';
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
 * Events tests.
 *
 * Event no longer has a status property in the spec; delivery outcome is on Attempt.
 * These tests verify listing events, getting a single event by ID, and that publishing
 * an event results in an attempt (with attempt.status).
 *
 * NOTE: For events to be logged and retrievable, there must be:
 * - A configured log store (e.g., PostgreSQL or ClickHouse)
 * - A destination to deliver to (we create a webhook destination)
 */
describe('Events (PR #491)', () => {
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

  describe('GET /events', () => {
    it('should list events by tenant', async function () {
      this.timeout(60000);

      const sdk: Outpost = client.getSDK();

      await sdk.publish.event({
        tenantId: client.getTenantId(),
        topic: TEST_TOPICS[0],
        data: {
          test: 'events-list-test',
          timestamp: new Date().toISOString(),
        },
      });
      console.log('Event published successfully');

      const events = await pollForEvents(
        async () => {
          const response = await sdk.events.list({
            tenantId: client.getTenantId(),
          });
          return response?.models || [];
        },
        30000,
        5000
      );

      expect(events.length).to.be.at.least(1, 'Expected at least one event after listing by tenant');
      console.log(`Listed ${events.length} event(s)`);
    });

    it('should get a single event by ID', async function () {
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
      expect(event).to.exist;
      expect(event.id).to.equal(eventId);
      console.log('Single event retrieved by ID');
    });
  });

  describe('Event → Attempt', () => {
    it('publishing an event with matching topic to an enabled destination should generate an attempt', async function () {
      this.timeout(60000);

      const sdk: Outpost = client.getSDK();

      const publishResponse = await sdk.publish.event({
        tenantId: client.getTenantId(),
        topic: TEST_TOPICS[0],
        data: {
          test: 'event-generates-attempt',
          timestamp: new Date().toISOString(),
        },
      });
      const eventId = publishResponse.id;
      console.log(`Event published (id=${eventId}); waiting for attempt...`);

      const attempts = await pollForAttempts(
        async () => {
          const response = await sdk.destinations.listAttempts({
            tenantId: client.getTenantId(),
            destinationId: destinationId,
            eventId,
          });
          return response?.models ?? [];
        },
        45000,
        5000
      );

      expect(attempts.length).to.be.at.least(1, 'Expected at least one attempt for the published event when tenant, enabled destination, and matching topic are in place');
      const attempt = attempts[0];
      expect(attempt.eventId).to.equal(eventId, 'Attempt should be for the event we just published');
      expect(attempt.status).to.equal('success', 'Delivery to mock.hookdeck.com should succeed');
      console.log(`Event ${eventId} generated attempt; status: ${attempt.status}`);
    });
  });
});
