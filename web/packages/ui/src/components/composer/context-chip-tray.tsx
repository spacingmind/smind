import type { KeyboardEvent } from "react";

import { useCanHover } from "@/hooks/use-can-hover";
import { Icons } from "@/lib/icons";
import type { ContextChip } from "@/lib/send-set";
import { cn } from "@/lib/utils";

function ChipPill({ chip, onRemove }: { chip: ContextChip; onRemove: () => void }) {
  const canHover = useCanHover();

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
      <div className="flex h-8 min-w-0 items-center gap-2 rounded-lg border border-border bg-surface px-2.5">
        {chip.kind === "terminal" ? (
          <Icons.terminal aria-hidden className="size-4 shrink-0 text-foreground-muted" />
        ) : (
          <Icons.file aria-hidden className="size-4 shrink-0 text-foreground-muted" />
        )}
        <span className="truncate text-ui-sm text-foreground" title={chip.label}>
          {chip.label}
        </span>
      </div>
      <button
        type="button"
        tabIndex={canHover ? -1 : 0}
        aria-label={`Remove ${chip.label}`}
        data-testid="context-chip-close"
        onClick={(e) => {
          e.stopPropagation();
          onRemove();
        }}
        className={cn(
          "absolute -right-1.5 -top-1.5 flex size-4 items-center justify-center rounded-full border border-border bg-background text-foreground-muted transition-opacity hover:text-foreground focus-visible:opacity-100 focus-visible:ring-2 focus-visible:ring-ring/50",
          canHover ? "opacity-100 md:opacity-0 md:group-focus-within:opacity-100 md:group-hover:opacity-100" : "opacity-100",
        )}
      >
        <Icons.close aria-hidden className="size-2.5" />
      </button>
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
