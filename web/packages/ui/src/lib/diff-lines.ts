/** One line of a rendered diff, as identified by a click on it. `line` is null for a line the diff doesn't number (a filler row on the opposite side of a side-by-side change). */
export interface DiffLineRef {
  side: "old" | "new";
  line: number | null;
  /** The line's own source text, carried into the review draft so the submitted prompt can quote what the comment is about. */
  text: string;
}

/**
 * Resolves a click inside diff2html's output to the diff line it landed
 * on (ui-redesign-parity plan, Item 19's per-line review comments).
 *
 * diff2html renders to `innerHTML`, so there is no React tree to hang a
 * per-line handler off -- the pane instead puts one listener on the
 * container and asks this function what was clicked. Both output formats
 * are handled, because the side-by-side/unified toggle is in the same
 * item:
 *
 * - unified (`line-by-line`): the number cell holds a `.line-num1` (old)
 *   and `.line-num2` (new) div.
 * - side-by-side: the number cell (`.d2h-code-side-linenumber`) holds the
 *   number as bare text, and which side you're on is decided by the row's
 *   own del/ins class rather than by which of the two tables it's in --
 *   the class is what's actually authoritative about the line's nature.
 *
 * Returns null for anything that isn't a code line (a `@@` hunk header,
 * an empty filler row, the file header, whitespace between rows), so the
 * caller can treat "no line here" and "not a diff" identically.
 */
export function resolveDiffLine(target: Element | null): DiffLineRef | null {
  const row = target?.closest("tr");
  if (!row) return null;

  const content = row.querySelector(".d2h-code-line-ctn");
  if (!content) return null;

  const side: DiffLineRef["side"] = row.querySelector(".d2h-del") || row.classList.contains("d2h-del") ? "old" : "new";

  const num1 = row.querySelector(".line-num1")?.textContent?.trim();
  const num2 = row.querySelector(".line-num2")?.textContent?.trim();
  const raw =
    num1 !== undefined || num2 !== undefined
      ? (side === "old" ? num1 : num2)
      : row.querySelector(".d2h-code-side-linenumber")?.textContent?.trim();

  return {
    side,
    line: raw && /^\d+$/.test(raw) ? Number(raw) : null,
    text: content.textContent ?? "",
  };
}

/**
 * The file a clicked row belongs to, read out of diff2html's own file
 * header (`.d2h-file-wrapper` > `.d2h-file-name`). Only the whole-diff
 * view needs this -- the per-file view already knows its path from React
 * -- but a single render covering every file has no other way back to it.
 *
 * Returns null when the click wasn't inside a file wrapper (or the render
 * carried no filename), so the caller can skip rather than guess.
 */
export function filePathForElement(target: Element | null): string | null {
  const name = target?.closest(".d2h-file-wrapper")?.querySelector(".d2h-file-name")?.textContent?.trim();
  return name || null;
}

/** A hunk's new-side line range; both absent when the hunk has no new-side lines (a pure deletion). */
export interface HunkRange {
  line?: number;
  endLine?: number;
}

/** The new-side range of a `@@ -a,b +c,d @@` header (`+c,d` → `c` to `c+d-1`; a missing count is 1); null when the text is not a hunk header. */
export function parseHunkHeader(text: string): HunkRange | null {
  const m = /^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@/.exec(text.trim());
  if (!m) return null;
  const start = Number(m[1]);
  const count = m[2] === undefined ? 1 : Number(m[2]);
  if (count === 0 || start < 1) return {};
  return count === 1 ? { line: start } : { line: start, endLine: start + count - 1 };
}
