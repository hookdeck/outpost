import { defineConfig, UserConfig } from "vite";
import react from "@vitejs/plugin-react-swc";

export default defineConfig(() => {
  const config: UserConfig = {
    plugins: [react()],
    server: {
      port: 3334,
    },
  };

  return config;
});
