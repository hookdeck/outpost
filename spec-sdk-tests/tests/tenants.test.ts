import { describe, it } from 'mocha';
import { expect } from 'chai';
import { createSdkClient } from '../utils/sdk-client';

/**
 * Tenant list tests.
 *
 * API/SDK: GET /tenants with cursor-based pagination. The SDK exposes
 * sdk.tenants.list(request) (v0.14+) with a single request object, consistent with
 * events.list and attempts.list. Requires Admin API Key (or Tenant JWT for single-tenant result).
 * Requires Redis with RediSearch; may return 501 if not available.
 */

describe('Tenants - List with request object', () => {
  it('should list tenants using tenants.list(request)', async function () {
    const client = createSdkClient();
    const sdk = client.getSDK();

    const page = await sdk.tenants.list({ limit: 5 });

    expect(page).to.not.be.undefined;
    expect(page.result.models).to.be.an('array');
    (page.result.models ?? []).forEach((t: { id?: string }, i: number) => {
      expect(t, `tenant[${i}]`).to.be.an('object');
      if (t.id != null) expect(t.id, `tenant[${i}].id`).to.be.a('string');
    });
  });
});
