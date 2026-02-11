import { describe, it } from 'mocha';
import { expect } from 'chai';
import { createSdkClient } from '../utils/sdk-client';

/**
 * Topic management tests.
 *
 * API/SDK support: The OpenAPI spec only defines GET /topics (list). The SDK exposes
 * sdk.topics.list() which returns the list of available event topics configured in the
 * Outpost instance. There are no create/update/delete topic endpoints in the public API,
 * so topic configuration (like in the portal UI) is done via server config or internal APIs.
 *
 * These tests cover:
 * - Listing topics and validating response shape
 * - Before-style check: test-only topic names must not appear in the list (so we don't
 *   rely on leftover test data; if the API later adds create/delete, we can add
 *   create → list (exists) → delete → list (absent) tests).
 */

/** Topic names used only for spec-SDK tests; they must not exist before/after. */
const TEST_ONLY_TOPIC_NAMES = [
  'spec-sdk-test.placeholder',
  'outpost.spec-test.topic',
  'test.only.topic.management',
];

describe('Topics - List and sanity checks', () => {
  it('should list topics and return an array of strings', async function () {
    const client = createSdkClient();
    const sdk = client.getSDK();

    const topics = await sdk.topics.list();

    expect(topics).to.be.an('array');
    topics.forEach((t: string, i: number) => {
      expect(t, `topic[${i}]`).to.be.a('string');
      expect(t.length, `topic[${i}]`).to.be.greaterThan(0);
    });
  });

  it('should not contain test-only placeholder topics (before/after sanity)', async function () {
    // Skip unless SPEC_STRICT_TOPICS=true; real deployments may legitimately have these topic names.
    if (process.env.SPEC_STRICT_TOPICS !== 'true') {
      this.skip();
    }
    const client = createSdkClient();
    const sdk = client.getSDK();

    const topics = await sdk.topics.list();
    const set = new Set(topics);

    for (const testTopic of TEST_ONLY_TOPIC_NAMES) {
      expect(set.has(testTopic), `Topic "${testTopic}" should not exist in instance list`).to.be.false;
    }
  });

  it('should include configured instance topics when TEST_TOPICS is set', async function () {
    const required = process.env.TEST_TOPICS?.split(',').map((t) => t.trim()).filter(Boolean);
    if (!required?.length) {
      this.skip();
      return;
    }

    const client = createSdkClient();
    const sdk = client.getSDK();
    const topics = await sdk.topics.list();
    const set = new Set(topics);

    for (const name of required) {
      expect(set.has(name), `Configured topic "${name}" (TEST_TOPICS) should be in list`).to.be.true;
    }
  });
});
