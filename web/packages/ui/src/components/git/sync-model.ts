import type { TaskGit, TaskPushResult, TaskUpdateFromBaseResult, UpdateConflictErrorData } from "@/lib/types";

export type SyncMode = "push" | "update";

/** The card's facts, taken from the task's git state when the dialog opens (frozen once the call starts). */
export interface SyncCard {
  branch: string;
  /** Push: where the branch goes. Update: the ref being brought in. */
  remote: string;
  published: boolean;
  /** Commits the branch has that the remote (push) or base (update) lacks; undefined while unknown. */
  ahead?: number;
  behind?: number;
  /** Push: the remote and branch the daemon will really push to; absent from older daemons (origin/<branch> assumed). */
  pushTo?: { remote: string; branch: string };
  /** Update: the base's name as the task knows it ("develop"). */
  base: string;
}

export function plural(n: number, one: string, many = `${one}s`): string {
  return `${n} ${n === 1 ? one : many}`;
}

export function syncCard(mode: SyncMode, git: TaskGit | undefined): SyncCard {
  const published = git?.origin?.published ?? false;
  const base = git?.base?.known ? (git.base.ref ?? "") : "";
  const branch = git?.branch?.name ?? "";
  if (mode === "push") {
    return {
      branch,
      remote: "origin",
      pushTo: git?.origin?.pushTarget,
      published,
      ahead: published ? git?.origin?.ahead : git?.commits?.sinceFork,
      behind: published ? git?.origin?.behind : undefined,
      base,
    };
  }
  return {
    branch,
    remote: base ? `origin/${base}` : "",
    published,
    ahead: git?.commits?.aheadOfBase,
    behind: git?.commits?.behindBase,
    base,
  };
}

/** The "Next step" row: what the primary button will do, in one sentence. */
export function nextStep(mode: SyncMode, card: SyncCard, rebase: boolean): string {
  if (mode === "push") {
    const to = card.pushTo;
    if (!card.published) return to ? `Publish ${card.branch} to ${to.remote}/${to.branch} and track it.` : `Publish ${card.branch} to origin and track it.`;
    if (to) return card.ahead ? `Push ${plural(card.ahead, "commit")} to ${to.remote}/${to.branch}.` : `Push ${card.branch} to ${to.remote}/${to.branch}.`;
    return card.ahead ? `Push ${plural(card.ahead, "commit")} to origin/${card.branch}.` : `Push ${card.branch} to origin.`;
  }
  const target = card.remote || "the base";
  if (card.behind === 0) return `Already up to date with ${card.base || "the base"}.`;
  const count = card.behind ? ` (${plural(card.behind, "new commit")})` : "";
  return rebase ? `Rebase this branch onto ${target}${count}.` : `Merge ${card.behind ? `${plural(card.behind, "commit")} from ` : ""}${target} into this branch.`;
}

/** A quiet line under the Remote row when the push does not go to origin/<task branch>; null otherwise. */
export function pushTargetNote(card: SyncCard): string | null {
  const to = card.pushTo;
  if (!to) return null;
  if (to.remote !== "origin") return `Pushes to the \`${to.remote}\` remote.`;
  return to.branch !== card.branch ? "Updates the pull request's branch." : null;
}

export function pushSuccessLine(result: TaskPushResult): string {
  const target = `${result.remote}/${result.branch}`;
  return result.upstreamCreated ? `Published ${result.branch} to ${result.remote}${result.pushed ? ` (${plural(result.pushed, "commit")})` : ""}` : `Pushed ${plural(result.pushed, "commit")} to ${target}`;
}

export function updateSuccessLine(result: TaskUpdateFromBaseResult, card: SyncCard): string {
  if (result.result === "up_to_date") return "Already up to date";
  const from = card.remote || "the base";
  return result.result === "rebased"
    ? `Rebased onto ${from} (${plural(result.commits, "commit")})`
    : `Merged ${plural(result.commits, "commit")} from ${from}`;
}

/** Reads a typed conflict error's `data`, defensively: the wire is not trusted to be well formed. */
export function conflictData(data: unknown): UpdateConflictErrorData | null {
  if (typeof data !== "object" || data === null) return null;
  const d = data as Partial<UpdateConflictErrorData>;
  if (!Array.isArray(d.files)) return null;
  return {
    files: d.files.filter((f): f is string => typeof f === "string"),
    operation: d.operation === "rebase" ? "rebase" : "merge",
    base: typeof d.base === "string" ? d.base : "",
    filesTruncated: d.filesTruncated === true,
  };
}

/** The prompt "Ask the agent to resolve" sends: the paths, what state the worktree is in, and the finish line. */
export function resolveConflictPrompt(c: UpdateConflictErrorData): string {
  const op = c.operation;
  const onto = c.base ? ` ${op === "rebase" ? "onto" : "of"} ${c.base}` : "";
  const files = c.files.map((f) => `- ${f}`).join("\n");
  const more = c.filesTruncated ? "\n(The list is truncated; run `git status` for the rest.)" : "";
  return [
    `The ${op}${onto} stopped with conflicts, and the worktree is in the middle of it. Conflicting files:`,
    `${files}${more}`,
    "",
    `Finish the ${op} in the worktree: resolve every conflict keeping the intent of both sides, stage the resolved files and run \`git ${op} --continue\` (do not abort it). Then run the project's tests and tell me the result.`,
  ].join("\n");
}
