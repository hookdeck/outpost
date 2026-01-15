import { describe, it, before, after } from 'mocha';
import { expect } from 'chai';
import { SdkClient, createSdkClient } from '../../utils/sdk-client';
import { createHookdeckDestination } from '../../factories/destination.factory';
/* eslint-disable no-console */
/* eslint-disable no-undef */

// Get configured test topics from environment (required)
if (!process.env.TEST_TOPICS) {
  throw new Error('TEST_TOPICS environment variable is required. Please set it in .env file.');
}
const TEST_TOPICS = process.env.TEST_TOPICS.split(',').map((t) => t.trim());

describe('Hookdeck Destinations - Contract Tests (SDK-based validation)', () => {
  let client: SdkClient;

  before(async () => {
    client = createSdkClient();

    // Create tenant if it doesn't exist (idempotent operation)
    try {
      await client.upsertTenant();
    } catch (error) {
      console.warn('Failed to create tenant (may already exist):', error);
    }
  });

  after(async () => {
    // Cleanup: delete all destinations for the test tenant
    try {
      const destinations = await client.listDestinations();
      console.log(`Cleaning up ${destinations.length} destinations...`);

      for (const destination of destinations) {
        try {
          await client.deleteDestination(destination.id);
          console.log(`Deleted destination: ${destination.id}`);
        } catch (error) {
          console.warn(`Failed to delete destination ${destination.id}:`, error);
        }
      }

      console.log('All destinations cleaned up');
    } catch (error) {
      console.warn('Failed to list destinations for cleanup:', error);
    }

    // Cleanup: delete the test tenant
    try {
      await client.deleteTenant();
      console.log('Test tenant deleted');
    } catch (error) {
      console.warn('Failed to delete tenant:', error);
    }
  });

  describe('POST /api/v1/tenants/{tenant_id}/destinations - Create Hookdeck Destination', () => {
    // TODO: Re-enable these tests once backend supports test mode without external API verification
    // Issue: Backend calls external Hookdeck API to verify tokens during destination creation
    // See: internal/destregistry/providers/desthookdeck/desthookdeck.go:243
    it.skip('should create a Hookdeck destination with valid config', async () => {
      const destinationData = createHookdeckDestination();
      const destination = await client.createDestination(destinationData);

      expect(destination.type).to.equal('hookdeck');
    });

    it.skip('should create a Hookdeck destination with array of topics', async () => {
      const destinationData = createHookdeckDestination({
        topics: TEST_TOPICS,
      });
      const destination = await client.createDestination(destinationData);

      expect(destination.topics).to.have.lengthOf(TEST_TOPICS.length);
      TEST_TOPICS.forEach((topic) => {
        expect(destination.topics).to.include(topic);
      });

      // Cleanup
      await client.deleteDestination(destination.id);
    });

    it.skip('should create destination with user-provided ID', async () => {
      const customId = `custom-hookdeck-${Date.now()}`;
      const destinationData = createHookdeckDestination({
        id: customId,
      });
      const destination = await client.createDestination(destinationData);

      expect(destination.id).to.equal(customId);

      // Cleanup
      await client.deleteDestination(destination.id);
    });

    it('should reject creation with missing credentials', async () => {
      let errorThrown = false;
      try {
        await client.createDestination({
          type: 'hookdeck',
          topics: '*',
          config: {},
          // Missing credentials
        } as any);
      } catch (error: any) {
        errorThrown = true;
        expect(error).to.exist;
        if (error.response) {
          expect(error.response.status).to.be.oneOf([400, 422]);
        } else {
          expect(error.message).to.exist;
        }
      }
      if (!errorThrown) {
        expect.fail('Should have thrown an error');
      }
    });

    it('should reject creation with missing type field', async () => {
      let errorThrown = false;
      try {
        await client.createDestination({
          topics: '*',
          config: {},
          credentials: {
            token: 'hk_12345',
          },
        } as any);
      } catch (error: any) {
        errorThrown = true;
        expect(error).to.exist;
        if (error.response) {
          expect(error.response.status).to.be.oneOf([400, 422]);
        } else {
          expect(error.message).to.exist;
        }
      }
      if (!errorThrown) {
        expect.fail('Should have thrown an error');
      }
    });

    it('should reject creation with empty topics', async () => {
      let errorThrown = false;
      try {
        const destinationData = createHookdeckDestination({
          topics: [],
        });
        await client.createDestination(destinationData);
      } catch (error: any) {
        errorThrown = true;
        expect(error).to.exist;
        if (error.response) {
          expect(error.response.status).to.be.oneOf([400, 422]);
        } else {
          expect(error.message).to.exist;
        }
      }
      if (!errorThrown) {
        expect.fail('Should have thrown an error');
      }
    });
  });

  // TODO: Re-enable these tests once backend supports test mode without external API verification
  // Issue: Backend calls external Hookdeck API to verify tokens during destination creation
  describe.skip('GET /api/v1/tenants/{tenant_id}/destinations/{id} - Retrieve Hookdeck Destination', () => {
    let destinationId: string;

    before(async () => {
      const destinationData = createHookdeckDestination();
      const destination = await client.createDestination(destinationData);
      destinationId = destination.id;
    });

    after(async () => {
      try {
        await client.deleteDestination(destinationId);
      } catch (error) {
        console.warn('Failed to cleanup destination:', error);
      }
    });

    it('should retrieve an existing Hookdeck destination', async () => {
      const destination = await client.getDestination(destinationId);

      expect(destination.id).to.equal(destinationId);
      expect(destination.type).to.equal('hookdeck');
    });

    it('should return 404 for non-existent destination', async () => {
      let errorThrown = false;
      try {
        await client.getDestination('non-existent-id-12345');
      } catch (error: any) {
        errorThrown = true;
        expect(error).to.exist;
        if (error.response) {
          expect(error.response.status).to.equal(404);
        } else {
          expect(error.message).to.exist;
        }
      }
      if (!errorThrown) {
        expect.fail('Should have thrown an error');
      }
    });
  });

  // TODO: Re-enable these tests once backend supports test mode without external API verification
  // Issue: Backend calls external Hookdeck API to verify tokens during destination creation
  describe.skip('GET /api/v1/tenants/{tenant_id}/destinations - List Hookdeck Destinations', () => {
    before(async () => {
      // Create multiple Hookdeck destinations for listing
      await client.createDestination(createHookdeckDestination());
      await client.createDestination(
        createHookdeckDestination({
          topics: [TEST_TOPICS[0]],
        })
      );
    });

    it('should list all destinations', async () => {
      const destinations = await client.listDestinations();

      expect(destinations.length).to.be.greaterThan(0);
    });

    it('should filter destinations by type', async () => {
      const destinations = await client.listDestinations({ type: 'hookdeck' });

      destinations.forEach((dest) => {
        expect(dest.type).to.equal('hookdeck');
      });
    });
  });

  // TODO: Re-enable these tests once backend supports test mode without external API verification
  // Issue: Backend calls external Hookdeck API to verify tokens during destination creation
  describe.skip('PATCH /api/v1/tenants/{tenant_id}/destinations/{id} - Update Hookdeck Destination', () => {
    let destinationId: string;

    before(async () => {
      const destinationData = createHookdeckDestination();
      const destination = await client.createDestination(destinationData);
      destinationId = destination.id;
    });

    after(async () => {
      try {
        await client.deleteDestination(destinationId);
      } catch (error) {
        console.warn('Failed to cleanup destination:', error);
      }
    });

    it('should update destination topics', async () => {
      const updated = await client.updateDestination(destinationId, {
        topics: ['user.created', 'user.updated'],
      });

      expect(updated.id).to.equal(destinationId);
      expect(updated.type).to.equal('hookdeck');
      expect(updated.topics).to.include('user.created');
      expect(updated.topics).to.include('user.updated');
    });

    it('should update destination credentials', async () => {
      const updated = await client.updateDestination(destinationId, {
        credentials: {
          token: 'hk_updated_token',
        },
      });

      expect(updated.id).to.equal(destinationId);
    });

    it('should return 404 for updating non-existent destination', async () => {
      let errorThrown = false;
      try {
        await client.updateDestination('non-existent-id-12345', {
          topics: ['test'],
        });
      } catch (error: any) {
        errorThrown = true;
        expect(error).to.exist;
        if (error.response) {
          expect(error.response.status).to.equal(404);
        } else {
          expect(error.message).to.exist;
        }
      }
      if (!errorThrown) {
        expect.fail('Should have thrown an error');
      }
    });
  });

  // TODO: Re-enable these tests once backend supports test mode without external API verification
  // Issue: Backend calls external Hookdeck API to verify tokens during destination creation
  describe.skip('DELETE /api/v1/tenants/{tenant_id}/destinations/{id} - Delete Hookdeck Destination', () => {
    it('should delete an existing destination', async () => {
      const destinationData = createHookdeckDestination();
      const destination = await client.createDestination(destinationData);

      await client.deleteDestination(destination.id);

      // Verify deletion by trying to get the destination
      let errorThrown = false;
      try {
        await client.getDestination(destination.id);
      } catch (error: any) {
        errorThrown = true;
        expect(error).to.exist;
      }
      expect(errorThrown).to.be.true;
    });

    it('should return 404 for deleting non-existent destination', async () => {
      let errorThrown = false;
      try {
        await client.deleteDestination('non-existent-id-12345');
      } catch (error: any) {
        errorThrown = true;
        expect(error).to.exist;
        if (error.response) {
          expect(error.response.status).to.equal(404);
        } else {
          expect(error.message).to.exist;
        }
      }
      if (!errorThrown) {
        expect.fail('Should have thrown an error');
      }
    });
  });
});
