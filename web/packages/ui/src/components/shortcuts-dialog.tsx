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
          className="rounded-md border border-border bg-muted px-1.5 py-0.5 font-mono text-ui-xs"
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
    <tr
      data-testid="shortcut-row"
      data-binding-id={row.id}
      className="border-t border-border max-sm:grid max-sm:grid-cols-[minmax(0,1fr)_auto] max-sm:gap-x-2"
    >
      <td className="max-sm:col-start-1 max-sm:row-start-1 max-sm:pb-0 px-3 py-1.5">
        <div className="truncate text-ui-base">{row.label}</div>
        {row.note && <div className="truncate text-ui-sm text-foreground-muted">{row.note}</div>}
        {conflictLabel && (
          <div className="text-ui-sm text-warning" data-testid="shortcut-conflict">
            Also used by {conflictLabel}
          </div>
        )}
      </td>
      <td className="max-sm:col-start-1 max-sm:row-start-2 max-sm:pt-1 whitespace-nowrap px-3 py-1.5">
        {capturing === "chord" ? (
          <span className="text-ui-sm text-foreground-muted" data-testid="shortcut-capturing">
            Recording a chord… (Enter to save, Esc to cancel)
          </span>
        ) : capturing === "single" ? (
          <span className="text-ui-sm text-foreground-muted" data-testid="shortcut-capturing">
            Press a key… (Esc to cancel)
          </span>
        ) : row.keys === null ? (
          <span className="text-ui-sm text-foreground-muted" data-testid="shortcut-unassigned">
            Unassigned
          </span>
        ) : (
          <KeyCaps keys={row.keys} />
        )}
      </td>
      <td className="hidden whitespace-nowrap px-3 py-1.5 text-ui-sm text-foreground-muted sm:table-cell">Global</td>
      <td className="max-sm:col-start-2 max-sm:row-start-1 max-sm:row-span-2 max-sm:self-center px-3 py-1.5">
        <div className="flex flex-wrap items-center justify-end gap-2">
          <Button
            variant="ghost"
            size="xs"
            className="whitespace-nowrap"
            onClick={capturing ? onCancelCapture : onStartCapture}
            aria-label={`${capturing ? "Cancel rebinding" : "Change shortcut for"} ${row.label}`}
          >
            {capturing ? "Cancel" : "Change"}
          </Button>
          {!capturing && (
            <Button
              variant="ghost"
              size="xs"
              className="whitespace-nowrap"
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
              className="whitespace-nowrap"
              onClick={onReset}
              aria-label={`Reset shortcut for ${row.label}`}
            >
              Reset
            </Button>
          )}
        </div>
      </td>
    </tr>
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
        <p className="text-ui-base text-foreground-muted" data-testid="shortcut-sections-empty">
          No shortcuts match "{query}".
        </p>
      ) : (
        <div
          className="overflow-hidden rounded-xl border border-border bg-card"
          data-testid="shortcut-sections"
        >
          <div className="overflow-x-auto">
            <table className="max-sm:block w-full border-collapse text-left text-ui-base">
              <thead className="max-sm:sr-only">
                <tr className="border-b border-border">
                  <th scope="col" className="px-3 py-2 text-ui-sm font-medium text-foreground-muted">
                    Command
                  </th>
                  <th scope="col" className="px-3 py-2 text-ui-sm font-medium text-foreground-muted">
                    Keybinding
                  </th>
                  <th
                    scope="col"
                    className="hidden whitespace-nowrap px-3 py-2 text-ui-sm font-medium text-foreground-muted sm:table-cell"
                  >
                    Scope
                  </th>
                  <th scope="col" className="px-3 py-2 text-ui-sm font-medium text-foreground-muted">
                    Actions
                  </th>
                </tr>
              </thead>
              {sections.map((section) => (
                <tbody key={section.id} data-testid={`shortcut-section-${section.id}`} className="max-sm:block">
                  <tr className="max-sm:block">
                    <th
                      colSpan={4}
                      scope="colgroup"
                      className="bg-muted/40 px-4 py-1.5 text-ui-sm font-medium uppercase tracking-wide text-foreground-muted"
                    >
                      {section.title}
                    </th>
                  </tr>
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
                </tbody>
              ))}
            </table>
          </div>
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
