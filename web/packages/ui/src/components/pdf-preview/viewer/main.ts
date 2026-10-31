/**
 * The PDF viewer: the script of `pdf-viewer.html`, which the app loads in an `<iframe sandbox="allow-scripts">` (ADR-0049 section 3.3).
 * It runs in an opaque origin under the page's own CSP (`connect-src 'none'`), so it cannot read the app's storage, call the daemon or
 * fetch anything. It is built as a classic IIFE script (a module script would need CORS from an opaque origin) with the pdf.js worker
 * inlined as a blob.
 *
 * Input: one `load` message carrying the PDF bytes, from the parent window only. Output: ready / pages / page / error messages.
 * Annotations, scripting and XFA are off (see viewer-options.ts); a text layer is on so text can be selected and copied.
 */
import { GlobalWorkerOptions, PasswordException, TextLayer, getDocument } from "pdfjs-dist";
import type { PDFDocumentProxy, PDFPageProxy } from "pdfjs-dist";
import workerSource from "pdfjs-dist/build/pdf.worker.min.mjs?raw";

import { PDF_LIMITS } from "../limits";
import { parseHostMessage, type PdfError, type ViewerMessage } from "../host-protocol";
import { zoomKeyCommand } from "../keys";
import { RENDER_OPTIONS, buildDocumentOptions, clampCanvasScale } from "../viewer-options";

const root = document.getElementById("pages") as HTMLElement;
const PAD = 16;

let doc: PDFDocumentProxy | null = null;
let pageCount = 0;
let baseWidth = 612;
let baseHeight = 792;
let zoom = 1;
let fitMode = true;
let renderToken = 0;
let observer: IntersectionObserver | null = null;
const holders: HTMLElement[] = [];
const rendered = new Set<number>();
const visible = new Set<number>();

function post(msg: ViewerMessage): void {
  // "*": the viewer's origin is opaque and the parent's is unknown to it; the message carries no secret.
  window.parent.postMessage(msg, "*");
}

function clamp(v: number, lo: number, hi: number): number {
  return Math.min(hi, Math.max(lo, v));
}

function teardown(): void {
  observer?.disconnect();
  observer = null;
  visible.clear();
  rendered.clear();
  holders.length = 0;
  root.replaceChildren();
  const d = doc;
  doc = null;
  void d?.loadingTask.destroy().catch(() => undefined);
}

function fail(reason: PdfError): void {
  teardown();
  post({ type: "error", reason });
}

function fitScale(): number {
  const avail = Math.max(120, document.documentElement.clientWidth - PAD * 2);
  return clamp(avail / baseWidth, PDF_LIMITS.minZoom, PDF_LIMITS.maxZoom);
}

function sizeHolder(h: HTMLElement): void {
  h.style.width = `${Math.round(baseWidth * zoom)}px`;
  h.style.height = `${Math.round(baseHeight * zoom)}px`;
}

function observeAll(): void {
  observer?.disconnect();
  visible.clear();
  // One page of margin either side, so the visible pages +-1 are rendered.
  observer = new IntersectionObserver(
    (entries) => {
      for (const e of entries) {
        const n = Number((e.target as HTMLElement).dataset.page);
        if (e.isIntersecting) visible.add(n);
        else visible.delete(n);
      }
      void renderVisible();
    },
    { rootMargin: `${Math.round(baseHeight * zoom)}px 0px` },
  );
  for (const h of holders) observer.observe(h);
}

function layout(): void {
  root.replaceChildren();
  holders.length = 0;
  for (let i = 1; i <= pageCount; i++) {
    const h = document.createElement("div");
    h.className = "page";
    h.dataset.page = String(i);
    sizeHolder(h);
    root.appendChild(h);
    holders.push(h);
  }
  observeAll();
}

async function renderVisible(): Promise<void> {
  if (!doc) return;
  const want = new Set<number>();
  for (const n of visible) {
    for (const m of [n - 1, n, n + 1]) if (m >= 1 && m <= pageCount) want.add(m);
  }
  // Release pages that scrolled away (canvas memory); the holder keeps its size.
  for (const n of [...rendered]) {
    if (!want.has(n)) {
      rendered.delete(n);
      holders[n - 1]?.replaceChildren();
    }
  }
  for (const n of [...want].sort((a, b) => a - b)) await renderPage(n);
  reportPage();
}

async function renderPage(n: number): Promise<void> {
  const d = doc;
  const holder = holders[n - 1];
  if (!d || !holder || rendered.has(n)) return;
  const token = renderToken;
  rendered.add(n);
  let page: PDFPageProxy;
  try {
    page = await d.getPage(n);
  } catch {
    return; // one bad page leaves a blank page, not a dead viewer
  }
  if (doc !== d || token !== renderToken) return;

  const unscaled = page.getViewport({ scale: 1 });
  const cssViewport = page.getViewport({ scale: zoom });
  holder.style.width = `${Math.round(cssViewport.width)}px`;
  holder.style.height = `${Math.round(cssViewport.height)}px`;

  const canvasViewport = page.getViewport({ scale: clampCanvasScale(unscaled.width, unscaled.height, zoom, window.devicePixelRatio || 1) });
  const canvas = document.createElement("canvas");
  canvas.width = Math.max(1, Math.floor(canvasViewport.width));
  canvas.height = Math.max(1, Math.floor(canvasViewport.height));
  canvas.style.width = `${Math.round(cssViewport.width)}px`;
  canvas.style.height = `${Math.round(cssViewport.height)}px`;
  const ctx = canvas.getContext("2d");
  if (!ctx) return;
  try {
    await page.render({ canvasContext: ctx, canvas, viewport: canvasViewport, ...RENDER_OPTIONS }).promise;
  } catch {
    return;
  }
  if (doc !== d || token !== renderToken) return;

  const text = document.createElement("div");
  text.className = "textLayer";
  text.style.setProperty("--total-scale-factor", String(zoom));
  text.style.setProperty("--scale-factor", String(zoom));
  holder.replaceChildren(canvas, text);
  try {
    await new TextLayer({ textContentSource: page.streamTextContent(), container: text, viewport: cssViewport }).render();
  } catch {
    // no selectable text for this page; the canvas stays
  }
}

