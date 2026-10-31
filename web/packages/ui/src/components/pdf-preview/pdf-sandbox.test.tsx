import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { PDF_VIEWER_SANDBOX, PdfViewerFrame, pdfViewerUrl } from "./pdf-viewer-frame";

describe("pdf viewer sandbox (AC18)", () => {
  it("iframe_sandbox_attribute_is_exactly_allow_scripts", () => {
    render(<PdfViewerFrame />);
    const frame = screen.getByTestId("pdf-viewer-frame");
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts");
    expect(PDF_VIEWER_SANDBOX).toBe("allow-scripts");
    // none of the escape hatches, by name (jsdom has no DOMTokenList for sandbox, so read the attribute)
    const tokens = (frame.getAttribute("sandbox") ?? "").split(/\s+/).filter(Boolean);
    expect(tokens).toEqual(["allow-scripts"]);
    for (const token of [
      "allow-same-origin", "allow-popups", "allow-forms", "allow-modals", "allow-top-navigation",
      "allow-downloads", "allow-popups-to-escape-sandbox", "allow-top-navigation-by-user-activation", "allow-presentation",
    ]) {
      expect(tokens.includes(token), token).toBe(false);
    }
  });

  it("loads_the_separate_viewer_document_and_no_data_or_blob_src", () => {
    render(<PdfViewerFrame />);
    const frame = screen.getByTestId("pdf-viewer-frame");
    expect(frame.getAttribute("src")).toBe(pdfViewerUrl());
    expect(pdfViewerUrl()).toMatch(/pdf-viewer\.html$/);
    expect(frame.getAttribute("srcdoc")).toBeNull();
    expect(frame.getAttribute("referrerpolicy")).toBe("no-referrer");
  });
});
