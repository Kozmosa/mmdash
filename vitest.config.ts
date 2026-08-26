import { defineConfig } from "vitest/config";

export default defineConfig({
  test: {
    // CI runners are slower than local development; retry once-flaky
    // timing-sensitive tests there instead of failing the whole pipeline.
    retry: process.env.CI ? 2 : 0,
  },
});
