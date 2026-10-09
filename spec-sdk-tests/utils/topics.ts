import { apiBaseURL } from './api-version';

/**
 * Plain HTTP access to GET /topics, for checks that must work against either
 * API version.
 *
 * GET /topics returns topic names in API v1 and topic objects in API v2. The
 * generated SDK validates the response against the shape in the spec it was
 * generated from, so a lookup that has to work against both versions, or
 * against a server whose version the SDK wasn't generated for, can't go
 * through it.
 */

/** A topic object as API v2 returns it. */
export interface TopicObject {
  name: string;
  description?: string;
  payload_schema?: Record<string, unknown>;
  validation: string;
  mcp: { enabled: boolean };
  deprecated?: boolean;
  replaced_by?: string;
}

const MAX_ATTEMPTS = 4;

/**
 * Returns the parsed body of GET <baseURL>/topics, authenticated with apiKey.
 * Retries 429 and 5xx responses with backoff, like the SDK client.
 */
export async function fetchTopics(
  baseURL: string = apiBaseURL(),
  apiKey: string = process.env.API_KEY || ''
): Promise<unknown[]> {
  const url = `${baseURL.replace(/\/+$/, '')}/topics`;
  for (let attempt = 1; ; attempt++) {
    const res = await fetch(url, {
      headers: { Accept: 'application/json', Authorization: `Bearer ${apiKey}` },
    });
    const body = await res.text();
    if (process.env.DEBUG_API_REQUESTS === 'true') {
      console.log(`[topics] GET ${url} -> ${res.status}`);
    }
    if ((res.status === 429 || res.status >= 500) && attempt < MAX_ATTEMPTS) {
      await new Promise((r) => setTimeout(r, 250 * 2 ** attempt));
      continue;
    }
    if (res.status !== 200) {
      throw new Error(`GET ${url} returned ${res.status}: ${body}`);
    }
    const topics: unknown = JSON.parse(body);
    if (!Array.isArray(topics)) {
      throw new Error(`GET ${url} returned ${body}, not an array`);
    }
    return topics;
  }
}

/** The topic names in a GET /topics response of either API version. */
export function topicNames(topics: unknown[]): string[] {
  return topics.map((t, i) => {
    if (typeof t === 'string') {
      return t;
    }
    const name = (t as { name?: unknown } | null)?.name;
    if (typeof name !== 'string') {
      throw new Error(`topic[${i}] is neither a name nor a topic object: ${JSON.stringify(t)}`);
    }
    return name;
  });
}
