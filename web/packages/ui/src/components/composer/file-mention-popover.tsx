import { FileIcon } from "@/lib/file-icons";
import type { RankedPath } from "@/lib/file-mention";
import { cn } from "@/lib/utils";

export const MENTION_LISTBOX_ID = "composer-mention-listbox";
export const mentionOptionId = (index: number) => `composer-mention-option-${index}`;

/** `text` (which starts at `offset` in the full path) with the characters in `indices` emphasised. */
function Emphasised({ text, offset, indices }: { text: string; offset: number; indices: Set<number> }) {
  const parts: { text: string; hit: boolean }[] = [];
  for (let i = 0; i < text.length; i++) {
    const hit = indices.has(offset + i);
    const last = parts[parts.length - 1];
    if (last && last.hit === hit) last.text += text[i];
    else parts.push({ text: text[i]!, hit });
  }
  return (
    <>
      {parts.map((p, i) =>
        p.hit ? (
          <span key={i} className="font-medium text-foreground">
            {p.text}
          </span>
        ) : (
          <span key={i}>{p.text}</span>
        ),
      )}
    </>
  );
}

/**
 * The `@` mention list (ADR-0038 §6): a listbox the composer keeps focus-neutral (the textarea stays the focused
 * element and points at the active row with aria-activedescendant). Shown and hidden instantly, never animated.
 */
export function FileMentionPopover({
  rows,
  activeIndex,
  loading,
  onPick,
  onHover,
}: {
  rows: RankedPath[];
  activeIndex: number;
  loading: boolean;
  onPick: (path: string) => void;
  onHover: (index: number) => void;
}) {
  return (
    <div
      id={MENTION_LISTBOX_ID}
      role="listbox"
      aria-label="Files"
      data-testid="file-mention-popover"
      className="absolute inset-x-0 bottom-full z-20 mb-1 max-h-60 overflow-auto rounded-lg border border-border bg-menu shadow-md"
    >
      {rows.length === 0 && (
        <p role="status" className="px-3 py-1.5 text-ui-sm text-foreground-muted">
          {loading ? "Searching…" : "No matching files"}
        </p>
      )}
      {rows.map((row, index) => {
        const slash = row.path.lastIndexOf("/");
        const dir = slash === -1 ? "" : row.path.slice(0, slash);
        const base = row.path.slice(slash + 1);
        const hits = new Set(row.indices);
        return (
          <div
            key={row.path}
            id={mentionOptionId(index)}
            role="option"
            aria-selected={index === activeIndex}
            // Keep the textarea focused: a click picks, it never moves focus.
            onMouseDown={(e) => e.preventDefault()}
            onClick={() => onPick(row.path)}
            onMouseMove={() => index !== activeIndex && onHover(index)}
            className={cn("flex cursor-pointer items-center gap-2 px-3 py-1.5 text-ui-sm", index === activeIndex && "bg-menu-hover")}
          >
            <FileIcon path={row.path} className="text-foreground-muted" />
            <span className="shrink-0 text-foreground">
              <Emphasised text={base} offset={slash + 1} indices={hits} />
            </span>
            {dir && (
              <span className="min-w-0 truncate text-foreground-muted" title={dir}>
                <Emphasised text={dir} offset={0} indices={hits} />
              </span>
            )}
          </div>
        );
      })}
    </div>
  );
}
