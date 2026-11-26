/// <reference types="vite/client" />

import * as Sentry from "@sentry/react";
import CONFIGS from "./config";

Sentry.init({
  dsn: CONFIGS.SENTRY_DSN,
  integrations: [],
  enabled: !!CONFIGS.SENTRY_DSN && CONFIGS.DISABLE_TELEMETRY !== "true",
  environment: import.meta.env.MODE,
});

Sentry.setContext("Portal Config", {
  organization_name: CONFIGS.ORGANIZATION_NAME,
  force_theme: CONFIGS.FORCE_THEME,
  referer_url: CONFIGS.REFERER_URL,
});

export default Sentry;
