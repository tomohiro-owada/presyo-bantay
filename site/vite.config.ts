import { defineConfig } from "vitest/config";

// Minimal Node process typing so `tsc --noEmit` passes without @types/node.
declare const process: { env: Record<string, string | undefined> };

// Production builds use base '/presyo-bantay/' for the GitHub Pages project
// subpath; the dev server keeps '/'. PRESYO_BASE env var overrides both.
// The app fetches data via import.meta.env.BASE_URL + "data/", so it keeps
// working under the subpath.
export default defineConfig(({ command }) => ({
  base:
    process.env.PRESYO_BASE ?? (command === "build" ? "/presyo-bantay/" : "/"),
  build: {
    outDir: "dist",
    sourcemap: false,
    chunkSizeWarningLimit: 200,
  },
  test: {
    environment: "node",
    include: ["src/**/*.test.ts"],
  },
}));
