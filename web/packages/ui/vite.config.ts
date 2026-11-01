import path from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { demoSecurityPlugin } from "./scripts/vite-plugin-demo-security";

// `vite build --mode demo` (ADR-0051) builds the real UI against the scripted in-browser daemon in
// src/demo/. __SMIND_DEMO__ is a build-time constant: every other mode (the daemon-embedded and the
// desktop build) drops src/demo/**, and the demo build drops the daemon connect code.
export default defineConfig(({ mode }) => {
  const demo = mode === "demo";
  return {
    plugins: [react(), tailwindcss(), ...(demo ? [demoSecurityPlugin()] : [])],
    base: "./",
    define: { __SMIND_DEMO__: JSON.stringify(demo) },
    resolve: {
      alias: {
        "@": path.resolve(__dirname, "./src"),
      },
    },
    build: demo ? { outDir: "dist-demo", emptyOutDir: true } : {},
    server: demo
      ? {}
      : {
          proxy: {
            "/healthz": "http://127.0.0.1:4648",
            "/api": "http://127.0.0.1:4648",
            "/ws": {
              target: "ws://127.0.0.1:4648",
              ws: true,
            },
          },
        },
    preview: { host: "127.0.0.1", port: 4173, strictPort: true },
  };
});
