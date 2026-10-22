import { useEffect, useRef, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { Diff2HtmlUI } from "diff2html/lib/ui/js/diff2html-ui.js";
import "diff2html/bundles/css/diff2html.min.css";

import { findAnnotationRow, mountAnnotationRow, type AnnotationTarget, type MountedAnnotation } from "@/lib/diff-annotations";
import { parseHunkHeader, resolveDiffLine, type DiffLineRef, type HunkRange } from "@/lib/diff-lines";

/** Unified (one column) vs side-by-side (two) -- diff2html's own `outputFormat` values, kept verbatim so there's no second vocabulary to translate. */
export type DiffOutputFormat = "line-by-line" | "side-by-side";

/** Something shown inline, directly under a diff line (a review comment or its composer). */
export interface DiffAnnotation extends AnnotationTarget {
  id: string;
  node: ReactNode;
}

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
  annotations,
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
  /**
   * Inline annotations: each is rendered, through a portal, into a row inserted directly under its line, and in
   * a side-by-side render the other side gets a spacer so the two stay aligned. One that cannot be placed (its line
   * is not in this diff any more) is shown after the diff instead, so it never silently disappears.
   */
  annotations?: DiffAnnotation[];
  testId?: string;
}) {
  const [hunkHosts, setHunkHosts] = useState<{ host: HTMLElement; hunk: HunkRange }[]>([]);
  const wantsHunks = renderHunkAction !== undefined;
  const containerRef = useRef<HTMLDivElement | null>(null);
  const onSelectLineRef = useRef(onSelectLine);
  onSelectLineRef.current = onSelectLine;

  // Inline annotations: `mounted` is what is in the DOM, `hosts`/`unplaced` its React-visible mirror.
  const annotationsRef = useRef(annotations);
  annotationsRef.current = annotations;
  const formatRef = useRef(outputFormat);
  formatRef.current = outputFormat;
  const mounted = useRef(new Map<string, MountedAnnotation>());
  const [hosts, setHosts] = useState<Record<string, HTMLElement>>({});
  const [unplaced, setUnplaced] = useState<DiffAnnotation[]>([]);

  const syncAnnotations = useRef(() => {
    const el = containerRef.current;
    const wanted = annotationsRef.current ?? [];
    const live = mounted.current;
    for (const [id, m] of live) {
      if (!wanted.some((a) => a.id === id)) {
        m.remove();
        live.delete(id);
      }
    }
    if (el) {
      for (const a of wanted) {
        if (live.has(a.id)) continue;
        const row = findAnnotationRow(el, formatRef.current, a);
        if (row) live.set(a.id, mountAnnotationRow(row, formatRef.current));
      }
    }
    const nextHosts: Record<string, HTMLElement> = {};
    for (const [id, m] of live) nextHosts[id] = m.host;
    const nextUnplaced = wanted.filter((a) => !live.has(a.id));
    setHosts((prev) => (sameHosts(prev, nextHosts) ? prev : nextHosts));
    setUnplaced((prev) => (prev.length === nextUnplaced.length && prev.every((a, i) => a === nextUnplaced[i]) ? prev : nextUnplaced));
  }).current;

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    for (const m of mounted.current.values()) m.remove();
    mounted.current.clear();
    el.innerHTML = "";
    setHunkHosts([]);
    if (!diff) {
      syncAnnotations();
      return;
    }
    const ui = new Diff2HtmlUI(el, diff, {
      outputFormat,
      drawFileList: false,
      matching: "lines",
      highlight: true,
    });
    ui.draw();
    ui.highlightCode();
    if (wantsHunks) setHunkHosts(mountHunkHosts(el, outputFormat));
    syncAnnotations();
  }, [diff, outputFormat, wantsHunks, syncAnnotations]);

  // Annotations added or removed without a redraw: a comment is saved, removed, or the composer opens on another line.
  useEffect(() => {
    syncAnnotations();
  }, [annotations, syncAnnotations]);

  return (
    <>
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
      {annotations?.map((a) => (hosts[a.id] ? createPortal(a.node, hosts[a.id]!, a.id) : null))}
    </div>
    {unplaced.length > 0 && (
      <div data-testid="diff-unplaced-annotations" className="mt-2 space-y-2">
        {unplaced.map((a) => (
          <div key={a.id}>{a.node}</div>
        ))}
      </div>
    )}
    </>
  );
}

function sameHosts(a: Record<string, HTMLElement>, b: Record<string, HTMLElement>): boolean {
  const ak = Object.keys(a);
  return ak.length === Object.keys(b).length && ak.every((k) => a[k] === b[k]);
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
