import { describe, it, before, after } from 'mocha';
import { expect } from 'chai';
import { SdkClient, createSdkClient } from '../../utils/sdk-client';
import { createAzureServiceBusDestination } from '../../factories/destination.factory';
/* eslint-disable no-console */
/* eslint-disable no-undef */

// Get configured test topics from environment (required)
if (!process.env.TEST_TOPICS) {
  throw new Error('TEST_TOPICS environment variable is required. Please set it in .env file.');
}
const TEST_TOPICS = process.env.TEST_TOPICS.split(',').map((t) => t.trim());

describe('Azure Service Bus Destinations - Contract Tests (SDK-based validation)', () => {
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

  describe('POST /api/v1/tenants/{tenant_id}/destinations - Create Azure Service Bus Destination', () => {
    it('should create an Azure Service Bus destination with valid config', async () => {
      const destinationData = createAzureServiceBusDestination();
      const destination = await client.createDestination(destinationData);

      expect(destination.type).to.equal('azure_servicebus');
      expect(destination.config.name).to.equal(destinationData.config.name);
    });

    it('should create an Azure Service Bus destination with array of topics', async function () {
      const sdk = client.getSDK();
      const instanceTopics = await sdk.topics.list();
      if (instanceTopics.length < 2) {
        this.skip();
        return;
      }
      const topicsToUse = instanceTopics.slice(0, 2);
      const destinationData = createAzureServiceBusDestination({
        topics: topicsToUse,
      });
      const destination = await client.createDestination(destinationData);

      expect(destination.topics).to.have.lengthOf(topicsToUse.length);
      topicsToUse.forEach((topic: string) => {
        expect(destination.topics).to.include(topic);
      });

      // Cleanup
      await client.deleteDestination(destination.id);
    });

    it('should create destination with user-provided ID', async () => {
      const customId = `custom-asb-${Date.now()}`;
      const destinationData = createAzureServiceBusDestination({
        id: customId,
      });
      const destination = await client.createDestination(destinationData);

      expect(destination.id).to.equal(customId);

      // Cleanup
      await client.deleteDestination(destination.id);
    });

    it('should reject creation with missing required config field: name', async () => {
      let errorThrown = false;
      try {
        await client.createDestination({
          type: 'azure_servicebus',
          topics: '*',
          config: {
            // Missing name
          },
          credentials: {
            connectionString:
              'Endpoint=sb://namespace.servicebus.windows.net/;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=key',
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

    it('should reject creation with missing credentials', async () => {
      let errorThrown = false;
      try {
        await client.createDestination({
          type: 'azure_servicebus',
          topics: '*',
          config: {
            name: 'my-queue',
          },
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
          config: {
            name: 'my-queue',
          },
          credentials: {
            connectionString:
              'Endpoint=sb://namespace.servicebus.windows.net/;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=key',
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
        const destinationData = createAzureServiceBusDestination({
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

  describe('GET /api/v1/tenants/{tenant_id}/destinations/{id} - Retrieve Azure Service Bus Destination', () => {
    let destinationId: string;

    before(async () => {
      const destinationData = createAzureServiceBusDestination();
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

    it('should retrieve an existing Azure Service Bus destination', async () => {
      const destination = await client.getDestination(destinationId);

      expect(destination.id).to.equal(destinationId);
      expect(destination.type).to.equal('azure_servicebus');
      expect(destination.config.name).to.exist;
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

  describe('GET /api/v1/tenants/{tenant_id}/destinations - List Azure Service Bus Destinations', () => {
    before(async () => {
      // Create multiple Azure Service Bus destinations for listing
      await client.createDestination(createAzureServiceBusDestination());
      await client.createDestination(
        createAzureServiceBusDestination({
          topics: [TEST_TOPICS[0]],
          config: {
            name: 'my-queue-2',
          },
        })
      );
    });

    it('should list all destinations', async () => {
      const destinations = await client.listDestinations();

      expect(destinations.length).to.be.greaterThan(0);
    });

    it('should filter destinations by type', async () => {
      const destinations = await client.listDestinations({ type: 'azure_servicebus' });

      destinations.forEach((dest) => {
        expect(dest.type).to.equal('azure_servicebus');
      });
    });
  });

  describe('PATCH /api/v1/tenants/{tenant_id}/destinations/{id} - Update Azure Service Bus Destination', () => {
    let destinationId: string;

    before(async () => {
      const destinationData = createAzureServiceBusDestination();
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
      expect(updated.type).to.equal('azure_servicebus');
      expect(updated.topics).to.include('user.created');
      expect(updated.topics).to.include('user.updated');
    });

    it('should update destination config', async () => {
      const updated = await client.updateDestination(destinationId, {
        config: {
          name: 'updated-queue',
        },
      });

      expect(updated.id).to.equal(destinationId);
      expect(updated.config).to.exist;
      if (updated.config) {
        expect(updated.config.name).to.equal('updated-queue');
      }
    });

    it('should update destination credentials', async () => {
      const updated = await client.updateDestination(destinationId, {
        credentials: {
          connectionString:
            'Endpoint=sb://updated.servicebus.windows.net/;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=updatedkey',
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

  describe('DELETE /api/v1/tenants/{tenant_id}/destinations/{id} - Delete Azure Service Bus Destination', () => {
    it('should delete an existing destination', async () => {
      const destinationData = createAzureServiceBusDestination();
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
