import type { DiffOutputFormat } from "@/components/diff-render";

/** Per-file collapsible list (today's view) vs one render of the whole task diff. */
export type DiffViewMode = "by-file" | "whole";

export interface DiffPrefs {
  view: DiffViewMode;
  format: DiffOutputFormat;
}

export const DIFF_PREFS_STORAGE_KEY = "smind.diff-prefs";

const DEFAULTS: DiffPrefs = { view: "by-file", format: "side-by-side" };

/**
 * The diff pane's two view toggles (ui-redesign-parity plan, Item 19),
 * persisted so they survive the pane unmounting on every tab switch --
 * a toggle that silently resets the moment you look at a file is worse
 * than no toggle.
 *
 * localStorage, and app-wide rather than per-task: this is a reading
 * preference about the person, not state about the task. Item 13's
 * settings screen is where it should eventually be *surfaced*; this is
 * the same client-side-until-proven-otherwise stance the plan takes for
 * every other preference.
 */
export function loadDiffPrefs(): DiffPrefs {
  try {
    const raw = window.localStorage.getItem(DIFF_PREFS_STORAGE_KEY);
    if (!raw) return DEFAULTS;
    const parsed = JSON.parse(raw) as Partial<DiffPrefs> | null;
    return {
      view: parsed?.view === "whole" || parsed?.view === "by-file" ? parsed.view : DEFAULTS.view,
      format:
        parsed?.format === "line-by-line" || parsed?.format === "side-by-side" ? parsed.format : DEFAULTS.format,
    };
  } catch {
    return DEFAULTS;
  }
}

export function saveDiffPrefs(prefs: DiffPrefs): void {
  try {
    window.localStorage.setItem(DIFF_PREFS_STORAGE_KEY, JSON.stringify(prefs));
  } catch {
    // Private-mode/quota failures must not break the toggle itself.
  }
}
