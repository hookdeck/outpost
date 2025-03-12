import axios from "axios";

interface Tenant {
  id: string;
}

interface Destination {
  id: string;
}

class OutpostClient {
  outpostApiUrl: string;
  outpostApiKey: string;

  constructor(outpostApiUrl: string, outpostApiKey: string) {
    this.outpostApiUrl = outpostApiUrl;
    this.outpostApiKey = outpostApiKey;
  }

  async request<T>(path: string, method: string, data: any): Promise<T> {
    const response = await axios.request<T>({
      url: `${this.outpostApiUrl}${path}`,
      method,
      data,
      headers: {
        Authorization: `Bearer ${this.outpostApiKey}`,
      },
    });
    return response.data;
  }

  async publishEvent({
    tenant_id,
    topic,
    data,
    meta_data,
  }: {
    tenant_id: string;
    topic: string;
    data: any;
    meta_data: any;
  }): Promise<boolean> {
    const response = await this.request("/publish", "POST", {
      tenant_id,
      topic,
      data,
      meta_data,
    });
    return !!response;
  }

  async registerTenant(tenant_id: string): Promise<Tenant> {
    const response = await this.request<Tenant>(`/${tenant_id}`, "PUT", {});
    return response;
  }

  async deleteTenant(tenant_id: string) {
    const response = await this.request<Tenant>(`/${tenant_id}`, "DELETE", {});
    return response;
  }

  async getDestinations(tenant_id: string) {
    const response = await this.request<Destination[]>(
      `/${tenant_id}/destinations`,
      "GET",
      {}
    );
    return response;
  }

  async createDestination({
    tenant_id,
    type,
    url,
    topics,
    secret,
  }: {
    tenant_id: any;
    type: string;
    url: string;
    topics: string[];
    secret: string;
  }) {
    const response = await this.request<Destination>(
      `/${tenant_id}/destinations`,
      "POST",
      {
        type,
        config: {
          url,
        },
        topics,
        credentials: {
          secret,
        },
      }
    );
    return response;
  }

  async deleteDestination(tenant_id: string, destination_id: string) {
    const response = await this.request<Destination>(
      `/${tenant_id}/destinations/${destination_id}`,
      "DELETE",
      {}
    );
    return response;
  }

  async getPortalURL(tenant_id: string): Promise<string> {
    const response = await this.request<{ redirect_url: string }>(
      `/${tenant_id}/portal`,
      "GET",
      {}
    );
    return response.redirect_url;
  }
}

export default OutpostClient;
