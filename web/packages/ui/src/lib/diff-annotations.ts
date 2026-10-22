import type { DiffOutputFormat } from "@/components/diff-render";
import { resolveDiffLine } from "@/lib/diff-lines";

/**
 * Where an inline annotation (a review comment or its composer) goes in a rendered diff. diff2html owns the diff's DOM
 * (React cannot be a child of it), so an annotation is a host element inserted as a table row right under its line and
 * rendered into through a portal (components/diff-render.tsx). These helpers find the row and build the host.
 */
export interface AnnotationTarget {
  /** Whole-diff render only: the file whose wrapper to search (matched on diff2html's own file name). */
  path?: string;
  side: "old" | "new";
  /** Null for a line the diff does not number: such a target cannot be placed. */
  line: number | null;
  /** The line's own text, which disambiguates the same number seen in both tables of a side-by-side render. */
  text: string;
}

/** A host row mounted into the diff; `remove()` takes it (and its side-by-side spacer) back out. */
export interface MountedAnnotation {
  host: HTMLElement;
  remove: () => void;
}

const ANNOTATION_ROW = "smind-annotation-row";

function wrapperFor(root: HTMLElement, path: string | undefined): HTMLElement | null {
  if (path === undefined) return root;
  for (const wrapper of root.querySelectorAll<HTMLElement>(".d2h-file-wrapper")) {
    if (wrapper.querySelector(".d2h-file-name")?.textContent?.trim() === path) return wrapper;
  }
  // A single-file render has one wrapper whose name may differ from the row's path (renames): trust the container.
  return root.querySelectorAll(".d2h-file-wrapper").length <= 1 ? root : null;
}

function realRows(table: ParentNode): HTMLTableRowElement[] {
  return Array.from(table.querySelectorAll<HTMLTableRowElement>("tr")).filter((r) => !r.classList.contains(ANNOTATION_ROW));
}

/** The rows to search, in order: the table of the target's own side first (side-by-side), then the other one. */
function searchTables(scope: HTMLElement, format: DiffOutputFormat, side: "old" | "new"): ParentNode[] {
  if (format !== "side-by-side") return [scope];
  const tables = Array.from(scope.querySelectorAll<HTMLElement>(".d2h-file-side-diff"));
  if (tables.length < 2) return tables.length === 1 ? tables : [scope];
  return side === "old" ? [tables[0]!, tables[1]!] : [tables[1]!, tables[0]!];
}

/** The diff row a target sits on, or null when it is not in this render (a stale draft, a collapsed hunk, an unnumbered line). */
export function findAnnotationRow(root: HTMLElement, format: DiffOutputFormat, target: AnnotationTarget): HTMLTableRowElement | null {
  if (target.line === null) return null;
  const scope = wrapperFor(root, target.path);
  if (!scope) return null;
  for (const table of searchTables(scope, format, target.side)) {
    for (const row of realRows(table)) {
      const ref = resolveDiffLine(row);
      if (ref && ref.side === target.side && ref.line === target.line && ref.text === target.text) return row;
    }
  }
  return null;
}

/**
 * Inserts a host right under `row`. In a side-by-side render the other table gets a spacer row of the same height
 * (kept in step by a ResizeObserver) so the two sides stay aligned.
 */
export function mountAnnotationRow(row: HTMLTableRowElement, format: DiffOutputFormat): MountedAnnotation {
  const table = row.closest("table");
  const span = Math.max(row.cells.length, 1);
  const host = document.createElement("div");
  host.dataset.annotationHost = "";
  const own = makeRow(span, host);
  row.after(own);

  let spacer: HTMLTableRowElement | null = null;
  let observer: ResizeObserver | null = null;
  if (format === "side-by-side" && table) {
    const side = row.closest(".d2h-file-side-diff");
    const sides = side?.parentElement ? Array.from(side.parentElement.children).filter((c) => c.classList.contains("d2h-file-side-diff")) : [];
    const other = sides.find((s) => s !== side);
    if (other) {
      const index = realRows(table).indexOf(row);
      const twin = realRows(other)[index];
      if (twin) {
        const filler = document.createElement("div");
        spacer = makeRow(span, filler);
        twin.after(spacer);
        const sync = () => {
          filler.style.height = `${host.offsetHeight}px`;
        };
        sync();
        if (typeof ResizeObserver !== "undefined") {
          observer = new ResizeObserver(sync);
          observer.observe(host);
        }
      }
    }
  }
  return {
    host,
    remove: () => {
      observer?.disconnect();
      own.remove();
      spacer?.remove();
    },
  };
}

function makeRow(span: number, content: HTMLElement): HTMLTableRowElement {
  const tr = document.createElement("tr");
  tr.className = ANNOTATION_ROW;
  const td = document.createElement("td");
  td.colSpan = span;
  td.style.padding = "0";
  td.appendChild(content);
  tr.appendChild(td);
  return tr;
}
