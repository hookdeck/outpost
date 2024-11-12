import { defineConfig, UserConfig } from "vite";
import react from "@vitejs/plugin-react-swc";

export default defineConfig(({ mode }) => {
  let config: UserConfig = {
    plugins: [react()],
    server: {
      port: 3334,
    },
  };

  if (mode === "development") {
    config.server!.proxy = {
      "/api": "http://localhost:3333",
    };
  }

  return config;
});