/** The 1-based page whose top is the last one above the upper third of the window. */
function currentPage(): number {
  const line = window.innerHeight / 3;
  let current = 1;
  for (let i = 0; i < holders.length; i++) {
    if (holders[i].getBoundingClientRect().top <= line) current = i + 1;
    else break;
  }
  return current;
}

let reportQueued = false;
function reportPage(): void {
  if (reportQueued) return;
  reportQueued = true;
  requestAnimationFrame(() => {
    reportQueued = false;
    if (pageCount > 0) post({ type: "page", current: currentPage(), count: pageCount, zoom });
  });
}

function rerender(): void {
  renderToken++;
  rendered.clear();
  for (const h of holders) {
    h.replaceChildren();
    sizeHolder(h);
  }
  // A fresh observer re-delivers the current intersections, which renders what is on screen.
  observeAll();
  reportPage();
}

function setZoom(next: number, fit: boolean): void {
  const target = clamp(next, PDF_LIMITS.minZoom, PDF_LIMITS.maxZoom);
  if (target === zoom && fit === fitMode) return;
  fitMode = fit;
  zoom = target;
  rerender();
}

function goto(target: number | "first" | "last" | "next" | "prev"): void {
  if (pageCount === 0) return;
  const cur = currentPage();
  let n: number;
  if (target === "first") n = 1;
  else if (target === "last") n = pageCount;
  else if (target === "next") n = Math.min(pageCount, cur + 1);
  else if (target === "prev") n = Math.max(1, cur - 1);
  else n = clamp(target, 1, pageCount);
  holders[n - 1]?.scrollIntoView({ block: "start" });
}

/**
 * The pdf.js worker, run from a blob as a CLASSIC worker: an opaque-origin page cannot start a module worker (its fetch is a CORS request
 * that has no origin to satisfy), but it can start a classic blob worker. The worker file has no imports. Two edits make it classic: the
 * trailing `export` statement is dropped (the file starts itself when it finds it is in a worker), and `import.meta.url` (used only by the
 * wasm image decoders to find their .wasm files, which this viewer never loads) becomes a URL that fails to resolve, inside their own try.
 */
function classicWorker(): Worker {
  const classic = workerSource
    .replace(/export\s*\{\s*WorkerMessageHandler\s*\}\s*;?\s*$/, "")
    .replace(/import\.meta\.url/g, '"about:blank"');
  if (classic === workerSource || /\bimport\.meta\b|^\s*export\b/m.test(classic)) throw new Error("pdf.js worker could not be made classic");
  return new Worker(URL.createObjectURL(new Blob([classic], { type: "text/javascript" })));
}

async function load(bytes: ArrayBuffer): Promise<void> {
  teardown();
  renderToken++;
  GlobalWorkerOptions.workerPort = classicWorker();
  try {
    const task = getDocument(buildDocumentOptions(new Uint8Array(bytes)) as Parameters<typeof getDocument>[0]);
    doc = await task.promise;
  } catch (err) {
    fail(err instanceof PasswordException ? "password" : "corrupt");
    return;
  }
  const total = doc.numPages;
  pageCount = Math.min(total, PDF_LIMITS.maxPages);
  if (pageCount < 1) {
    fail("corrupt");
    return;
  }
  try {
    const vp = (await doc.getPage(1)).getViewport({ scale: 1 });
    baseWidth = vp.width;
    baseHeight = vp.height;
  } catch {
    fail("corrupt");
    return;
  }
  fitMode = true;
  zoom = fitScale();
  layout();
  post({ type: "pages", count: pageCount, total });
  reportPage();
}

window.addEventListener("message", (event: MessageEvent) => {
  // Only the embedding page may drive the viewer.
  if (event.source !== window.parent) return;
  const msg = parseHostMessage(event.data);
  if (!msg) return;
  if (msg.type === "load") void load(msg.data);
  else if (msg.type === "goto") goto(msg.page);
  else if (msg.mode === "fit") setZoom(fitScale(), true);
  else if (msg.mode === "set") setZoom(msg.value ?? zoom, false);
  else setZoom(zoom * (msg.value ?? 1), false);
});

window.addEventListener("resize", () => {
  if (fitMode && doc) {
    zoom = fitScale();
    rerender();
  }
});
window.addEventListener("scroll", reportPage, { passive: true });
// Focus inside the viewer: Mod+= / Mod+- / Mod+0 zoom the document instead of the browser page.
window.addEventListener("keydown", (e) => {
  const cmd = zoomKeyCommand(e);
  if (!cmd || cmd.type !== "zoom" || !doc) return;
  e.preventDefault();
  if (cmd.mode === "fit") setZoom(fitScale(), true);
  else setZoom(zoom * (cmd.value ?? 1), false);
});

post({ type: "ready" });
