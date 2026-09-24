import { useCallback, useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { useKeyboard } from "@/keyboard/keyboard-provider";
import { filterShortcutHelpSections } from "@/keyboard/shortcut-help-search";
import { comboStringFromEvent } from "@/keyboard/shortcut-string";
import { conflictingBindings, helpSections, UNASSIGNED } from "@/keyboard/shortcuts";

/**
 * The grouped, searchable, rebindable binding list -- what the Settings
 * screen's Shortcuts section (`components/settings/shortcuts-section.tsx`)
 * renders. This used to be a standalone `Shift+?` dialog; moving rebinding
 * into Settings (AC5 of `docs/plans/active/web-keyboard-tabs.md`) folded
 * the dialog into this file's `<ShortcutRows />`, which is now the whole of
 * what this file exports.
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
          className="rounded border border-border bg-surface px-1.5 py-0.5 font-mono text-ui-xs text-foreground"
        >
          {part}
        </kbd>
      ))}
    </span>
  );
}

/**
 * One binding's row.
 *
 * "Change" captures a single key press and commits immediately -- the
 * overwhelming common case, and unchanged from before chords existed.
 * "Record chord…" instead captures key press after key press, each
 * appended as a further step, until Enter commits the whole sequence or
 * Escape cancels it; the first Escape-cancels-immediately shortcut Change
 * offers isn't available there, since Escape is itself a legitimate chord
 * step and a binding whose combo is Escape must stay reachable. Committing
 * on an explicit Enter rather than a pause avoids needing a real timer at
 * all here (a fixed pause long enough for a deliberate multi-key chord
 * would also be long enough to make a single-key rebind feel laggy) --
 * unlike the live matcher's own {@link CHORD_TIMEOUT_MS}, which times a
 * chord the user is trying to *use*, not one they're *recording*.
 */
function ShortcutRow({
  row,
  capturing,
  onStartCapture,
  onStartChordCapture,
  onCancelCapture,
  onRebind,
  onReset,
  conflictLabel,
}: {
  row: ReturnType<typeof helpSections>[number]["rows"][number];
  capturing: false | "single" | "chord";
  onStartCapture: () => void;
  onStartChordCapture: () => void;
  onCancelCapture: () => void;
  onRebind: (combo: string) => void;
  onReset: () => void;
  conflictLabel: string | null;
}) {
  useEffect(() => {
    if (!capturing) return;

    const steps: string[] = [];

    function onKeyDown(event: KeyboardEvent) {
      event.preventDefault();
      event.stopPropagation();
      if (event.key === "Escape" && (capturing === "single" || steps.length === 0)) {
        onCancelCapture();
        return;
      }
      if (capturing === "chord" && event.key === "Enter") {
        if (steps.length > 0) onRebind(steps.join(" "));
        return;
      }
      const combo = comboStringFromEvent(event);
      // A bare modifier press (Shift alone, on the way to Shift+K) yields
      // null -- stay in capture rather than treating it as a failed step.
      if (combo === null) return;
      if (capturing === "single") {
        onRebind(combo);
        return;
      }
      steps.push(combo);
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
        <div className="truncate text-ui-sm">{row.label}</div>
        {row.note && <div className="truncate text-ui-xs text-foreground-muted">{row.note}</div>}
        {conflictLabel && (
          <div className="text-ui-xs text-warning" data-testid="shortcut-conflict">
            Also used by {conflictLabel}
          </div>
        )}
      </div>
      <div className="flex shrink-0 items-center gap-2">
        {capturing === "chord" ? (
          <span className="text-ui-xs text-foreground-muted" data-testid="shortcut-capturing">
            Recording a chord… (Enter to save, Esc to cancel)
          </span>
        ) : capturing === "single" ? (
          <span className="text-ui-xs text-foreground-muted" data-testid="shortcut-capturing">
            Press a key… (Esc to cancel)
          </span>
        ) : row.keys === null ? (
          <span className="text-ui-xs text-foreground-muted" data-testid="shortcut-unassigned">
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
        {!capturing && (
          <Button
            variant="ghost"
            size="xs"
            onClick={onStartChordCapture}
            aria-label={`Record a chord for ${row.label}`}
          >
            Record chord…
          </Button>
        )}
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

/**
 * The grouped binding list. Exported so the settings screen's Shortcuts
 * section (`components/settings/shortcuts-section.tsx`) can embed it
 * without a dialog around it, narrowed by that section's own search box.
 */
export function ShortcutRows({ query = "" }: { query?: string } = {}) {
  const { bindings, isMac, overrides, rebind, resetBinding, resetAllBindings } = useKeyboard();
  const [capturing, setCapturing] = useState<{ id: string; mode: "single" | "chord" } | null>(null);

  const sections = filterShortcutHelpSections(helpSections(bindings, isMac), query);

  const handleRebind = useCallback(
    (bindingId: string, combo: string) => {
      rebind(bindingId, combo);
      setCapturing(null);
    },
    [rebind],
  );

  return (
    <div className="flex flex-col gap-4">
      {sections.length === 0 ? (
        <p className="text-ui-sm text-foreground-muted" data-testid="shortcut-sections-empty">
          No shortcuts match "{query}".
        </p>
      ) : (
        <div className="flex flex-col gap-4" data-testid="shortcut-sections">
          {sections.map((section) => (
            <section key={section.id} data-testid={`shortcut-section-${section.id}`}>
              <h3 className="mb-1 text-ui-sm font-medium tracking-wide text-foreground-muted uppercase">
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
                      capturing={capturing?.id === row.id ? capturing.mode : false}
                      onStartCapture={() => setCapturing({ id: row.id, mode: "single" })}
                      onStartChordCapture={() => setCapturing({ id: row.id, mode: "chord" })}
                      onCancelCapture={() => setCapturing(null)}
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
      )}
      {Object.keys(overrides).length > 0 && (
        <div className="flex justify-end">
          <Button variant="ghost" size="sm" onClick={resetAllBindings}>
            Reset all to defaults
          </Button>
        </div>
      )}
    </div>
  );
}

export { UNASSIGNED };
