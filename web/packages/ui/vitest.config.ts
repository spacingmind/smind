import path from "node:path";
import { defineConfig } from "vitest/config";

export default defineConfig({
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  test: {
    // jsdom (not "node") so component tests can render real DOM trees via
    // @testing-library/react -- ws-client.test.ts doesn't need a DOM, but
    // running it under jsdom too is harmless, and Vitest only supports one
    // environment per config short of per-file overrides.
    environment: "jsdom",
    include: ["src/**/*.test.{ts,tsx}"],
    setupFiles: ["./src/test/setup.ts"],
  },
});
