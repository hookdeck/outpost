import axios, { AxiosInstance, AxiosError, AxiosRequestConfig } from 'axios';
import { config as loadEnv } from 'dotenv';

// Load environment variables from .env file
loadEnv();

export interface ApiClientConfig {
  baseURL?: string;
  tenantId?: string;
  apiKey?: string;
  timeout?: number;
  useProxy?: boolean;
}

export interface CreateDestinationRequest {
  id?: string;
  type: string;
  topics: string | string[];
  config: Record<string, any>;
  credentials?: Record<string, any>;
}

export interface Destination {
  id: string;
  type: string;
  topics: string | string[];
  config: Record<string, any>;
  credentials?: Record<string, any>;
  disabled_at: string | null;
  created_at: string;
  target?: string;
  target_url?: string | null;
}

export interface UpdateDestinationRequest {
  topics?: string | string[];
  config?: Record<string, any>;
  credentials?: Record<string, any>;
}

export interface ApiError {
  message: string;
  code?: string;
  details?: any;
}

export class ApiClient {
  private client: AxiosInstance;
  private tenantId: string;

  constructor(config: ApiClientConfig = {}) {
    const baseURL = config.baseURL || process.env.API_BASE_URL || 'http://localhost:9000';
    this.tenantId = config.tenantId || process.env.TENANT_ID || 'test-tenant';

    if (process.env.DEBUG_API_REQUESTS === 'true') {
      console.log(`[ApiClient] Creating client with baseURL: ${baseURL}`);
    }

    this.client = axios.create({
      baseURL: baseURL,
      timeout: config.timeout || 10000,
      headers: {
        'Content-Type': 'application/json',
        ...(config.apiKey && { Authorization: `Bearer ${config.apiKey}` }),
      },
      validateStatus: () => true, // Don't throw on any status code
    });

    // Add request interceptor for logging (optional, for debugging)
    this.client.interceptors.request.use(
      (config) => {
        if (process.env.DEBUG_API_REQUESTS === 'true') {
          console.log(`[API Request] ${config.method?.toUpperCase()} ${config.url}`);
          if (config.data) {
            console.log('[API Request Body]', JSON.stringify(config.data, null, 2));
          }
        }
        return config;
      },
      (error) => Promise.reject(error)
    );

    // Add response interceptor for logging
    this.client.interceptors.response.use(
      (response) => {
        if (process.env.DEBUG_API_REQUESTS === 'true') {
          console.log(
            `[API Response] ${response.status} ${response.config.method?.toUpperCase()} ${response.config.url}`
          );
          console.log('[API Response Body]', JSON.stringify(response.data, null, 2));
        }
        return response;
      },
      (error) => Promise.reject(error)
    );
  }

  /**
   * Create or update a tenant (idempotent)
   */
  async upsertTenant(): Promise<any> {
    const response = await this.client.put(`/${this.tenantId}`);

    if (response.status >= 200 && response.status < 300) {
      return response.data;
    }

    throw this.createError(response);
  }

  /**
   * Delete a tenant
   */
  async deleteTenant(): Promise<void> {
    const response = await this.client.delete(`/${this.tenantId}`);

    if (response.status >= 200 && response.status < 300) {
      return;
    }

    throw this.createError(response);
  }

  /**
   * Create a new destination
   */
  async createDestination(data: CreateDestinationRequest): Promise<Destination> {
    const response = await this.client.post(`/${this.tenantId}/destinations`, data);

    if (response.status >= 200 && response.status < 300) {
      return response.data;
    }

    throw this.createError(response);
  }

  /**
   * Get a destination by ID
   */
  async getDestination(id: string): Promise<Destination> {
    const response = await this.client.get(`/${this.tenantId}/destinations/${id}`);

    if (response.status >= 200 && response.status < 300) {
      return response.data;
    }

    throw this.createError(response);
  }

  /**
   * List all destinations
   */
  async listDestinations(params?: {
    type?: string;
    limit?: number;
    cursor?: string;
  }): Promise<Destination[]> {
    const response = await this.client.get(`/${this.tenantId}/destinations`, {
      params,
    });

    if (response.status >= 200 && response.status < 300) {
      // Handle both array response and paginated response
      if (Array.isArray(response.data)) {
        return response.data;
      }
      if (response.data.data) {
        return response.data.data;
      }
      return response.data;
    }

    throw this.createError(response);
  }

  /**
   * Update a destination
   */
  async updateDestination(id: string, data: UpdateDestinationRequest): Promise<Destination> {
    const response = await this.client.patch(`/${this.tenantId}/destinations/${id}`, data);

    if (response.status >= 200 && response.status < 300) {
      return response.data;
    }

    throw this.createError(response);
  }

  /**
   * Delete a destination
   */
  async deleteDestination(id: string): Promise<void> {
    const response = await this.client.delete(`/${this.tenantId}/destinations/${id}`);

    if (response.status >= 200 && response.status < 300) {
      return;
    }

    throw this.createError(response);
  }

  /**
   * Make a raw request (for testing error scenarios)
   */
  async rawRequest(config: AxiosRequestConfig) {
    return this.client.request(config);
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
   * Create a standardized error from an API response
   */
  private createError(response: any): Error {
    const error: ApiError = {
      message: response.data?.message || `API request failed with status ${response.status}`,
      code: response.data?.code,
      details: response.data,
    };

    const err = new Error(error.message) as Error & { response: any; apiError: ApiError };
    err.response = response;
    err.apiError = error;

    return err;
  }
}

/**
 * Create an API client that points directly to the API (bypassing Specmatic)
 * Useful for setup/teardown operations
 */
export function createDirectClient(config: ApiClientConfig = {}): ApiClient {
  return new ApiClient({
    ...config,
    baseURL: config.baseURL || process.env.API_DIRECT_URL || 'http://localhost:3333',
    apiKey: config.apiKey || process.env.API_KEY,
  });
}

/**
 * Create an API client that points to Specmatic proxy
 * This is the default for contract testing
 */
export function createProxyClient(config: ApiClientConfig = {}): ApiClient {
  return new ApiClient({
    ...config,
    baseURL: config.baseURL || process.env.API_PROXY_URL || 'http://localhost:9000',
    apiKey: config.apiKey || process.env.API_KEY,
    useProxy: true,
  });
}
