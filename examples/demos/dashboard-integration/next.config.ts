import path from "node:path";
import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  // The @hookdeck/outpost-sdk dependency is a symlink to ../../../sdks/outpost-typescript,
  // which lives outside this demo's directory. Point Turbopack's root at the repo root so
  // it traverses the symlink target during module resolution.
  turbopack: {
    root: path.resolve(process.cwd(), "../../.."),
  },
};

export default nextConfig;
