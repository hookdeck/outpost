import { defineConfig, UserConfig } from "vite";
import react from "@vitejs/plugin-react-swc";
import { sentryVitePlugin } from "@sentry/vite-plugin";

export default defineConfig(({ mode }) => {
  let plugins: UserConfig["plugins"] = [react()];

  if (process.env.SENTRY_AUTH_TOKEN && mode === "production") {
    plugins.push(
      sentryVitePlugin({
        authToken: process.env.SENTRY_AUTH_TOKEN,
        org: "hookdeck",
        project: "outpost-portal",
        telemetry: false,
        bundleSizeOptimizations: {
          excludeTracing: true,
          excludePerformanceMonitoring: true,
          excludeReplayCanvas: true,
          excludeReplayShadowDom: true,
          excludeReplayIframe: true,
          excludeReplayWorker: true,
        },
      }),
    );
  }

  const config: UserConfig = {
    plugins,
    server: {
      port: 3334,
      // hmr: {
      //   port: 3334,
      // },
    },
    build: {
      sourcemap: true,
    },
  };

  return config;
});
