import { describe, it, before, after } from 'mocha';
import { expect } from 'chai';
import { SdkClient, createSdkClient } from '../../utils/sdk-client';
import { createWebhookDestination } from '../../factories/destination.factory';
/* eslint-disable no-console */
/* eslint-disable no-undef */

describe('Webhook Destinations - Filter/Metadata/DeliveryMetadata Removal (SDK)', () => {
  let client: SdkClient;
  let destinationId: string;

  before(async () => {
    client = createSdkClient();
    try {
      await client.upsertTenant();
    } catch (error) {
      console.warn('Failed to create tenant (may already exist):', error);
    }
  });

  after(async () => {
    try {
      if (destinationId) {
        await client.deleteDestination(destinationId);
      }
    } catch (error) {
      console.warn('Failed to cleanup:', error);
    }
    try {
      await client.deleteTenant();
    } catch (error) {
      console.warn('Failed to delete tenant:', error);
    }
  });

  describe('Filter removal', () => {
    before(async () => {
      const dest = await client.createDestination(
        createWebhookDestination({
          filter: {
            body: { user_id: 'usr_123' },
          },
        })
      );
      destinationId = dest.id;
    });

    it('should have filter set after creation', async () => {
      const dest = await client.getDestination(destinationId);
      expect(dest.filter).to.not.be.null;
      expect(dest.filter).to.deep.include({ body: { user_id: 'usr_123' } });
    });

    it('should clear filter when set to empty object {}', async () => {
      const updated = await client.updateDestination(destinationId, {
        filter: {},
      });
      const isEmpty =
        updated.filter === null ||
        updated.filter === undefined ||
        (typeof updated.filter === 'object' && Object.keys(updated.filter).length === 0);
      expect(isEmpty).to.be.true;
    });

    it('should not change filter when field is omitted', async () => {
      await client.updateDestination(destinationId, {
        filter: { body: { user_id: 'usr_456' } },
      });
      const updated = await client.updateDestination(destinationId, {
        topics: ['user.created', 'user.updated'],
      });
      expect(updated.filter).to.deep.include({ body: { user_id: 'usr_456' } });
    });
  });

  describe('Metadata removal', () => {
    before(async () => {
      try {
        if (destinationId) await client.deleteDestination(destinationId);
      } catch {}
      const dest = await client.createDestination(
        createWebhookDestination({
          metadata: {
            env: 'production',
            team: 'platform',
            region: 'us-east-1',
          },
        })
      );
      destinationId = dest.id;
    });

    it('should have metadata set after creation', async () => {
      const dest = await client.getDestination(destinationId);
      expect(dest.metadata).to.deep.equal({
        env: 'production',
        team: 'platform',
        region: 'us-east-1',
      });
    });

    it('should remove a single metadata key when sending subset', async () => {
      const updated = await client.updateDestination(destinationId, {
        metadata: {
          env: 'production',
          team: 'platform',
        },
      });
      expect(updated.metadata).to.deep.equal({
        env: 'production',
        team: 'platform',
      });
      expect(updated.metadata).to.not.have.property('region');
    });

    it('should clear all metadata when set to empty object {}', async () => {
      const updated = await client.updateDestination(destinationId, {
        metadata: {},
      });
      const isEmpty =
        updated.metadata === null ||
        updated.metadata === undefined ||
        (typeof updated.metadata === 'object' && Object.keys(updated.metadata).length === 0);
      expect(isEmpty).to.be.true;
    });

    it('should not change metadata when field is omitted', async () => {
      await client.updateDestination(destinationId, {
        metadata: { env: 'staging' },
      });
      const updated = await client.updateDestination(destinationId, {
        topics: ['user.created', 'user.updated'],
      });
      expect(updated.metadata).to.deep.equal({ env: 'staging' });
    });
  });

  describe('DeliveryMetadata removal', () => {
    before(async () => {
      try {
        if (destinationId) await client.deleteDestination(destinationId);
      } catch {}
      const dest = await client.createDestination(
        createWebhookDestination({
          deliveryMetadata: {
            source: 'outpost',
            version: '1.0',
          },
        })
      );
      destinationId = dest.id;
    });

    it('should have delivery_metadata set after creation', async () => {
      const dest = await client.getDestination(destinationId);
      expect(dest.deliveryMetadata).to.deep.equal({
        source: 'outpost',
        version: '1.0',
      });
    });

    it('should remove a single delivery_metadata key when sending subset', async () => {
      const updated = await client.updateDestination(destinationId, {
        deliveryMetadata: {
          source: 'outpost',
        },
      });
      expect(updated.deliveryMetadata).to.deep.equal({
        source: 'outpost',
      });
      expect(updated.deliveryMetadata).to.not.have.property('version');
    });

    it('should clear all delivery_metadata when set to empty object {}', async () => {
      const updated = await client.updateDestination(destinationId, {
        deliveryMetadata: {},
      });
      const isEmpty =
        updated.deliveryMetadata === null ||
        updated.deliveryMetadata === undefined ||
        (typeof updated.deliveryMetadata === 'object' &&
          Object.keys(updated.deliveryMetadata).length === 0);
      expect(isEmpty).to.be.true;
    });

    it('should not change delivery_metadata when field is omitted', async () => {
      await client.updateDestination(destinationId, {
        deliveryMetadata: { source: 'test' },
      });
      const updated = await client.updateDestination(destinationId, {
        topics: ['user.created', 'user.updated'],
      });
      expect(updated.deliveryMetadata).to.deep.equal({ source: 'test' });
    });
  });
});
