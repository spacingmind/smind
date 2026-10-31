import { describe, expect, it } from "vitest";

import { PDF_LIMITS } from "./limits";
import { ANNOTATION_MODE_DISABLE, RENDER_OPTIONS, buildDocumentOptions, clampCanvasScale } from "./viewer-options";

describe("pdf viewer options (AC19)", () => {
  it("getDocument_options_are_hardened", () => {
    const bytes = new Uint8Array([1, 2, 3]);
    const o = buildDocumentOptions(bytes);
    expect(o.isEvalSupported).toBe(false);
    expect(o.enableScripting).toBe(false);
    expect(o.enableXfa).toBe(false);
    expect(o.useSystemFonts).toBe(false);
    expect(o.disableAutoFetch).toBe(true);
    expect(o.disableStream).toBe(true);
    expect(o.maxImageSize).toBe(PDF_LIMITS.maxImageSize);
    expect(o.canvasMaxAreaInBytes).toBe(PDF_LIMITS.canvasMaxAreaInBytes);
    // data only: no url, httpHeaders, range transport or password prompt that could reach out
    expect(o.data).toBe(bytes);
    for (const key of ["url", "httpHeaders", "range", "withCredentials", "password"]) expect(key in o).toBe(false);
    // no same-origin asset folders either (an opaque-origin viewer under connect-src 'none' could not fetch them)
    expect([o.cMapUrl, o.standardFontDataUrl, o.wasmUrl]).toEqual([null, null, null]);
  });

  it("annotation_layer_is_off_on_every_render", () => {
    expect(RENDER_OPTIONS.annotationMode).toBe(ANNOTATION_MODE_DISABLE);
    expect(ANNOTATION_MODE_DISABLE).toBe(0); // pdf.js AnnotationMode.DISABLE
  });

  it("caps_are_the_adr_numbers", () => {
    expect(PDF_LIMITS.maxPages).toBe(2000);
    expect(PDF_LIMITS.maxCanvasPixels).toBe(16 * 1024 * 1024);
    expect(PDF_LIMITS.parseTimeoutMs).toBe(30_000);
  });

  it("canvas_scale_never_exceeds_16_megapixels", () => {
    for (const [w, h, scale, dpr] of [
      [612, 792, 1, 1],
      [612, 792, 4, 3],
      [14400, 14400, 1, 2], // a 200 inch poster
      [200, 200, 0.25, 1],
    ] as const) {
      const s = clampCanvasScale(w, h, scale, dpr);
      expect(w * s * (h * s)).toBeLessThanOrEqual(PDF_LIMITS.maxCanvasPixels * 1.0001);
    }
    // a small page is not scaled down
    expect(clampCanvasScale(612, 792, 1, 2)).toBe(2);
  });
});
