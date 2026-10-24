import { defineConfig } from "vitest/config";

// base './' keeps asset URLs relative so the build works both on a
// GitHub Pages project subpath and on Cloudflare Pages.
export default defineConfig({
  base: "./",
  build: {
    outDir: "dist",
    sourcemap: false,
    chunkSizeWarningLimit: 200,
  },
  test: {
    environment: "node",
    include: ["src/**/*.test.ts"],
  },
});
