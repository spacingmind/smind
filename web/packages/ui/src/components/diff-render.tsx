import { useEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Diff2HtmlUI } from "diff2html/lib/ui/js/diff2html-ui.js";
import "diff2html/bundles/css/diff2html.min.css";

import { parseHunkHeader, resolveDiffLine, type DiffLineRef, type HunkRange } from "@/lib/diff-lines";

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
  renderHunkAction,
  hideFileHeader = false,
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
  /** Content appended to each `@@` hunk header row (the new-side range of that hunk); rendered into the row through a portal. */
  renderHunkAction?: (hunk: HunkRange) => ReactNode;
  /**
   * Hide diff2html's own per-file header (icon, name, CHANGED tag, "Viewed" box). The by-file view already has a row
   * header with the name, status and viewed toggle above each diff, so showing both read as two headers and two
   * "viewed" boxes per file. The whole-diff view has no row headers and keeps diff2html's. The `!` matters:
   * diff2html's stylesheet is unlayered, so it beats Tailwind's layered utilities unless this one is important.
   */
  hideFileHeader?: boolean;
  testId?: string;
}) {
  const [hunkHosts, setHunkHosts] = useState<{ host: HTMLElement; hunk: HunkRange }[]>([]);
  const wantsHunks = renderHunkAction !== undefined;
  const containerRef = useRef<HTMLDivElement | null>(null);
  const onSelectLineRef = useRef(onSelectLine);
  onSelectLineRef.current = onSelectLine;

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    el.innerHTML = "";
    setHunkHosts([]);
    if (!diff) return;
    const ui = new Diff2HtmlUI(el, diff, {
      outputFormat,
      drawFileList: false,
      matching: "lines",
      highlight: true,
    });
    ui.draw();
    ui.highlightCode();
    if (wantsHunks) setHunkHosts(mountHunkHosts(el, outputFormat));
  }, [diff, outputFormat, wantsHunks]);

  return (
    <div
      ref={containerRef}
      data-testid={testId}
      data-output-format={outputFormat}
      data-file-header={hideFileHeader ? "hidden" : "shown"}
      className={hideFileHeader ? "[&_.d2h-file-header]:hidden!" : undefined}
      onClick={(e) => {
        const handler = onSelectLineRef.current;
        if (!handler) return;
        const element = e.target as Element;
        const line = resolveDiffLine(element);
        if (line) handler(line, element);
      }}
    >
      {renderHunkAction && hunkHosts.map(({ host, hunk }, i) => createPortal(renderHunkAction(hunk), host, String(i)))}
    </div>
  );
}

/**
 * One empty host span at the end of each `@@` header row, for the hunk action. Side-by-side draws the header text on the
 * left side only (the right one is blank), so that is where the hunk's one button goes.
 */
function mountHunkHosts(container: HTMLElement, format: DiffOutputFormat): { host: HTMLElement; hunk: HunkRange }[] {
  const scope = format === "side-by-side" ? Array.from(container.querySelectorAll(".d2h-file-side-diff")).slice(0, 1) : [container];
  const out: { host: HTMLElement; hunk: HunkRange }[] = [];
  for (const root of scope) {
    for (const line of root.querySelectorAll<HTMLElement>("td.d2h-info > .d2h-code-line, td.d2h-info > .d2h-code-side-line")) {
      const hunk = parseHunkHeader(line.textContent ?? "");
      if (!hunk) continue;
      const host = document.createElement("span");
      host.className = "ml-2 inline-flex align-middle";
      host.dataset.hunkAction = "";
      // The whole header row reveals the button, not just the text span (which is only as wide as the header text).
      (line.closest("tr") ?? line).classList.add("group");
      line.appendChild(host);
      out.push({ host, hunk });
    }
  }
  return out;
}
