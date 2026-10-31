import { forwardRef } from "react";

/**
 * The `sandbox` attribute of the PDF viewer iframe (ADR-0049 section 3.3): scripts only. No `allow-same-origin` (the viewer's origin is
 * opaque, so a pdf.js exploit cannot read the app's storage or call the daemon), and none of `allow-popups`, `allow-forms`,
 * `allow-modals`, `allow-top-navigation` or `allow-downloads`.
 */
export const PDF_VIEWER_SANDBOX = "allow-scripts";

/** The viewer's document: a separate built page with its own CSP (header from the daemon, meta everywhere else). */
export function pdfViewerUrl(): string {
  return `${import.meta.env.BASE_URL}pdf-viewer.html`;
}

/** The sandboxed iframe. It is given no path, id or token: the host posts bytes to it once it says `ready`. */
export const PdfViewerFrame = forwardRef<HTMLIFrameElement, { className?: string }>(function PdfViewerFrame({ className }, ref) {
  return (
    <iframe
      ref={ref}
      title="PDF viewer"
      data-testid="pdf-viewer-frame"
      src={pdfViewerUrl()}
      sandbox={PDF_VIEWER_SANDBOX}
      referrerPolicy="no-referrer"
      className={className}
    />
  );
});
