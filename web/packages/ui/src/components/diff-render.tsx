import { useEffect, useRef } from "react";
import { Diff2HtmlUI } from "diff2html/lib/ui/js/diff2html-ui.js";
import "diff2html/bundles/css/diff2html.min.css";
import "highlight.js/styles/github.css";

import { resolveDiffLine, type DiffLineRef } from "@/lib/diff-lines";

/** Unified (one column) vs side-by-side (two) -- diff2html's own `outputFormat` values, kept verbatim so there's no second vocabulary to translate. */
export type DiffOutputFormat = "line-by-line" | "side-by-side";

/**
 * One diff2html render, shared by the per-file rows and the whole-diff
 * view (ui-redesign-parity plan, Item 19) -- previously this effect lived
 * inline in diff-viewer-pane.tsx's FileRow and would have had to be
 * duplicated for the whole-diff view.
 *
 * diff2html writes to `innerHTML`, so per-line UI cannot be React
 * children of it. `onSelectLine` is therefore one delegated listener on
 * the container, resolved by lib/diff-lines.ts -- see that module for the
 * DOM shapes it reads. It stays in a ref so changing the handler doesn't
 * force a re-render of the (potentially large) diff HTML.
 *
 * Dark mode needs nothing here: index.css overrides diff2html's *base*
 * variables to point at smind's tokens (docs/design.md §2).
 */
export function DiffRender({
  diff,
  outputFormat,
  onSelectLine,
  testId,
}: {
  diff: string;
  outputFormat: DiffOutputFormat;
  /**
   * Called with the clicked diff line, or not at all when the click didn't
   * land on one (a hunk header, filler row, or the gap between rows). The
   * clicked element is passed along too, so a caller rendering *several*
   * files in one go (the whole-diff view) can recover which file the row
   * belongs to -- there's no React parent per file to read it from.
   */
  onSelectLine?: (line: DiffLineRef, element: Element) => void;
  testId?: string;
}) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const onSelectLineRef = useRef(onSelectLine);
  onSelectLineRef.current = onSelectLine;

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    el.innerHTML = "";
    if (!diff) return;
    const ui = new Diff2HtmlUI(el, diff, {
      outputFormat,
      drawFileList: false,
      matching: "lines",
      highlight: true,
    });
    ui.draw();
    ui.highlightCode();
  }, [diff, outputFormat]);

  return (
    <div
      ref={containerRef}
      data-testid={testId}
      data-output-format={outputFormat}
      onClick={(e) => {
        const handler = onSelectLineRef.current;
        if (!handler) return;
        const element = e.target as Element;
        const line = resolveDiffLine(element);
        if (line) handler(line, element);
      }}
    />
  );
}
