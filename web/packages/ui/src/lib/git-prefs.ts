/** Per-workspace git UI preferences, kept in localStorage (a reading/habit preference about the person, not task state). */
export const GIT_PREFS_STORAGE_KEY = "smind.git-prefs";

interface GitPrefs {
  /** workspace id -> whether "Draft" starts on in the Create PR dialog. */
  draftPr?: Record<string, boolean>;
}

function load(): GitPrefs {
  try {
    const parsed: unknown = JSON.parse(window.localStorage.getItem(GIT_PREFS_STORAGE_KEY) ?? "{}");
    return typeof parsed === "object" && parsed !== null ? (parsed as GitPrefs) : {};
  } catch {
    return {};
  }
}

/** Whether Create PR starts as a draft in this workspace; on until the person turns it off. */
export function loadDraftPr(workspaceId: number): boolean {
  const stored = load().draftPr?.[String(workspaceId)];
  return typeof stored === "boolean" ? stored : true;
}

export function saveDraftPr(workspaceId: number, draft: boolean): void {
  try {
    const prefs = load();
    window.localStorage.setItem(
      GIT_PREFS_STORAGE_KEY,
      JSON.stringify({ ...prefs, draftPr: { ...prefs.draftPr, [String(workspaceId)]: draft } }),
    );
  } catch {
    // Storage unavailable (private mode, quota): the choice just doesn't persist.
  }
}
