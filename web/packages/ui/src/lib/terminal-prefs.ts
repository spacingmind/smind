export const TERMINAL_SCROLLBACK_STORAGE_KEY = "smind.terminal-scrollback";

/** xterm.js's own default. Stated here rather than left implicit, so the setting's "reset to default" is a real value. */
export const DEFAULT_TERMINAL_SCROLLBACK = 1000;

const MIN_SCROLLBACK = 100;
const MAX_SCROLLBACK = 100_000;

/**
 * How many lines of scrollback a terminal keeps
 * (ui-redesign-parity plan, Item 20).
 *
 * Stored client-side, like every other preference in this plan. Item 13's
 * settings screen is where it should be *surfaced* -- this module is what
 * that screen reads and writes; the terminal pane only honors it. A bare
 * scrollback control in the terminal's own header would be exactly the
 * ad-hoc, un-grouped setting Item 13 exists to collect.
 *
 * Clamped on read: a hand-edited or corrupted value must not hand xterm a
 * negative buffer size or a memory-eating one.
 */
export function loadTerminalScrollback(): number {
  try {
    const raw = window.localStorage.getItem(TERMINAL_SCROLLBACK_STORAGE_KEY);
    if (raw === null) return DEFAULT_TERMINAL_SCROLLBACK;
    const value = Number(raw);
    if (!Number.isFinite(value)) return DEFAULT_TERMINAL_SCROLLBACK;
    return Math.min(MAX_SCROLLBACK, Math.max(MIN_SCROLLBACK, Math.round(value)));
  } catch {
    return DEFAULT_TERMINAL_SCROLLBACK;
  }
}

export function saveTerminalScrollback(lines: number): void {
  try {
    window.localStorage.setItem(
      TERMINAL_SCROLLBACK_STORAGE_KEY,
      String(Math.min(MAX_SCROLLBACK, Math.max(MIN_SCROLLBACK, Math.round(lines)))),
    );
  } catch {
    // Private-mode/quota failures must not break the terminal.
  }
}
