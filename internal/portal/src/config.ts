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
    DISABLE_TELEMETRY: boolean;
    BRAND_COLOR: string;
  }) || {};

export default CONFIGS;
