import net from "node:net";
import path from "node:path";
import { pathToFileURL } from "node:url";
import { afterEach, describe, expect, it, vi } from "vitest";
import * as pdfjs from "pdfjs-dist/legacy/build/pdf.mjs";

import { buildDocumentOptions } from "./viewer-options";
import { hostilePdf } from "./test-fixtures";

// vitest runs with the package (web/packages/ui) as cwd; pdfjs-dist is hoisted to web/node_modules.
const WORKER = pathToFileURL(path.resolve(process.cwd(), "../../node_modules/pdfjs-dist/legacy/build/pdf.worker.mjs")).href;

afterEach(() => vi.restoreAllMocks());

describe("a PDF that carries JavaScript, an external link and a launch action", () => {
  it("is_actually_hostile", () => {
    // Guard against a vacuous test: the fixture really contains each of the three.
    const text = new TextDecoder("latin1").decode(hostilePdf());
    expect(text).toContain("/S /JavaScript");
    expect(text).toContain("/URI (https://example.invalid/tracking-pixel)");
    expect(text).toContain("/S /Launch");
    expect(text).toContain("app.alert" + "\\" + "('pwned'"); // parentheses are backslash-escaped inside a PDF string
  });

  it("opens_and_reads_without_executing_or_fetching_anything", async () => {
    // Anything that would reach out: fetch, a socket, a WebSocket, a new window.
    const fetchSpy = vi.fn().mockRejectedValue(new Error("network is forbidden in this test"));
    vi.stubGlobal("fetch", fetchSpy);
    const connectSpy = vi.spyOn(net.Socket.prototype, "connect").mockImplementation(() => {
      throw new Error("a socket was opened");
    });
    const wsSpy = vi.fn();
    vi.stubGlobal("WebSocket", wsSpy);
    const openSpy = vi.fn();
    vi.stubGlobal("open", openSpy);
    const evalSpy = vi.fn();
    vi.stubGlobal("alert", evalSpy);

    pdfjs.GlobalWorkerOptions.workerSrc = WORKER;
    const opts = buildDocumentOptions(hostilePdf()) as Parameters<typeof pdfjs.getDocument>[0];
    const task = pdfjs.getDocument(opts);
    const doc = await task.promise;
    try {
      expect(doc.numPages).toBe(1);
      const page = await doc.getPage(1);
      const text = (await page.getTextContent()).items.map((i) => ("str" in i ? i.str : "")).join(" ");
      expect(text).toContain("Quarterly report");

      // The link and launch annotations are there as data; with the annotation layer off nothing renders them.
      const annots = await page.getAnnotations();
      expect(annots.some((a) => a.subtype === "Link")).toBe(true);
      const ops = await page.getOperatorList({ annotationMode: 0 /* AnnotationMode.DISABLE */ });
      expect(ops.fnArray.length).toBeGreaterThan(0);

      // The script exists in the document (we are not hiding it), but no sandbox is created, so nothing runs.
      expect(await doc.hasJSActions()).toBe(true);
    } finally {
      await task.destroy();
    }

    expect(fetchSpy).not.toHaveBeenCalled();
    expect(connectSpy).not.toHaveBeenCalled();
    expect(wsSpy).not.toHaveBeenCalled();
    expect(openSpy).not.toHaveBeenCalled();
    expect(evalSpy).not.toHaveBeenCalled();
  });

  it("the_viewer_never_loads_pdfjs_scripting_or_a_url_input", async () => {
    const { readFileSync } = await import("node:fs");
    const viewer = readFileSync(path.resolve(process.cwd(), "src/components/pdf-preview/viewer/main.ts"), "utf8");
    expect(viewer).not.toMatch(/pdf\.sandbox|enableScripting:\s*true|PDFScriptingManager|PDFLinkService/);
    expect(viewer).not.toMatch(/\burl:/);
    expect(viewer).not.toMatch(/window\.open|location\.(href|assign|replace)\s*=|fetch\(|XMLHttpRequest|WebSocket/);
  });
});
