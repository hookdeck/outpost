import { describe, it, before } from 'mocha';
import { expect } from 'chai';
import { createSdkClient } from '../utils/sdk-client';
import { apiV2BaseURL, skipUnlessV2 } from '../utils/api-version';

/**
 * Topic management tests.
 *
 * API/SDK support: The OpenAPI spec only defines GET /topics (list). The SDK exposes
 * sdk.topics.list() which returns the event topics configured in the Outpost instance.
 * There are no create/update/delete topic endpoints in the public API, so topic
 * configuration (like in the portal UI) is done via server config or internal APIs.
 *
 * In API v2, GET /topics returns topic objects (name, validation, mcp and, for topics
 * with a schema, description and payload_schema); API v1 returns topic names. These
 * tests run against v2 on the server API_BASE_URL points at, and skip when the server
 * does not serve v2 (see utils/api-version.ts).
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

/** The SDK's v2 topic object; the SDK camel-cases payload_schema and replaced_by. */
interface Topic {
  name: string;
  description?: string;
  payloadSchema?: Record<string, unknown>;
  validation: string;
  mcp: { enabled: boolean };
  deprecated?: boolean;
  replacedBy?: string;
}

async function listTopics(): Promise<Topic[]> {
  const client = createSdkClient({ baseURL: apiV2BaseURL() });
  return client.getSDK().topics.list();
}

describe('Topics - List and sanity checks', () => {
  before(async function () {
    await skipUnlessV2(this);
  });

  it('should list topics and return an array of topic objects', async function () {
    const topics = await listTopics();

    expect(topics).to.be.an('array');
    topics.forEach((t, i) => {
      expect(t, `topic[${i}]`).to.be.an('object');
      expect(t.name, `topic[${i}].name`).to.be.a('string');
      expect(t.name.length, `topic[${i}].name`).to.be.greaterThan(0);
      expect(t.validation, `topic[${i}].validation`).to.be.oneOf(['off', 'warn', 'enforce']);
      expect(t.mcp, `topic[${i}].mcp`).to.be.an('object');
      expect(t.mcp.enabled, `topic[${i}].mcp.enabled`).to.be.a('boolean');
    });
  });

  it('should list each topic once', async function () {
    const names = (await listTopics()).map((t) => t.name);

    expect(new Set(names).size, `topics: ${names.join(', ')}`).to.equal(names.length);
  });

  it('should only validate or expose to MCP topics that have a payload schema', async function () {
    const topics = await listTopics();

    for (const t of topics.filter((t) => !t.payloadSchema)) {
      expect(t.validation, `${t.name} has no payload schema`).to.equal('off');
      expect(t.mcp.enabled, `${t.name} has no payload schema`).to.be.false;
    }
  });

  it('should not contain test-only placeholder topics (before/after sanity)', async function () {
    // Skip unless SPEC_STRICT_TOPICS=true; real deployments may legitimately have these topic names.
    if (process.env.SPEC_STRICT_TOPICS !== 'true') {
      this.skip();
    }

    const set = new Set((await listTopics()).map((t) => t.name));

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

    const set = new Set((await listTopics()).map((t) => t.name));

    for (const name of required) {
      expect(set.has(name), `Configured topic "${name}" (TEST_TOPICS) should be in list`).to.be.true;
    }
  });
});
