import path from "node:path";
import { defineConfig } from "vite";

/**
 * The sandboxed PDF viewer (ADR-0049 section 3.3), built as a separate classic IIFE script next to the app: `pdf-viewer.html`
 * (public/) loads `pdf-viewer.js` with a plain <script src>, which an opaque-origin iframe may do without CORS. The app build runs
 * first and owns the output directory; this build adds one file to it (the `build` scripts pass --outDir).
 */
export default defineConfig({
  publicDir: false,
  resolve: { alias: { "@": path.resolve(__dirname, "./src") } },
  build: {
    emptyOutDir: false,
    target: "es2022",
    minify: true,
    lib: {
      entry: path.resolve(__dirname, "src/components/pdf-preview/viewer/main.ts"),
      formats: ["iife"],
      name: "SmindPdfViewer",
      fileName: () => "pdf-viewer.js",
    },
    rollupOptions: { output: { inlineDynamicImports: true } },
  },
});
