import { describe, it } from 'mocha';
import { expect } from 'chai';
import { Outpost } from '../../sdks/outpost-typescript/dist/commonjs';

const EXPECTED_DEFAULT_SERVER_URL = 'https://api.outpost.hookdeck.com/2025-07-01';

describe('SDK defaults', () => {
  it('new Outpost instance defaults to expected server URL when no serverURL or serverIdx override', () => {
    // Minimal options: no serverURL, no serverIdx (uses first server from ServerList)
    const client = new Outpost({});
    expect(client._baseURL).to.exist;
    const baseURL = client._baseURL!.href;
    // Trailing slash may be added by SDK; normalize for comparison
    const normalized = baseURL.replace(/\/+$/, '') + '/';
    expect(normalized).to.equal(EXPECTED_DEFAULT_SERVER_URL + '/');
  });
});
