import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { Dialog, DialogContent, DialogDescription, DialogTitle } from "@/components/ui/dialog";
import { useKeyboard, useModalKeyboardLock } from "@/keyboard/keyboard-provider";
import { formatCombo, matchCombo } from "@/keyboard/shortcut-string";
import { filterCommands, toRows } from "@/palette/commands";
import { usePalette, useRegisteredCommands } from "@/palette/palette-provider";

/**
 * The command palette (`Mod+K`): one searchable list over every registered
 * command source.
 *
 * This component contains no commands. It renders what
 * `palette/palette-provider.tsx` has registered, which is what lets a
 * later item add entries without editing this file.
 */

/** The current shortcut for `action`, formatted for this platform, or null when it has none. */
function useShortcutLabel(): (action: string | undefined) => string | null {
  const { bindings, isMac } = useKeyboard();
  return useCallback(
    (action) => {
      if (action === undefined) return null;
      const binding = bindings.find((b) => b.action === action);
      if (!binding?.effectiveCombo) return null;
      return formatCombo(binding.effectiveCombo, isMac);
    },
    [bindings, isMac],
  );
}

export function CommandPalette() {
  const { open, setOpen } = usePalette();
  const commands = useRegisteredCommands();
  const shortcutLabel = useShortcutLabel();
  useModalKeyboardLock(open);

  const [query, setQuery] = useState("");
  const [highlight, setHighlight] = useState(0);

  // The element focus returns to on close. Captured when the palette
  // opens rather than left to Radix: the palette is opened by a global
  // shortcut, not by a trigger element, and Radix's focus restoration is
  // built around having had a trigger.
  const restoreFocusTo = useRef<HTMLElement | null>(null);

  const rows = useMemo(() => toRows(filterCommands(commands, query)), [commands, query]);

  useEffect(() => {
    if (!open) return;
    restoreFocusTo.current =
      document.activeElement instanceof HTMLElement ? document.activeElement : null;
    setQuery("");
    setHighlight(0);
    return () => {
      restoreFocusTo.current?.focus();
      restoreFocusTo.current = null;
    };
  }, [open]);

  // Typing narrows the list under the highlight, so clamp rather than
  // leaving it pointing past the end (where Enter would do nothing).
  useEffect(() => {
    setHighlight((current) => (current >= rows.length ? 0 : current));
  }, [rows.length]);

  const move = useCallback(
    (delta: 1 | -1) => {
      if (rows.length === 0) return;
      // Wraps at both ends. Headings are a property of a row, never a row
      // of their own, so there is nothing to skip over.
      setHighlight((current) => (current + delta + rows.length) % rows.length);
    },
    [rows.length],
  );

  const runHighlighted = useCallback(() => {
    const row = rows[highlight];
    if (!row) return;
    // Close first: a command that opens another dialog (accounts, new
    // task) would otherwise mount it underneath an open palette, and the
    // palette's own modal keyboard lock would still be held.
    setOpen(false);
    row.command.run();
  }, [rows, highlight, setOpen]);

  const { bindings, isMac } = useKeyboard();

  function onInputKeyDown(event: React.KeyboardEvent<HTMLInputElement>) {
    // The palette holds the modal keyboard lock, so the global dispatcher
    // is deliberately silent while it's open -- which would otherwise make
    // Mod+K a one-way door. Re-checking just this one binding here keeps
    // the toggle rebinding-aware without reopening the whole registry to
    // a surface that owns its keyboard.
    const paletteBinding = bindings.find((b) => b.action === "palette.open");
    const paletteCombo = paletteBinding?.parsed?.length === 1 ? paletteBinding.parsed[0] : undefined;
    if (paletteCombo && matchCombo(paletteCombo, event, isMac)) {
      event.preventDefault();
      setOpen(false);
      return;
    }

    switch (event.key) {
      case "ArrowDown":
        event.preventDefault();
        move(1);
        break;
      case "ArrowUp":
        event.preventDefault();
        move(-1);
        break;
      case "Enter":
        event.preventDefault();
        runHighlighted();
        break;
      case "Escape":
        event.preventDefault();
        setOpen(false);
        break;
      default:
        break;
    }
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent
        className="top-[15%] max-h-[70svh] translate-y-0 gap-0 overflow-hidden p-0 sm:max-w-xl"
        showCloseButton={false}
        data-testid="command-palette"
      >
        {/* Radix requires a title and description for an accessible dialog; the palette's own chrome is the input, so both are screen-reader only. */}
        <DialogTitle className="sr-only">Command palette</DialogTitle>
        <DialogDescription className="sr-only">
          Search tasks, files and actions. Arrow keys to move, Enter to run, Escape to close.
        </DialogDescription>

        <input
          autoFocus
          aria-label="Command palette search"
          aria-controls="command-palette-list"
          aria-activedescendant={rows[highlight] ? `palette-row-${rows[highlight].command.key}` : undefined}
          role="combobox"
          aria-expanded
          data-testid="command-palette-input"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          onKeyDown={onInputKeyDown}
          placeholder="Search tasks, files and actions…"
          className="w-full border-b border-border bg-transparent px-4 py-3 text-ui-base outline-none placeholder:text-foreground-muted"
        />

        <div
          id="command-palette-list"
          role="listbox"
          className="max-h-96 overflow-y-auto p-1"
          data-testid="command-palette-list"
        >
          {rows.length === 0 ? (
            <div className="px-3 py-6 text-center text-ui-base text-foreground-muted" data-testid="command-palette-empty">
              No matching commands
            </div>
          ) : (
            rows.map((row, index) => {
              const keys = shortcutLabel(row.command.action);
              return (
                <div key={row.command.key}>
                  {row.groupStart !== null && (
                    <div
                      className="px-3 pt-3 pb-1 text-ui-sm font-medium tracking-wide text-foreground-muted uppercase"
                      data-testid="command-palette-group"
                    >
                      {row.groupStart}
                    </div>
                  )}
                  <div
                    id={`palette-row-${row.command.key}`}
                    role="option"
                    aria-selected={index === highlight}
                    data-testid="command-palette-row"
                    data-command-id={row.command.key}
                    // Highlight follows the pointer, the same as the
                    // keyboard -- one highlight, so Enter always runs the
                    // row that looks selected.
                    onMouseMove={() => setHighlight(index)}
                    onClick={() => {
                      setHighlight(index);
                      setOpen(false);
                      row.command.run();
                    }}
                    className={`flex cursor-pointer items-center justify-between gap-3 rounded px-3 py-2 text-ui-base ${
                      index === highlight ? "bg-selected text-foreground" : "text-foreground"
                    }`}
                  >
                    <span className="min-w-0">
                      <span className="block truncate">{row.command.title}</span>
                      {row.command.subtitle && (
                        <span className="block truncate text-ui-sm text-foreground-muted">
                          {row.command.subtitle}
                        </span>
                      )}
                    </span>
                    {keys && (
                      <kbd className="shrink-0 rounded border border-border bg-surface px-1.5 py-0.5 font-mono text-ui-xs text-foreground-muted">
                        {keys}
                      </kbd>
                    )}
                  </div>
                </div>
              );
            })
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
