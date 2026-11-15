import type { KeyboardEvent } from "react";

import { Icons } from "@/lib/icons";
import type { ContextChip } from "@/lib/send-set";

/** What the remove control says for a chip (per kind), so a screen reader hears what goes away. */
export function removeLabel(chip: ContextChip): string {
  if (chip.kind === "file_ref") return "Remove file reference";
  return chip.blockLabel === "selection" ? "Remove terminal selection" : "Remove terminal output";
}

function ChipPill({ chip, onRemove }: { chip: ContextChip; onRemove: () => void }) {
  function handleKeyDown(e: KeyboardEvent<HTMLDivElement>) {
    // Keys typed into the close badge itself (Enter/Space) keep their default; only the pill's own keys act here.
    if (e.target !== e.currentTarget) return;
    if (e.key === "Backspace" || e.key === "Delete") {
      e.preventDefault();
      onRemove();
    }
  }

  return (
    <div
      role="group"
      tabIndex={0}
      aria-label={chip.label}
      data-testid="context-chip"
      data-kind={chip.kind}
      onKeyDown={handleKeyDown}
      className="group relative h-8 max-w-64 shrink-0 rounded-lg outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
    >
      <div className="flex h-8 min-w-0 items-center gap-2 rounded-lg border border-border bg-surface pl-2.5 pr-1">
        {chip.kind === "terminal" ? (
          <Icons.terminal aria-hidden className="size-4 shrink-0 text-foreground-muted" />
        ) : (
          <Icons.file aria-hidden className="size-4 shrink-0 text-foreground-muted" />
        )}
        <span className="truncate text-ui-sm text-foreground" title={chip.label}>
          {chip.label}
        </span>
        {/* Always visible and always reachable: touch and keyboard users have no hover. 24px hit target. */}
        <button
          type="button"
          aria-label={removeLabel(chip)}
          data-testid="context-chip-close"
          onClick={(e) => {
            e.stopPropagation();
            onRemove();
          }}
          className="flex size-6 shrink-0 items-center justify-center rounded-md text-foreground-muted hover:bg-hover hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50"
        >
          <Icons.close aria-hidden className="size-3" />
        </button>
      </div>
    </div>
  );
}

/**
 * The composer's context chips (DT3-D AC14): the send set's terminal texts and file references as
 * removable pills in the attachment tray's look, above the textarea. Nothing is rendered while empty.
 */
export function ContextChipTray({ chips, onRemove }: { chips: ContextChip[]; onRemove: (localId: string) => void }) {
  if (chips.length === 0) return null;
  return (
    <div data-testid="context-chip-tray" role="list" aria-label="Context" className="flex flex-wrap gap-2 px-3 pt-3">
      {chips.map((chip) => (
        <div key={chip.localId} role="listitem">
          <ChipPill chip={chip} onRemove={() => onRemove(chip.localId)} />
        </div>
      ))}
    </div>
  );
}
