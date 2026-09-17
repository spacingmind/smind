import { useCallback, useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { useKeyboard, useModalKeyboardLock } from "@/keyboard/keyboard-provider";
import { comboStringFromEvent, formatCombo } from "@/keyboard/shortcut-string";
import { conflictingBindings, helpSections, UNASSIGNED } from "@/keyboard/shortcuts";

/**
 * The shortcuts reference (`Shift+?`), and the place bindings are rebound.
 *
 * Rebinding lives here rather than waiting for Item 13's settings screen
 * for a plain reason: this is the surface someone is already looking at
 * when they decide a shortcut is wrong. Item 13 can embed the same
 * `<ShortcutRows />` when it lands.
 */

/** Renders a formatted combo as individual key caps, split on the platform's own separator. */
function KeyCaps({ keys }: { keys: string }) {
  // On mac `formatCombo` returns "⌘⌥T" (no separator, matching Apple's own
  // rendering); elsewhere "Ctrl+Alt+T". Splitting the mac form back into
  // caps would mean re-parsing glyphs, so mac gets one cap and the rest get
  // one per key -- which is what each platform's conventions look like
  // anyway.
  const parts = keys.includes("+") ? keys.split("+") : [keys];
  return (
    <span className="flex items-center gap-1">
      {parts.map((part, i) => (
        <kbd
          key={`${part}-${i}`}
          className="rounded border border-border bg-surface-2 px-1.5 py-0.5 font-mono text-xs text-foreground"
        >
          {part}
        </kbd>
      ))}
    </span>
  );
}

/**
 * One binding's row. While `capturing`, the next key press becomes the new
 * combo -- Escape cancels instead, since a binding whose combo is Escape
 * would otherwise be impossible to back out of.
 */
function ShortcutRow({
  row,
  capturing,
  onStartCapture,
  onCancelCapture,
  onRebind,
  onReset,
  conflictLabel,
}: {
  row: ReturnType<typeof helpSections>[number]["rows"][number];
  capturing: boolean;
  onStartCapture: () => void;
  onCancelCapture: () => void;
  onRebind: (combo: string) => void;
  onReset: () => void;
  conflictLabel: string | null;
}) {
  useEffect(() => {
    if (!capturing) return;

    function onKeyDown(event: KeyboardEvent) {
      event.preventDefault();
      event.stopPropagation();
      if (event.key === "Escape") {
        onCancelCapture();
        return;
      }
      const combo = comboStringFromEvent(event);
      // A bare modifier press (Shift alone, on the way to Shift+K) yields
      // null -- stay in capture rather than treating it as a failed attempt.
      if (combo !== null) onRebind(combo);
    }

    window.addEventListener("keydown", onKeyDown, true);
    return () => window.removeEventListener("keydown", onKeyDown, true);
  }, [capturing, onCancelCapture, onRebind]);

  return (
    <div
      className="flex items-center justify-between gap-3 py-1.5"
      data-testid="shortcut-row"
      data-binding-id={row.id}
    >
      <div className="min-w-0">
        <div className="truncate text-sm">{row.label}</div>
        {row.note && <div className="truncate text-xs text-foreground-muted">{row.note}</div>}
        {conflictLabel && (
          <div className="text-xs text-status-warning" data-testid="shortcut-conflict">
            Also used by {conflictLabel}
          </div>
        )}
      </div>
      <div className="flex shrink-0 items-center gap-2">
        {capturing ? (
          <span className="text-xs text-foreground-muted" data-testid="shortcut-capturing">
            Press a key… (Esc to cancel)
          </span>
        ) : row.keys === null ? (
          <span className="text-xs text-foreground-muted" data-testid="shortcut-unassigned">
            Unassigned
          </span>
        ) : (
          <KeyCaps keys={row.keys} />
        )}
        <Button
          variant="ghost"
          size="xs"
          onClick={capturing ? onCancelCapture : onStartCapture}
          aria-label={`${capturing ? "Cancel rebinding" : "Change shortcut for"} ${row.label}`}
        >
          {capturing ? "Cancel" : "Change"}
        </Button>
        {row.overridden && !capturing && (
          <Button
            variant="ghost"
            size="xs"
            onClick={onReset}
            aria-label={`Reset shortcut for ${row.label}`}
          >
            Reset
          </Button>
        )}
      </div>
    </div>
  );
}

/** The grouped binding list. Exported so Item 13's settings screen can embed it without a dialog around it. */
export function ShortcutRows() {
  const { bindings, isMac, rebind, resetBinding } = useKeyboard();
  const [capturingId, setCapturingId] = useState<string | null>(null);

  const sections = helpSections(bindings, isMac);

  const handleRebind = useCallback(
    (bindingId: string, combo: string) => {
      rebind(bindingId, combo);
      setCapturingId(null);
    },
    [rebind],
  );

  return (
    <div className="flex flex-col gap-4" data-testid="shortcut-sections">
      {sections.map((section) => (
        <section key={section.id} data-testid={`shortcut-section-${section.id}`}>
          <h3 className="mb-1 text-metadata-label tracking-wide text-foreground-muted uppercase">
            {section.title}
          </h3>
          <div className="divide-y divide-border">
            {section.rows.map((row) => {
              const effective = bindings.find((b) => b.id === row.id)?.effectiveCombo;
              const conflicts =
                effective === undefined || effective === null
                  ? []
                  : conflictingBindings(bindings, effective, row.id, isMac);
              return (
                <ShortcutRow
                  key={row.id}
                  row={row}
                  capturing={capturingId === row.id}
                  onStartCapture={() => setCapturingId(row.id)}
                  onCancelCapture={() => setCapturingId(null)}
                  onRebind={(combo) => handleRebind(row.id, combo)}
                  onReset={() => resetBinding(row.id)}
                  conflictLabel={conflicts.length > 0 ? conflicts.map((c) => c.label).join(", ") : null}
                />
              );
            })}
          </div>
        </section>
      ))}
    </div>
  );
}

export function ShortcutsDialog({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const { overrides, resetAllBindings, isMac } = useKeyboard();
  useModalKeyboardLock(open);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[80svh] overflow-y-auto sm:max-w-2xl" data-testid="shortcuts-dialog">
        <DialogHeader>
          <DialogTitle>Keyboard shortcuts</DialogTitle>
          <DialogDescription>
            Press {formatCombo("Shift+?", isMac)} any time to reopen this. Click Change on a row to
            rebind it.
          </DialogDescription>
        </DialogHeader>
        <ShortcutRows />
        {Object.keys(overrides).length > 0 && (
          <div className="flex justify-end">
            <Button variant="ghost" size="sm" onClick={resetAllBindings}>
              Reset all to defaults
            </Button>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

export { UNASSIGNED };
