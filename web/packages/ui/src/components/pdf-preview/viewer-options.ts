import { PDF_LIMITS } from "./limits";

/** pdf.js `AnnotationMode.DISABLE`: no annotation layer, so links, forms and attachments inside a PDF do nothing. */
export const ANNOTATION_MODE_DISABLE = 0;

/**
 * The only way the viewer builds `getDocument`'s argument (ADR-0049 section 3.3). `data` is the sole input (never `url`), so the
 * viewer can not fetch anything; scripting, XFA and eval are off; streaming and auto-fetch are off; image and canvas sizes are capped.
 * `isEvalSupported` and `enableScripting` are belt and braces on current pdf.js (which has no eval path and runs scripts only in a
 * sandbox the viewer never loads) and stay so a future pdf.js default cannot turn them on.
 */
export function buildDocumentOptions(data: Uint8Array): Record<string, unknown> {
  return {
    data,
    isEvalSupported: false,
    enableScripting: false,
    enableXfa: false,
    useSystemFonts: false,
    disableAutoFetch: true,
    disableStream: true,
    stopAtErrors: false,
    maxImageSize: PDF_LIMITS.maxImageSize,
    canvasMaxAreaInBytes: PDF_LIMITS.canvasMaxAreaInBytes,
    // No same-origin asset fetches from an opaque-origin viewer under connect-src 'none': wasm image decoders and the cMap and
    // standard-font folders are deliberately not configured (an affected image or glyph degrades; nothing is requested).
    wasmUrl: null,
    cMapUrl: null,
    standardFontDataUrl: null,
  };
}

/** The options of every `page.render` call: the annotation layer is off. */
export const RENDER_OPTIONS = { annotationMode: ANNOTATION_MODE_DISABLE, intent: "display" } as const;

/** Canvas scale for a viewport of `width x height` CSS pixels at `scale`: reduced until the canvas fits the pixel cap. */
export function clampCanvasScale(width: number, height: number, scale: number, deviceRatio: number): number {
  let s = scale * Math.max(1, deviceRatio);
  const pixels = width * height * s * s;
  if (pixels > PDF_LIMITS.maxCanvasPixels) s *= Math.sqrt(PDF_LIMITS.maxCanvasPixels / pixels);
  return s;
}
