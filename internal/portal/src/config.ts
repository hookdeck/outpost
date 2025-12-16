const CONFIGS =
  ((window as any).PORTAL_CONFIGS as {
    ORGANIZATION_NAME: string;
    LOGO: string;
    LOGO_DARK: string;
    FAVICON_URL: string;
    REFERER_URL: string;
    FORCE_THEME: string;
    TOPICS: string;
    DISABLE_OUTPOST_BRANDING: string;
    DISABLE_TELEMETRY: string;
    BRAND_COLOR: string;
    ENABLE_DESTINATION_FILTER: string;
    ENABLE_WEBHOOK_CUSTOM_HEADERS: string;
  }) || {};

export default CONFIGS;
