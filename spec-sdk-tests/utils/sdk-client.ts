import { config as loadEnv } from 'dotenv';
// Import from the built CommonJS distribution
import { Outpost } from '../../sdks/outpost-typescript/dist/commonjs/index';
// Import proper types from the SDK
import type {
  Destination,
  DestinationCreate,
  DestinationUpdate,
  DestinationType,
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
  private sdk: Outpost;
  private tenantId: string;

  constructor(config: SdkClientConfig = {}) {
    const baseURL = config.baseURL || process.env.API_BASE_URL || 'http://localhost:3333/api/v1';
    this.tenantId = config.tenantId || process.env.TENANT_ID || 'test-tenant';

    if (process.env.DEBUG_API_REQUESTS === 'true') {
      console.log(`[SdkClient] Creating SDK client with baseURL: ${baseURL}`);
    }

    this.sdk = new Outpost({
      serverURL: baseURL,
      apiKey: config.apiKey || process.env.API_KEY || '',
      timeoutMs: config.timeout || 10000,
    });
  }

  /**
   * Create or update a tenant (idempotent)
   */
  async upsertTenant(data?: { id?: string; name?: string }): Promise<Tenant> {
    const tenantId = data?.id || this.tenantId;
    const params = data?.name ? { metadata: { name: data.name } } : undefined;
    return await this.sdk.tenants.upsert(tenantId, params);
  }

  /**
   * Delete a tenant
   */
  async deleteTenant(tenantId?: string): Promise<void> {
    await this.sdk.tenants.delete(tenantId || this.tenantId);
  }

  /**
   * Create a new destination
   */
  async createDestination(data: DestinationCreate): Promise<Destination> {
    return await this.sdk.destinations.create(this.tenantId, data);
  }

  /**
   * Get a destination by ID
   */
  async getDestination(destinationId: string, tenantId?: string): Promise<Destination> {
    return await this.sdk.destinations.get(tenantId || this.tenantId, destinationId);
  }

  /**
   * List all destinations
   */
  async listDestinations(params?: {
    type?: DestinationType | DestinationType[];
  }): Promise<Destination[]> {
    return await this.sdk.destinations.list(this.tenantId, params?.type);
  }

  /**
   * Update a destination
   */
  async updateDestination(
    destinationId: string,
    data: DestinationUpdate,
    tenantId?: string
  ): Promise<Destination> {
    return await this.sdk.destinations.update(tenantId || this.tenantId, destinationId, data);
  }

  /**
   * Delete a destination
   */
  async deleteDestination(destinationId: string, tenantId?: string): Promise<void> {
    await this.sdk.destinations.delete(tenantId || this.tenantId, destinationId);
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
    baseURL: config.baseURL || process.env.API_BASE_URL || 'http://localhost:3333/api/v1',
    apiKey: config.apiKey || process.env.API_KEY,
  });
}
