/** Resource caps of the PDF preview (ADR-0049 section 3.3). Shared by the host pane and the sandboxed viewer. */
export const PDF_LIMITS = {
  /** Pages laid out at all; a longer document shows its first 2000. */
  maxPages: 2000,
  /** One page canvas, in pixels (16 megapixels). */
  maxCanvasPixels: 16 * 1024 * 1024,
  /** pdf.js `canvasMaxAreaInBytes`: 4 bytes per pixel of the cap above. */
  canvasMaxAreaInBytes: 16 * 1024 * 1024 * 4,
  /** pdf.js `maxImageSize`: one decoded image, in pixels. */
  maxImageSize: 16 * 1024 * 1024,
  /** Parse timeout before the host tears the viewer down. */
  parseTimeoutMs: 30_000,
  minZoom: 0.25,
  maxZoom: 4,
} as const;
