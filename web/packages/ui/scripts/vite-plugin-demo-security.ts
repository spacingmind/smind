import { rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import type { Plugin } from "vite";

import { cspMetaTag, headersFile, inlineScriptHashes } from "./demo-csp.mjs";

/**
 * Demo build only (ADR-0051 section 10): injects the CSP <meta> (the sha256 of the inline theme bootstrap is
 * computed from the final index.html, so editing the script cannot ship without a matching hash) and writes the
 * `_headers` file with the full policy including frame-ancestors.
 */
export function demoSecurityPlugin(): Plugin {
  let hashes: string[] = [];
  let outDir = "";
  return {
    name: "smind-demo-security",
    apply: "build",
    configResolved(config) {
      outDir = path.resolve(config.root, config.build.outDir);
    },
    transformIndexHtml: {
      order: "post",
      handler(html) {
        hashes = inlineScriptHashes(html);
        return html.replace("<head>", `<head>\n    ${cspMetaTag(hashes)}`);
      },
    },
    writeBundle() {
      writeFileSync(path.join(outDir, "_headers"), headersFile(hashes));
      // public/pdf-viewer.html is the PDF preview's page; the demo has no file preview, so it must not ship.
      rmSync(path.join(outDir, "pdf-viewer.html"), { force: true });
    },
  };
}
