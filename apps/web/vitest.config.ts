import { defineConfig } from "vitest/config";
import { fileURLToPath } from "node:url";

export default defineConfig({
  esbuild: {
    jsx: "automatic",
  },
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  test: {
    environment: "jsdom",
    setupFiles: ["./test/setup.ts"],
    // CI runners are slower than local development; retry timing-sensitive
    // DOM/request-order assertions there instead of failing the pipeline.
    retry: process.env.CI ? 2 : 0,
  },
});
