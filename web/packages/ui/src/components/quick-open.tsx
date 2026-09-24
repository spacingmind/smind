import { useEffect, useMemo, useRef, useState } from "react";

import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog";
import { InlineSpinner } from "@/components/ui/inline-spinner";
import { useTaskSearchIndex } from "@/hooks/use-task-search-index";
import { FileIcon } from "@/lib/file-icons";
import { fuzzyFilter, type FuzzyMatch } from "@/lib/fuzzy-match";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import type { Task } from "@/lib/types";

const MAX_RESULTS = 50;

/**
 * The fuzzy file switcher (ui-redesign-parity plan, Item 18): a dialog
 * over the task's whole-worktree path list
 * (hooks/use-task-search-index.ts), filtered client-side
 * (lib/fuzzy-match.ts) as the query changes. Paseo binds this to Cmd+P
 * (`audit-paseo.md` §7), wired through the `quick-open.open` action in
 * `keyboard/actions.ts`.
 *
 * A controlled dialog (`open`/`onOpenChange`), like FolderPickerDialog --
 * the caller owns when it's mounted/visible, this owns only what's inside
 * it.
 */
export function QuickOpen({
  client,
  task,
  open,
  onOpenChange,
  onOpenFile,
  events,
}: {
  client: { call<T>(method: string, params?: unknown): Promise<T> } | null;
  task: Task | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Called with the chosen worktree-relative path; the caller opens/activates its file tab. */
  onOpenFile: (path: string) => void;
  events?: DaemonEvents | null;
}) {
  const index = useTaskSearchIndex(client, task, events);
  const [query, setQuery] = useState("");
  const [activeIndex, setActiveIndex] = useState(0);
  const inputRef = useRef<HTMLInputElement | null>(null);

  // Fresh query and selection every time the dialog opens -- a stale
  // filter from the last time it was open would be surprising, and
  // "always start at the top match" is what makes Enter-without-arrowing
  // do the right thing.
  useEffect(() => {
    if (open) {
      setQuery("");
      setActiveIndex(0);
      // Radix focuses its own content root on open; grabbing the input
      // right after is what makes typing work without a manual click.
      const id = requestAnimationFrame(() => inputRef.current?.focus());
      return () => cancelAnimationFrame(id);
    }
  }, [open]);

  const matches = useMemo(
    () => fuzzyFilter(query, index.paths ?? [], MAX_RESULTS),
    [query, index.paths],
  );

  // The query changing invalidates whatever was "active" under the old
  // result set -- always re-land on the top match.
  useEffect(() => {
    setActiveIndex(0);
  }, [query]);

  function choose(match: FuzzyMatch | undefined): void {
    if (!match) return;
    onOpenFile(match.path);
    onOpenChange(false);
  }

  function handleKeyDown(e: React.KeyboardEvent<HTMLInputElement>): void {
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActiveIndex((i) => Math.min(i + 1, matches.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActiveIndex((i) => Math.max(i - 1, 0));
    } else if (e.key === "Enter") {
      e.preventDefault();
      choose(matches[activeIndex]);
    }
    // Escape is left to Radix's own Dialog -- it already closes on Escape
    // and returns focus to whatever opened it, which is exactly Item 18's
    // "Escape closes without opening" plus the palette's own focus rule.
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        showCloseButton={false}
        className="top-[20%] max-w-lg translate-y-0 gap-0 p-0"
        data-testid="quick-open"
        onOpenAutoFocus={(e) => {
          // Focus the query input, not Radix's own content root.
          e.preventDefault();
          inputRef.current?.focus();
        }}
      >
        {/* Visually hidden -- DialogContent requires an accessible name, and "Quick open" is announced without a visible header competing with the search input for the top slot. */}
        <DialogTitle className="sr-only">Quick open</DialogTitle>
        <input
          ref={inputRef}
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={handleKeyDown}
          placeholder="Go to file…"
          aria-label="Go to file"
          data-testid="quick-open-input"
          className="w-full border-b bg-transparent px-4 py-3 text-ui-sm outline-none placeholder:text-muted-foreground"
        />
        <div className="max-h-80 overflow-y-auto py-1" data-testid="quick-open-results">
          {index.loading && index.paths === null && <InlineSpinner label="Loading files…" className="px-4 py-3" />}
          {index.error && (
            <p className="px-4 py-3 text-ui-sm text-destructive" data-testid="quick-open-error">
              {index.error}
            </p>
          )}
          {!index.loading && index.paths !== null && matches.length === 0 && (
            <p className="px-4 py-3 text-ui-sm text-muted-foreground">No matching files</p>
          )}
          {matches.map((match, i) => (
            <button
              key={match.path}
              type="button"
              data-testid="quick-open-result"
              data-active={i === activeIndex}
              onMouseEnter={() => setActiveIndex(i)}
              onClick={() => choose(match)}
              className={
                "flex w-full items-center gap-2 truncate px-4 py-1.5 text-left text-ui-sm " +
                (i === activeIndex ? "bg-accent" : "")
              }
            >
              <FileIcon path={match.path} />
              <HighlightedPath match={match} />
            </button>
          ))}
        </div>
      </DialogContent>
    </Dialog>
  );
}

/** Renders a matched path with its matched characters bolded, per the plan's "typing a fuzzy query ranks the expected path first" scenario -- bolding is what lets a person confirm *why* a result ranked where it did. */
function HighlightedPath({ match }: { match: FuzzyMatch }) {
  if (match.indices.length === 0) return <span className="min-w-0 truncate">{match.path}</span>;

  const parts: { text: string; matched: boolean }[] = [];
  let cursor = 0;
  for (const index of match.indices) {
    if (index > cursor) parts.push({ text: match.path.slice(cursor, index), matched: false });
    parts.push({ text: match.path[index]!, matched: true });
    cursor = index + 1;
  }
  if (cursor < match.path.length) parts.push({ text: match.path.slice(cursor), matched: false });

  return (
    <span className="min-w-0 truncate">
      {parts.map((part, i) =>
        part.matched ? (
          <strong key={i} className="font-medium text-foreground">
            {part.text}
          </strong>
        ) : (
          <span key={i}>{part.text}</span>
        ),
      )}
    </span>
  );
}
