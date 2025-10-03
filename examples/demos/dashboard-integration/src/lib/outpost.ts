import { Outpost } from "@hookdeck/outpost-sdk";
import logger from "./logger";

let outpostClient: Outpost | null = null;

export function getOutpostClient(): Outpost {
  if (!outpostClient) {
    if (!process.env.OUTPOST_API_KEY) {
      throw new Error("OUTPOST_API_KEY environment variable is required");
    }

    outpostClient = new Outpost({
      serverURL: `${
        process.env.OUTPOST_BASE_URL || "http://localhost:3333"
      }/api/v1`,
      security: {
        adminApiKey: process.env.OUTPOST_API_KEY,
      },
    });
  }

  return outpostClient;
}

export async function createTenant(tenantId: string): Promise<void> {
  try {
    logger.info(`Creating tenant in Outpost: ${tenantId}`);
    const outpost = getOutpostClient();

    const tenant = await outpost.tenants.upsert({
      tenantId,
    });
    logger.info(`Tenant created successfully in Outpost`, { tenantId, tenant });
  } catch (error) {
    logger.error(`Error creating tenant in Outpost: ${tenantId}`, {
      error,
      tenantId,
    });
    throw error;
  }
}

export async function getPortalUrl(
  tenantId: string,
  theme?: string
): Promise<string> {
  try {
    const outpost = getOutpostClient();
    const result = await outpost.tenants.getPortalUrl({
      tenantId,
      theme: theme as any, // SDK accepts theme as string but types are restrictive
    });
    return result.redirectUrl || "";
  } catch (error) {
    logger.error("Error getting portal URL", { error, tenantId, theme });
    throw error;
  }
}

export async function getTenantOverview(tenantId: string) {
  try {
    logger.debug(`Getting tenant overview for: ${tenantId}`);
    const outpost = getOutpostClient();

    // Get tenant details
    const tenant = await outpost.tenants.get({ tenantId });
    logger.debug(`Tenant found`, { tenantId, tenant });

    // Get destinations for this tenant
    const destinationsResponse = await outpost.destinations.list({ tenantId });
    logger.debug(`Destinations found`, {
      tenantId,
      count: destinationsResponse?.length,
    });

    // Transform destinations to match our interface
    const destinations = Array.isArray(destinationsResponse)
      ? destinationsResponse.map((dest: any) => ({
          ...dest,
          enabled: dest.disabledAt === null, // Convert disabledAt to enabled boolean
        }))
      : [];

    // Get recent events with proper error handling
    let recentEvents: any[] = [];
    let totalEvents = 0;

    try {
      const eventsResponse = await outpost.events.list({ tenantId });
      // Handle paginated API response structure
      if (eventsResponse && typeof eventsResponse === "object") {
        // Check if it's a paginated response with data array
        if ("data" in eventsResponse && Array.isArray(eventsResponse.data)) {
          recentEvents = eventsResponse.data.slice(0, 10);
          totalEvents =
            (eventsResponse as any).count || eventsResponse.data.length;
        } else if (Array.isArray(eventsResponse)) {
          // Direct array response
          recentEvents = eventsResponse.slice(0, 10);
          totalEvents = eventsResponse.length;
        }
      }
      logger.debug(`Events found`, { tenantId, totalEvents });
    } catch (error) {
      // Handle SDK validation error - the API response is valid but SDK expects different format
      if (error && typeof error === "object" && "rawValue" in error) {
        const rawResponse = (error as any).rawValue;
        if (
          rawResponse &&
          typeof rawResponse === "object" &&
          "data" in rawResponse &&
          Array.isArray(rawResponse.data)
        ) {
          recentEvents = rawResponse.data.slice(0, 10);
          totalEvents = rawResponse.count || rawResponse.data.length;
          logger.debug(`Events extracted from validation error`, {
            tenantId,
            totalEvents,
          });
        } else {
          logger.warn("Could not fetch events", { error, tenantId });
        }
      } else {
        logger.warn("Could not fetch events", { error, tenantId });
      }
    }

    const overview = {
      tenant,
      destinations: Array.isArray(destinations) ? destinations : [],
      recentEvents,
      stats: {
        totalDestinations: Array.isArray(destinations)
          ? destinations.length
          : 0,
        totalEvents,
      },
    };

    logger.debug(`Complete overview retrieved for tenant: ${tenantId}`, {
      tenantId,
      destinationCount: overview.stats.totalDestinations,
      eventCount: overview.stats.totalEvents,
    });
    return overview;
  } catch (error) {
    logger.error("Error getting tenant overview", { error, tenantId });
    throw error;
  }
}
