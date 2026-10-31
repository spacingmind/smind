import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { describe, expect, it } from "vitest";

// vitest runs with the package (web/packages/ui) as cwd.
const pkgDir = process.cwd();
const srcDir = path.join(pkgDir, "src");
const read = (p: string) => readFileSync(p, "utf8");

function sourceFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((n) => {
    const p = path.join(dir, n);
    if (statSync(p).isDirectory()) return sourceFiles(p);
    return /\.(ts|tsx)$/.test(n) && !/\.test\.(ts|tsx)$/.test(n) ? [p] : [];
  });
}

describe("pdf.js is pinned and lazy (AC21)", () => {
  it("pdfjs_is_pinned_exactly_at_a_fixed_version", () => {
    const pkg = JSON.parse(read(path.join(pkgDir, "package.json"))) as { dependencies: Record<string, string> };
    const v = pkg.dependencies["pdfjs-dist"];
    expect(v).toMatch(/^\d+\.\d+\.\d+$/); // no ^, ~, range or tag
    const [maj, min, pat] = v.split(".").map(Number);
    // CVE-2024-4367 (code execution through a crafted font) is fixed in 4.2.67
    expect(maj * 1e6 + min * 1e3 + pat).toBeGreaterThanOrEqual(4 * 1e6 + 2 * 1e3 + 67);
    expect(v).toBe("6.4.299");
    // the licence attribution travels with the pin
    const notice = read(path.join(pkgDir, "../../../NOTICE"));
    expect(notice).toContain("pdfjs-dist 6.4.299");
    expect(notice).toContain("Apache License, Version 2.0");
  });

  it("entry_code_never_imports_pdfjs_or_the_pane_statically", () => {
    const viewerDir = path.join(srcDir, "components", "pdf-preview", "viewer");
    for (const f of sourceFiles(srcDir)) {
      const text = read(f);
      const rel = path.relative(srcDir, f).replaceAll("\\", "/");
      // only the viewer's own script may import pdf.js, and it is not part of the app entry (a separate IIFE build)
      if (!f.startsWith(viewerDir) && !rel.endsWith("pdf-preview/test-fixtures.ts")) {
        expect(text, `${rel} imports pdfjs-dist`).not.toMatch(/from\s+["']pdfjs-dist|import\s*\(?\s*["']pdfjs-dist/); // named, default, side-effect and dynamic imports alike
      }
      // the pane is reached through React.lazy / import() only
      if (rel !== "components/pdf-preview/pdf-preview-pane.tsx") {
        expect(text, `${rel} statically imports the pane`).not.toMatch(/^import\s[^;]*from\s+["'](@\/components\/pdf-preview\/pdf-preview-pane|\.\/pdf-preview-pane)["']/m);
      }
    }
    const editor = read(path.join(srcDir, "components", "file-editor-pane.tsx"));
    expect(editor).toMatch(/lazy\(\(\)\s*=>\s*import\("@\/components\/pdf-preview\/pdf-preview-pane"\)/);
  });

  it("the_viewer_is_built_separately_not_into_the_app_entry", () => {
    const pkg = JSON.parse(read(path.join(pkgDir, "package.json"))) as { scripts: Record<string, string> };
    expect(pkg.scripts.build).toContain("vite build -c vite.pdf-viewer.config.ts");
    expect(pkg.scripts["build:desktop"]).toContain("vite build -c vite.pdf-viewer.config.ts");
    const html = read(path.join(pkgDir, "public", "pdf-viewer.html"));
    expect(html).toContain('<script src="./pdf-viewer.js"></script>'); // a classic script: an opaque origin cannot load a module one
    expect(html).not.toMatch(/type="module"/);
    expect(html).toContain("connect-src 'none'");
    expect(html).toContain("default-src 'none'");
  });
});
