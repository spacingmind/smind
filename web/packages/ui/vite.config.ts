import path from "node:path";
import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  base: "./",
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    proxy: {
      "/healthz": "http://127.0.0.1:4648",
      "/api": "http://127.0.0.1:4648",
      "/ws": {
        target: "ws://127.0.0.1:4648",
        ws: true,
      },
    },
  },
});
