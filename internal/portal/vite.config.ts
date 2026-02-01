import { defineConfig, type UserConfig } from "vite";
import react from "@vitejs/plugin-react-swc";
import { sentryVitePlugin } from "@sentry/vite-plugin";

export default defineConfig(({ mode }) => {
  const plugins: UserConfig["plugins"] = [react()];

  if (process.env.SENTRY_AUTH_TOKEN && mode === "production") {
    plugins.push(
      sentryVitePlugin({
        authToken: process.env.SENTRY_AUTH_TOKEN,
        org: "hookdeck",
        project: "outpost-portal",
        telemetry: false,
        bundleSizeOptimizations: {
          excludeTracing: true,
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
      host: true,
      watch: {
        usePolling: true, // Required for Docker volume mounts
      },
      hmr: {
        clientPort: 3334, // Ensure HMR WebSocket connects to the right port
      },
    },
    build: {
      sourcemap: true,
    },
  };

  return config;
});
