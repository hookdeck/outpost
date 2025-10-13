import { config as loadEnv } from 'dotenv';
// Import from the built CommonJS distribution
import { Outpost } from '../../sdks/outpost-typescript/dist/commonjs/index';
// Import proper types from the SDK
import type {
  Destination,
  DestinationCreate,
  DestinationUpdate,
  Tenant,
} from '../../sdks/outpost-typescript/dist/commonjs/models/components';

// Load environment variables from .env file
loadEnv();

export interface SdkClientConfig {
  baseURL?: string;
  tenantId?: string;
  apiKey?: string;
  timeout?: number;
}

// Re-export SDK types for convenience
export type { Destination, DestinationCreate, DestinationUpdate, Tenant };

/**
 * Wrapper around the Speakeasy-generated SDK to provide a similar API
 * to the original api-client.ts for easier migration.
 *
 * The SDK automatically validates all requests and responses against the OpenAPI schema.
 * Validation errors are thrown as SDKValidationError or ResponseValidationError.
 */
export class SdkClient {
  private sdk: any;
  private tenantId: string;

  constructor(config: SdkClientConfig = {}) {
    const baseURL = config.baseURL || process.env.API_BASE_URL || 'http://localhost:3333';
    this.tenantId = config.tenantId || process.env.TENANT_ID || 'test-tenant';

    if (process.env.DEBUG_API_REQUESTS === 'true') {
      console.log(`[SdkClient] Creating SDK client with baseURL: ${baseURL}`);
    }

    this.sdk = new Outpost({
      serverURL: baseURL,
      tenantId: this.tenantId,
      security: {
        adminApiKey: config.apiKey || process.env.API_KEY || '',
      },
      timeoutMs: config.timeout || 10000,
    });
  }

  /**
   * Create or update a tenant (idempotent)
   */
  async upsertTenant(data?: { id?: string; name?: string }): Promise<Tenant> {
    // Note: The upsert endpoint only takes tenantId, no body
    return await this.sdk.tenants.upsert({
      tenantId: data?.id || this.tenantId,
    });
  }

  /**
   * Delete a tenant
   */
  async deleteTenant(tenantId?: string): Promise<void> {
    await this.sdk.tenants.delete({
      tenantId: tenantId || this.tenantId,
    });
  }

  /**
   * Create a new destination
   */
  async createDestination(data: DestinationCreate): Promise<Destination> {
    return await this.sdk.destinations.create({
      tenantId: this.tenantId,
      destinationCreate: data,
    });
  }

  /**
   * Get a destination by ID
   */
  async getDestination(destinationId: string, tenantId?: string): Promise<Destination> {
    return await this.sdk.destinations.get({
      tenantId: tenantId || this.tenantId,
      destinationId,
    });
  }

  /**
   * List all destinations
   */
  async listDestinations(params?: { type?: string }): Promise<Destination[]> {
    return await this.sdk.destinations.list({
      tenantId: this.tenantId,
      type: params?.type,
    });
  }

  /**
   * Update a destination
   */
  async updateDestination(
    destinationId: string,
    data: DestinationUpdate,
    tenantId?: string
  ): Promise<Destination> {
    // The update endpoint returns a Destination directly
    return await this.sdk.destinations.update({
      tenantId: tenantId || this.tenantId,
      destinationId,
      destinationUpdate: data,
    });
  }

  /**
   * Delete a destination
   */
  async deleteDestination(destinationId: string, tenantId?: string): Promise<void> {
    await this.sdk.destinations.delete({
      tenantId: tenantId || this.tenantId,
      destinationId,
    });
  }

  /**
   * Get the current tenant ID
   */
  getTenantId(): string {
    return this.tenantId;
  }

  /**
   * Set a new tenant ID
   */
  setTenantId(tenantId: string): void {
    this.tenantId = tenantId;
  }

  /**
   * Get the underlying SDK instance for advanced usage
   */
  getSDK(): any {
    return this.sdk;
  }
}

/**
 * Create an SDK client (replaces both proxy and direct clients)
 * The SDK validates responses automatically, so no proxy is needed
 */
export function createSdkClient(config: SdkClientConfig = {}): SdkClient {
  return new SdkClient({
    ...config,
    baseURL: config.baseURL || process.env.API_BASE_URL || 'http://localhost:3333',
    apiKey: config.apiKey || process.env.API_KEY,
  });
}
