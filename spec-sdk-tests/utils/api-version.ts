import type { Context } from 'mocha';

/**
 * API version helpers for tests of behaviour that differs between Outpost API
 * versions.
 *
 * API_BASE_URL points at one API version of a server, e.g.
 * http://localhost:3333/api/v2. v2-only tests run against the v2 base URL of
 * that server, so they also run when API_BASE_URL points at v1, and skip when
 * the server predates v2: GET <origin>/api/v2/healthz returns 404 there.
 * v1-only tests run against the v1 base URL, which every server serves.
 *
 * A base URL that does not end in /api/vN (a managed dated version) cannot be
 * rewritten or probed. It is used as it is and must point at a version that
 * serves what the test needs.
 */

const DEFAULT_BASE_URL = 'http://localhost:3333/api/v1';
const VERSIONED_PATH = /\/api\/v\d+\/?$/;

/** The base URL the suite is configured with. */
export function apiBaseURL(): string {
  return process.env.API_BASE_URL || DEFAULT_BASE_URL;
}

/** The base URL of API v1 on the server that baseURL points at. */
export function apiV1BaseURL(baseURL: string = apiBaseURL()): string {
  return baseURL.replace(VERSIONED_PATH, '/api/v1');
}

/** The base URL of API v2 on the server that baseURL points at. */
export function apiV2BaseURL(baseURL: string = apiBaseURL()): string {
  return baseURL.replace(VERSIONED_PATH, '/api/v2');
}

/** The v2 health check URL of the server, or undefined when it can't be derived. */
export function apiV2HealthURL(baseURL: string = apiBaseURL()): string | undefined {
  if (!VERSIONED_PATH.test(baseURL)) {
    return undefined;
  }
  return baseURL.replace(VERSIONED_PATH, '/api/v2/healthz');
}

let v2Support: Promise<boolean> | undefined;

/**
 * Reports whether the server serves API v2. Only a 404 from the v2 health
 * check means it does not. A server that can't be reached fails the run
 * rather than skipping the v2 tests.
 */
export function serverSupportsV2(): Promise<boolean> {
  if (!v2Support) {
    v2Support = probeV2();
  }
  return v2Support;
}

async function probeV2(): Promise<boolean> {
  const url = apiV2HealthURL();
  if (!url) {
    return true;
  }
  const res = await fetch(url);
  await res.body?.cancel();
  if (process.env.DEBUG_API_REQUESTS === 'true') {
    console.log(`[api-version] GET ${url} -> ${res.status}`);
  }
  return res.status !== 404;
}

/** Skips the calling test, or every test of the calling hook's suite, unless the server serves API v2. */
export async function skipUnlessV2(ctx: Context): Promise<void> {
  if (!(await serverSupportsV2())) {
    ctx.skip();
  }
}
