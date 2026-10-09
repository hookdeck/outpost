import { describe, it, before } from 'mocha';
import { expect } from 'chai';
import * as components from '../../sdks/outpost-typescript/dist/commonjs/models/components';
import { createSdkClient } from '../utils/sdk-client';
import { apiV1BaseURL, apiV2BaseURL, skipUnlessV2 } from '../utils/api-version';
import { fetchTopics, topicNames, TopicObject } from '../utils/topics';

/**
 * Topic management tests.
 *
 * API/SDK support: The OpenAPI spec only defines GET /topics (list). The SDK exposes
 * sdk.topics.list() which returns the event topics configured in the Outpost instance.
 * There are no create/update/delete topic endpoints in the public API, so topic
 * configuration (like in the portal UI) is done via server config or internal APIs.
 *
 * In API v2, GET /topics returns topic objects (name, validation, mcp and, for topics
 * with a schema, description and payload_schema); API v1 returns topic names. The SDK
 * validates the response against the shape it was generated for, so only the SDK tests
 * go through it, against a version whose shape the SDK reads:
 * - API v1 (names): every SDK generated while sdks/schemas/speakeasy-modifications-overlay.yaml
 *   widens the GET /topics response reads names, so this runs everywhere. A failure here
 *   means a regenerated SDK can no longer read v1 servers, the default server of the SDKs.
 * - API v2 (objects): only SDKs generated with the TopicSchema model read objects. The SDK
 *   that spec-sdk-tests-vs-release builds can predate it, so the test skips for that SDK.
 * The other tests use plain HTTP (utils/topics.ts). The v2 object tests run against v2
 * on the server API_BASE_URL points at, and skip when the server does not serve v2 (see
 * utils/api-version.ts).
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

/** Whether the SDK under test was generated with API v2 topic objects. */
const sdkHasTopicObjects = 'TopicSchema$inboundSchema' in components;

describe('Topics - List and sanity checks', () => {
  it('should list each topic once', async function () {
    const names = topicNames(await fetchTopics());

    expect(new Set(names).size, `topics: ${names.join(', ')}`).to.equal(names.length);
  });

  it('should not contain test-only placeholder topics (before/after sanity)', async function () {
    // Skip unless SPEC_STRICT_TOPICS=true; real deployments may legitimately have these topic names.
    if (process.env.SPEC_STRICT_TOPICS !== 'true') {
      this.skip();
    }

    const set = new Set(topicNames(await fetchTopics()));

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

    const set = new Set(topicNames(await fetchTopics()));

    for (const name of required) {
      expect(set.has(name), `Configured topic "${name}" (TEST_TOPICS) should be in list`).to.be.true;
    }
  });
});

describe('Topics - SDK', () => {
  it('should list topic names through the SDK on API v1', async function () {
    const expected = await fetchTopics(apiV1BaseURL());
    expected.forEach((t, i) => expect(t, `API v1 topic[${i}]`).to.be.a('string'));

    const topics = await createSdkClient({ baseURL: apiV1BaseURL() }).getSDK().topics.list();

    expect(topics).to.deep.equal(expected);
  });

  it('should list topic objects through the SDK on API v2', async function () {
    await skipUnlessV2(this);
    if (!sdkHasTopicObjects) {
      // The SDK was generated before GET /topics returned topic objects.
      this.skip();
    }
    const expected = topicNames(await fetchTopics(apiV2BaseURL()));

    const topics: unknown[] = await createSdkClient({ baseURL: apiV2BaseURL() })
      .getSDK()
      .topics.list();

    topics.forEach((t, i) => expect(t, `topic[${i}]`).to.be.an('object'));
    expect(topicNames(topics)).to.deep.equal(expected);
  });
});

describe('Topics - API v2 topic objects', () => {
  before(async function () {
    await skipUnlessV2(this);
  });

  async function listTopics(): Promise<TopicObject[]> {
    return (await fetchTopics(apiV2BaseURL())) as TopicObject[];
  }

  it('should list topics and return an array of topic objects', async function () {
    const topics = await listTopics();

    topics.forEach((t, i) => {
      expect(t, `topic[${i}]`).to.be.an('object');
      expect(t.name, `topic[${i}].name`).to.be.a('string');
      expect(t.name.length, `topic[${i}].name`).to.be.greaterThan(0);
      expect(t.validation, `topic[${i}].validation`).to.be.oneOf(['off', 'warn', 'enforce']);
      expect(t.mcp, `topic[${i}].mcp`).to.be.an('object');
      expect(t.mcp.enabled, `topic[${i}].mcp.enabled`).to.be.a('boolean');
    });
  });

  it('should list the same topics as API v1, in the same order', async function () {
    const v1 = await fetchTopics(apiV1BaseURL());

    expect(topicNames(await listTopics())).to.deep.equal(v1);
  });

  it('should only validate or expose to MCP topics that have a payload schema', async function () {
    const topics = await listTopics();

    for (const t of topics.filter((t) => !t.payload_schema)) {
      expect(t.validation, `${t.name} has no payload schema`).to.equal('off');
      expect(t.mcp.enabled, `${t.name} has no payload schema`).to.be.false;
    }
  });
});
