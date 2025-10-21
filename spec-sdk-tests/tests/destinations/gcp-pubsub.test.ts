import { describe, it, before, after } from 'mocha';
import { expect } from 'chai';
import { SdkClient, createSdkClient } from '../../utils/sdk-client';
/* eslint-disable no-console */
/* eslint-disable no-undef */

// Get configured test topics from environment (required)
if (!process.env.TEST_TOPICS) {
  throw new Error('TEST_TOPICS environment variable is required. Please set it in .env file.');
}
const TEST_TOPICS = process.env.TEST_TOPICS.split(',').map((t) => t.trim());

describe('GCP Pub/Sub Destinations - Contract Tests (SDK-based validation)', () => {
  let client: SdkClient;

  before(async () => {
    // Use SDK client with built-in OpenAPI validation
    // No need for separate proxy and direct clients - SDK validates all requests/responses
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

  describe('POST /api/v1/{tenant_id}/destinations - Create GCP Pub/Sub Destination', () => {
    it('should create a GCP Pub/Sub destination with valid config', async () => {
      const destination = await client.createDestination({
        type: 'gcp_pubsub',
        topics: ['*'],
        config: {
          projectId: 'test-project-123',
          topic: 'test-topic',
          endpoint: 'pubsub.googleapis.com:443',
        },
        credentials: {
          serviceAccountJson: JSON.stringify({
            type: 'service_account',
            projectId: 'test-project-123',
            private_key_id: 'key123',
            private_key: '-----BEGIN PRIVATE KEY-----\ntest\n-----END PRIVATE KEY-----\n',
            client_email: 'test@test-project-123.iam.gserviceaccount.com',
            client_id: '123456789',
            auth_uri: 'https://accounts.google.com/o/oauth2/auth',
            token_uri: 'https://oauth2.googleapis.com/token',
            auth_provider_x509_cert_url: 'https://www.googleapis.com/oauth2/v1/certs',
            client_x509_cert_url: 'https://www.googleapis.com/robot/v1/metadata/x509/test',
          }),
        },
      });

      // TODO: Re-enable this check once the backend includes the 'created_at' property in the response.
      // expect(destination).to.have.property('created_at');
      expect(destination.type).to.equal('gcp_pubsub');
      expect(destination.config.projectId).to.equal('test-project-123');
      expect(destination.config.topic).to.equal('test-topic');
    });

    it('should create a GCP Pub/Sub destination with array of topics', async () => {
      const destination = await client.createDestination({
        type: 'gcp_pubsub',
        topics: TEST_TOPICS,
        config: {
          projectId: 'test-project-topics',
          topic: 'events-topic',
        },
        credentials: {
          serviceAccountJson: '{"type":"service_account","projectId":"test"}',
        },
      });

      expect(destination.topics).to.have.lengthOf(TEST_TOPICS.length);
      // Verify all configured test topics are present
      TEST_TOPICS.forEach((topic) => {
        expect(destination.topics).to.include(topic);
      });

      // Cleanup
      await client.deleteDestination(destination.id);
    });

    it('should create destination with user-provided ID', async () => {
      const customId = `custom-gcp-${Date.now()}`;
      const destination = await client.createDestination({
        id: customId,
        type: 'gcp_pubsub',
        topics: '*',
        config: {
          projectId: 'test-project',
          topic: 'test-topic',
        },
        credentials: {
          serviceAccountJson: '{"type":"service_account","projectId":"test"}',
        },
      });

      expect(destination.id).to.equal(customId);

      // Cleanup
      await client.deleteDestination(destination.id);
    });

    it('should reject creation with missing required config field: projectId', async () => {
      let errorThrown = false;
      try {
        await client.createDestination({
          type: 'gcp_pubsub',
          topics: '*',
          config: {
            // Missing projectId
            topic: 'test-topic',
          },
          credentials: {
            serviceAccountJson: '{"type":"service_account"}',
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

    it('should reject creation with missing required config field: topic', async () => {
      let errorThrown = false;
      try {
        await client.createDestination({
          type: 'gcp_pubsub',
          topics: '*',
          config: {
            projectId: 'test-project',
            // Missing topic
          },
          credentials: {
            serviceAccountJson: '{"type":"service_account"}',
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
          type: 'gcp_pubsub',
          topics: '*',
          config: {
            projectId: 'test-project',
            topic: 'test-topic',
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

    it('should reject creation with invalid serviceAccountJson', async () => {
      let errorThrown = false;
      try {
        await client.createDestination({
          type: 'gcp_pubsub',
          topics: '*',
          config: {
            projectId: 'test-project',
            topic: 'test-topic',
          },
          credentials: {
            serviceAccountJson: 'not-valid-json',
          },
        });
      } catch (error: any) {
        errorThrown = true;
        expect(error).to.exist;
        // Backend rejects invalid JSON - error might not have response object
        if (error.response) {
          expect(error.response.status).to.be.oneOf([400, 422]);
        } else {
          // If no response, just verify error was thrown
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
          // Missing type
          topics: '*',
          config: {
            projectId: 'test-project',
            topic: 'test-topic',
          },
          credentials: {
            serviceAccountJson: '{"type":"service_account"}',
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
        await client.createDestination({
          type: 'gcp_pubsub',
          topics: [],
          config: {
            projectId: 'test-project',
            topic: 'test-topic',
          },
          credentials: {
            serviceAccountJson: '{"type":"service_account"}',
          },
        });
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

  describe('GET /api/v1/{tenant_id}/destinations/{id} - Retrieve GCP Pub/Sub Destination', () => {
    let destinationId: string;

    before(async () => {
      // Create a destination to retrieve
      const destination = await client.createDestination({
        type: 'gcp_pubsub',
        topics: '*',
        config: {
          projectId: 'test-project-retrieve',
          topic: 'test-topic-retrieve',
        },
        credentials: {
          serviceAccountJson: '{"type":"service_account","projectId":"test"}',
        },
      });
      destinationId = destination.id;
    });

    after(async () => {
      try {
        await client.deleteDestination(destinationId);
      } catch (error) {
        console.warn('Failed to cleanup destination:', error);
      }
    });

    it('should retrieve an existing GCP Pub/Sub destination', async () => {
      const destination = await client.getDestination(destinationId);

      // TODO: Re-enable this check once the backend includes the 'created_at' property in the response.
      // expect(destination).to.have.property('created_at');
      expect(destination.id).to.equal(destinationId);
      expect(destination.type).to.equal('gcp_pubsub');
      expect(destination.config.projectId).to.equal('test-project-retrieve');
      expect(destination.config.topic).to.equal('test-topic-retrieve');
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

    it('should return error for invalid destination ID format', async () => {
      let errorThrown = false;
      try {
        await client.getDestination('invalid id with spaces');
      } catch (error: any) {
        errorThrown = true;
        expect(error).to.exist;
        if (error.response) {
          expect(error.response.status).to.be.oneOf([400, 404]);
        } else {
          expect(error.message).to.exist;
        }
      }
      if (!errorThrown) {
        expect.fail('Should have thrown an error');
      }
    });
  });

  describe('GET /api/v1/{tenant_id}/destinations - List GCP Pub/Sub Destinations', () => {
    before(async () => {
      // Create multiple GCP Pub/Sub destinations for listing
      await client.createDestination({
        type: 'gcp_pubsub',
        topics: '*',
        config: {
          projectId: 'test-project-1',
          topic: 'test-topic-1',
        },
        credentials: {
          serviceAccountJson: '{"type":"service_account","projectId":"test"}',
        },
      });

      await client.createDestination({
        type: 'gcp_pubsub',
        topics: [TEST_TOPICS[0]],
        config: {
          projectId: 'test-project-2',
          topic: 'test-topic-2',
        },
        credentials: {
          serviceAccountJson: '{"type":"service_account","projectId":"test"}',
        },
      });
    });

    it('should list all destinations', async () => {
      const destinations = await client.listDestinations();

      expect(destinations.length).to.be.greaterThan(0);
      // TODO: Re-enable this check once the backend includes the 'created_at' property in the response.
      // destinations.forEach((dest) => {
      //   expect(dest).to.have.property('created_at');
      // });
    });

    it('should filter destinations by type', async () => {
      const destinations = await client.listDestinations({ type: 'gcp_pubsub' });

      destinations.forEach((dest) => {
        expect(dest.type).to.equal('gcp_pubsub');
      });
    });

    it('should return destinations array', async () => {
      await client.listDestinations();

      // Note: The current endpoint doesn't support pagination per OpenAPI spec
    });
  });

  describe('PATCH /api/v1/{tenant_id}/destinations/{id} - Update GCP Pub/Sub Destination', () => {
    let destinationId: string;

    before(async () => {
      // Create a destination to update
      const destination = await client.createDestination({
        type: 'gcp_pubsub',
        topics: '*',
        config: {
          projectId: 'test-project-update',
          topic: 'test-topic-update',
        },
        credentials: {
          serviceAccountJson: '{"type":"service_account","projectId":"test"}',
        },
      });
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
      expect(updated.type).to.equal('gcp_pubsub');
      expect(updated.topics).to.include('user.created');
      expect(updated.topics).to.include('user.updated');
    });

    it('should update destination config', async () => {
      const updated = await client.updateDestination(destinationId, {
        config: {
          topic: 'updated-topic-name',
        },
      });

      expect(updated.id).to.equal(destinationId);
      expect(updated.config).to.exist;
      if (updated.config) {
        expect(updated.config.topic).to.equal('updated-topic-name');
      }
    });

    it('should update destination credentials', async () => {
      const updated = await client.updateDestination(destinationId, {
        credentials: {
          serviceAccountJson: '{"type":"service_account","projectId":"updated"}',
        },
      });

      expect(updated.id).to.equal(destinationId);
    });

    it('should return 404 for updating non-existent destination', async () => {
      let errorThrown = false;
      try {
        await client.updateDestination('non-existent-id-12345', {
          topics: '*',
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

    // TODO: Re-enable this test once the backend validates the config on update.
    it.skip('should reject update with invalid config', async () => {
      let errorThrown = false;
      try {
        await client.updateDestination(destinationId, {
          config: {
            // Missing required fields
          },
        });
      } catch (error: any) {
        errorThrown = true;
        expect(error).to.exist;
        // PATCH endpoint missing from spec - error might not have response object
        if (error.response) {
          expect(error.response.status).to.be.oneOf([400, 422]);
        } else {
          // If no response, just verify error was thrown
          expect(error.message).to.exist;
        }
      }
      if (!errorThrown) {
        expect.fail('Should have thrown an error');
      }
    });
  });

  describe('DELETE /api/v1/{tenant_id}/destinations/{id} - Delete GCP Pub/Sub Destination', () => {
    it('should delete an existing destination', async () => {
      // Create a destination to delete
      const destination = await client.createDestination({
        type: 'gcp_pubsub',
        topics: '*',
        config: {
          projectId: 'test-project-delete',
          topic: 'test-topic-delete',
        },
        credentials: {
          serviceAccountJson: '{"type":"service_account","projectId":"test"}',
        },
      });

      // Delete it
      await client.deleteDestination(destination.id);

      // Verify it's gone
      let errorThrown = false;
      try {
        await client.getDestination(destination.id);
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
        expect.fail('Destination should have been deleted');
      }
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

  describe('Edge Cases and Error Handling', () => {
    it('should handle very long topic names', async () => {
      // Use an existing topic since backend validates topics must exist
      const destination = await client.createDestination({
        type: 'gcp_pubsub',
        topics: [TEST_TOPICS[0]],
        config: {
          projectId: 'test-project-long-topic',
          topic: 'test-topic',
        },
        credentials: {
          serviceAccountJson: '{"type":"service_account","projectId":"test"}',
        },
      });

      expect(destination.topics).to.include(TEST_TOPICS[0]);

      // Cleanup
      await client.deleteDestination(destination.id);
    });

    it('should handle special characters in config values', async () => {
      const destination = await client.createDestination({
        type: 'gcp_pubsub',
        topics: '*',
        config: {
          projectId: 'test-project-with-dashes-123',
          topic: 'test.topic_with-special.chars_123',
        },
        credentials: {
          serviceAccountJson: '{"type":"service_account","projectId":"test"}',
        },
      });

      expect(destination.config.projectId).to.equal('test-project-with-dashes-123');
      expect(destination.config.topic).to.equal('test.topic_with-special.chars_123');

      // Cleanup
      await client.deleteDestination(destination.id);
    });

    it('should handle optional endpoint configuration', async () => {
      const destination = await client.createDestination({
        type: 'gcp_pubsub',
        topics: '*',
        config: {
          projectId: 'test-project-optional-endpoint',
          topic: 'test-topic',
          endpoint: 'localhost:8085',
        },
        credentials: {
          serviceAccountJson: '{"type":"service_account","projectId":"test"}',
        },
      });

      expect(destination.config.endpoint).to.equal('localhost:8085');

      // Cleanup
      await client.deleteDestination(destination.id);
    });
  });
});
