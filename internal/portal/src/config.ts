const DEFAULTS = {
  ORGANIZATION_NAME: "",
  LOGO: "",
  LOGO_DARK: "",
  FAVICON_URL: "",
  REFERER_URL: "",
  REFRESH_URL: "",
  FORCE_THEME: "",
  TOPICS: "",
  TOPICS_ALLOW_WILDCARDS: "",
  DISABLE_OUTPOST_BRANDING: "",
  DISABLE_TELEMETRY: "",
  SENTRY_DSN: "",
  BRAND_COLOR: "",
  ENABLE_DESTINATION_FILTER: "",
  ENABLE_WEBHOOK_CUSTOM_HEADERS: "",
};

type PortalConfig = typeof DEFAULTS;

export const configLoaded = (window as any).PORTAL_CONFIGS != null;

const CONFIGS: PortalConfig = {
  ...DEFAULTS,
  ...((window as any).PORTAL_CONFIGS as Partial<PortalConfig> | undefined),
};

export default CONFIGS;
