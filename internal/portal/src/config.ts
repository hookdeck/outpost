import { parseShowMCPDestinations } from "./utils/destinationTypes";

const CONFIGS =
  ((window as any).PORTAL_CONFIGS as {
    ORGANIZATION_NAME: string;
    LOGO: string;
    LOGO_DARK: string;
    FAVICON_URL: string;
    REFERER_URL: string;
    REFRESH_URL: string;
    FORCE_THEME: string;
    TOPICS: string;
    TOPICS_ALLOW_WILDCARDS: string;
    DISABLE_OUTPOST_BRANDING: string;
    DISABLE_TELEMETRY: string;
    BRAND_COLOR: string;
    ENABLE_DESTINATION_FILTER: string;
    ENABLE_WEBHOOK_CUSTOM_HEADERS: string;
    // Optional: servers that predate it don't send it. Absent means "true".
    SHOW_MCP_DESTINATIONS?: string;
  }) || {};

// API_BASE_PATH is the only place the portal names the API version. MCP
// destinations and destination type create modes exist in v2 only.
export const API_BASE_PATH = "/api/v2";

// SHOW_MCP_DESTINATIONS hides the MCP destination type and MCP destinations
// when false. Display only: the API still returns them to the tenant token.
export const SHOW_MCP_DESTINATIONS = parseShowMCPDestinations(
  CONFIGS.SHOW_MCP_DESTINATIONS,
);

export default CONFIGS;
