import { describe, it, before, after } from 'mocha';
import { expect } from 'chai';
import { SdkClient, createSdkClient } from '../../utils/sdk-client';
import { createWebhookDestination } from '../../factories/destination.factory';
/* eslint-disable no-console */
/* eslint-disable no-undef */

describe('Webhook Destinations - Merge-Patch Semantics (RFC 7396)', () => {
  let client: SdkClient;
  const createdDestinations: string[] = [];

  before(async () => {
    client = createSdkClient();
    try {
      await client.upsertTenant();
    } catch (error) {
      console.warn('Failed to create tenant (may already exist):', error);
    }
  });

  after(async () => {
    for (const id of createdDestinations) {
      try {
        await client.deleteDestination(id);
      } catch {}
    }
    try {
      await client.deleteTenant();
    } catch {}
  });

  async function createDest(overrides?: Record<string, any>): Promise<string> {
    const dest = await client.createDestination(createWebhookDestination(overrides));
    createdDestinations.push(dest.id);
    return dest.id;
  }

  // ── metadata merge-patch ──

  describe('metadata merge-patch', () => {
    it('should add key while preserving existing', async () => {
      const id = await createDest({ metadata: { env: 'prod' } });

      const updated = await client.updateDestination(id, {
        metadata: { env: 'prod', team: 'platform' },
      });

      expect(updated.metadata).to.deep.equal({ env: 'prod', team: 'platform' });
    });

    it('should update existing key', async () => {
      const id = await createDest({ metadata: { env: 'prod' } });

      const updated = await client.updateDestination(id, {
        metadata: { env: 'staging' },
      });

      expect(updated.metadata).to.deep.include({ env: 'staging' });
    });

    it('should delete key via null value', async () => {
      const id = await createDest({ metadata: { env: 'prod', region: 'us-east-1' } });

      const updated = await client.updateDestination(id, {
        metadata: { env: 'prod', region: null },
      });

      expect(updated.metadata).to.deep.equal({ env: 'prod' });
      expect(updated.metadata).to.not.have.property('region');
    });

    it('should clear entire field via null', async () => {
      const id = await createDest({ metadata: { env: 'prod' } });

      const updated = await client.updateDestination(id, {
        metadata: null,
      });

      const isEmpty =
        updated.metadata === null ||
        updated.metadata === undefined ||
        (typeof updated.metadata === 'object' && Object.keys(updated.metadata).length === 0);
      expect(isEmpty).to.be.true;
    });

    it('should not change when empty object sent', async () => {
      const id = await createDest({ metadata: { env: 'prod' } });

      const updated = await client.updateDestination(id, {
        metadata: {},
      });

      expect(updated.metadata).to.deep.equal({ env: 'prod' });
    });

    it('should not change when field omitted', async () => {
      const id = await createDest({ metadata: { env: 'prod' } });

      const updated = await client.updateDestination(id, {
        topics: ['*'],
      });

      expect(updated.metadata).to.deep.equal({ env: 'prod' });
    });

    it('should handle mixed add/update/delete', async () => {
      const id = await createDest({
        metadata: { keep: 'v', remove: 'v', update: 'old' },
      });

      const updated = await client.updateDestination(id, {
        metadata: { keep: 'v', remove: null, update: 'new', add: 'v' },
      });

      expect(updated.metadata).to.deep.equal({ keep: 'v', update: 'new', add: 'v' });
      expect(updated.metadata).to.not.have.property('remove');
    });
  });

  // ── delivery_metadata merge-patch ──

  describe('delivery_metadata merge-patch', () => {
    it('should add key while preserving existing', async () => {
      const id = await createDest({ deliveryMetadata: { source: 'outpost' } });

      const updated = await client.updateDestination(id, {
        deliveryMetadata: { source: 'outpost', version: '1.0' },
      });

      expect(updated.deliveryMetadata).to.deep.equal({ source: 'outpost', version: '1.0' });
    });

    it('should delete key via null value', async () => {
      const id = await createDest({
        deliveryMetadata: { source: 'outpost', version: '1.0' },
      });

      const updated = await client.updateDestination(id, {
        deliveryMetadata: { source: 'outpost', version: null },
      });

      expect(updated.deliveryMetadata).to.deep.equal({ source: 'outpost' });
    });

    it('should clear entire field via null', async () => {
      const id = await createDest({ deliveryMetadata: { source: 'outpost' } });

      const updated = await client.updateDestination(id, {
        deliveryMetadata: null,
      });

      const isEmpty =
        updated.deliveryMetadata === null ||
        updated.deliveryMetadata === undefined ||
        (typeof updated.deliveryMetadata === 'object' &&
          Object.keys(updated.deliveryMetadata).length === 0);
      expect(isEmpty).to.be.true;
    });

    it('should not change when empty object sent', async () => {
      const id = await createDest({ deliveryMetadata: { source: 'outpost' } });

      const updated = await client.updateDestination(id, {
        deliveryMetadata: {},
      });

      expect(updated.deliveryMetadata).to.deep.equal({ source: 'outpost' });
    });
  });

  // ── filter replacement ──

  describe('filter replacement', () => {
    it('should replace filter entirely', async () => {
      const id = await createDest({
        filter: { body: { user_id: 'usr_123' } },
      });

      const updated = await client.updateDestination(id, {
        filter: { body: { status: 'active' } },
      });

      expect(updated.filter).to.deep.equal({ body: { status: 'active' } });
    });

    it('should clear filter with empty object', async () => {
      const id = await createDest({
        filter: { body: { user_id: 'usr_123' } },
      });

      const updated = await client.updateDestination(id, {
        filter: {},
      });

      const isEmpty =
        updated.filter === null ||
        updated.filter === undefined ||
        (typeof updated.filter === 'object' && Object.keys(updated.filter).length === 0);
      expect(isEmpty).to.be.true;
    });

    it('should clear filter with null', async () => {
      const id = await createDest({
        filter: { body: { user_id: 'usr_123' } },
      });

      const updated = await client.updateDestination(id, {
        filter: null,
      });

      const isEmpty =
        updated.filter === null ||
        updated.filter === undefined ||
        (typeof updated.filter === 'object' && Object.keys(updated.filter).length === 0);
      expect(isEmpty).to.be.true;
    });

    it('should not change filter when omitted', async () => {
      const id = await createDest({
        filter: { body: { user_id: 'usr_123' } },
      });

      const updated = await client.updateDestination(id, {
        topics: ['*'],
      });

      expect(updated.filter).to.deep.include({ body: { user_id: 'usr_123' } });
    });
  });
});
