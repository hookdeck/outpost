/// <reference types="vite/client" />

import * as Sentry from "@sentry/react";
import CONFIGS from "./config";

Sentry.init({
  dsn: "https://e53653f8b83691c0b4d02c92fc7505b6@o376420.ingest.us.sentry.io/4508422371409920", // Outpost maintainers managed DSN for Sentry
  integrations: [],
  enabled: !CONFIGS.DISABLE_TELEMETRY,
  environment: import.meta.env.MODE,
});

Sentry.setContext("Portal Config", {
  organization_name: CONFIGS.ORGANIZATION_NAME,
  force_theme: CONFIGS.FORCE_THEME,
  referer_url: CONFIGS.REFERER_URL,
});

export default Sentry;
