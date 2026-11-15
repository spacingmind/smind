import { syncTitle } from "@/components/git/sync-dialog";
import type { TaskGit, TaskGitAction } from "@/lib/types";

/** One git action of the Diff header's menu. */
export interface GitActionEntry {
  id: "push" | "update" | "create-pr" | "update-pr" | "view-pr" | "archive";
  /** The menu label ("Publish branch", "Create PR…"). */
  label: string;
  enabled: boolean;
  /** The daemon's sentence when disabled (shown as plain text in the menu, never only in a tooltip). */
  reason?: string;
  /** The machine code behind `reason` (a forge-setup reason offers a link to Settings). */
  code?: string;
  /** view-pr: the PR's https URL. */
  url?: string;
}

/**
 * The git actions a task offers right now, in the order of the review-then-ship flow (decision 8 of chat-surface-ui): sync the
 * branch (Publish / Push, Update from base), then the PR step (Create PR, or Update PR + View PR once one is open), and once the
 * PR (or the branch) is merged only Archive and View PR. An action the daemon does not report (an older daemon) is left out
 * rather than shown dead. `[]` while the git state is still unknown.
 */
export function gitActionEntries(git: TaskGit | undefined, opts: { updatingPr?: boolean } = {}): GitActionEntry[] {
  if (!git) return [];
  const base = git.base?.known ? (git.base.ref ?? "") : "";
  const published = git.origin?.published ?? false;
  const merged = git.pr?.state === "merged" || git.merged?.state === "merged";
  const pr = git.pr?.state === "open" ? git.pr : undefined;
  const prUrl = (merged ? git.pr?.url : pr?.url) ?? "";
  const view: GitActionEntry[] = prUrl.startsWith("https://") ? [{ id: "view-pr", label: "View PR", enabled: true, url: prUrl }] : [];
  if (merged) return [{ id: "archive", label: "Archive…", enabled: true }, ...view];

  const entries: GitActionEntry[] = [];
  const sync = (id: "push" | "update", action: TaskGitAction | undefined) => {
    if (action) entries.push({ id, label: syncTitle(id, published, base), enabled: action.enabled, reason: action.enabled ? undefined : action.reason, code: action.code });
  };
  sync("push", git.actions?.push);
  sync("update", git.actions?.updateFromBase);
  const prAction = pr ? git.actions?.updatePr : git.actions?.createPr;
  if (prAction) {
    const enabled = prAction.enabled && !opts.updatingPr;
    entries.push({
      id: pr ? "update-pr" : "create-pr",
      label: pr ? (opts.updatingPr ? "Updating…" : "Update PR") : "Create PR…",
      enabled,
      reason: prAction.enabled ? undefined : prAction.reason,
      code: prAction.code,
    });
  }
  return [...entries, ...view];
}

/**
 * The one action the header button runs: the first enabled one in priority order -- archive, the PR step, publish/push, update
 * from base -- so a task ready for review offers Create PR, a task with unpublished commits offers Publish branch, and a
 * behind branch offers Update. With nothing enabled it is the first step in flow order (disabled, its reason in the menu).
 * `view-pr` is never primary: opening a page is not the next step of the work.
 */
export function primaryGitAction(entries: GitActionEntry[]): GitActionEntry | undefined {
  const order: GitActionEntry["id"][] = ["archive", "update-pr", "create-pr", "push", "update"];
  for (const id of order) {
    const e = entries.find((x) => x.id === id && x.enabled);
    if (e) return e;
  }
  return entries.find((e) => e.id !== "view-pr");
}
