import type { PrExistsErrorData } from "@/lib/types";

export type PrOperation = "preview" | "create" | "update" | "push" | "sync" | "token" | "logout" | "archive" | "restore";

/** What the UI shows for a failed PR call: one sentence, an optional scrubbed detail, and `pr_exists`'s PR. */
export interface PrErrorView {
  message: string;
  detail?: string;
  code?: string;
  existing?: { url: string; number: number };
}

/** Sentences for the typed codes (docs/research/local/git-pr-g5-briefs.md "PR 6 wire contract"), used when the daemon's own message is missing or unsafe to show. */
const FALLBACK: Record<string, string> = {
  task_no_worktree: "This task has no worktree yet, so there is nothing to open a pull request from.",
  base_unknown: "smind doesn't know which branch this task started from. Pick a base branch first.",
  run_active: "An agent is still running in this task. Wait for it to finish, then try again.",
  no_commits: "This branch has no commits beyond its base. Commit your changes first.",
  pr_exists: "This branch already has an open pull request.",
  pr_not_open: "The pull request is no longer open, so it can't be updated.",
  no_pr: "This task has no pull request yet.",
  fork_pr_readonly: "This pull request comes from a fork, so smind can't push to it.",
  update_unsupported: "This pull request was opened from a rebased copy, so smind can't update it automatically.",
  push_rejected: "GitHub rejected the push because the branch there has changes this branch doesn't. Update this branch first.",
  push_auth: "Git couldn't sign in to push. Check the daemon host's git credentials, or run `gh auth login` there.",
  cherry_pick_conflict: "smind couldn't replay your commits onto the base branch without conflicts. Update this branch from the base first.",
  invalid_title: "The title must be one line of at most 256 characters.",
  invalid_body: "The description is too long or contains characters GitHub won't accept.",
  invalid_branch: "That base branch isn't valid.",
  pr_busy: "Another pull request action is already running for this task.",
  forge_no_remote: "This workspace has no `origin` remote, so there's nowhere to open a pull request.",
  forge_unauthenticated: "GitHub isn't signed in on the daemon host. Sign in under Settings → GitHub, or run `gh auth login` there.",
  insecure_channel: "This connection isn't encrypted, so smind won't send a token over it. Use the relay, an ssh tunnel or TLS.",
  token_invalid: "GitHub rejected that token. Check that it is correct, current and for the right host.",
  token_scopes: "That token is missing permissions. A classic token needs the `repo` and `read:org` scopes.",
  forge_cli_missing: "The GitHub CLI (`gh`) isn't installed on the daemon host.",
  forge_rate_limited: "GitHub is rate limiting requests. Try again in a minute.",
  forge_timeout: "GitHub didn't answer in time. Try again.",
  forge_error: "GitHub reported an error. Try again.",
  // Push and Update from base (G7).
  non_fast_forward: "Origin has newer changes on this branch. Bring those changes in first (pull in the worktree, or ask the agent), then push again.",
  nothing_to_push: "Nothing to push.",
  conflict: "Updating hit conflicts, and the worktree is now in the middle of the merge. Resolve the conflicts and commit, or ask the agent to resolve them.",
  merge_in_progress: "The worktree is in the middle of a merge or rebase. Finish or abort it first.",
  worktree_dirty: "The worktree has uncommitted changes. Commit or stash them, then try again.",
  invalid_strategy: "That update strategy isn't supported.",
  base_not_found: "smind couldn't find the base branch. Check the task's base branch and try again.",
  git_error: "Git reported an error. See the details.",
  // Archive and Restore (ADR-0039 §9).
  branch_unmerged: "The branch isn't merged anywhere yet, so smind won't delete it.",
  branch_dirty: "The worktree has uncommitted changes, so smind won't delete the branch.",
  branch_not_owned: "smind didn't create this branch, so it never deletes it.",
  branch_checked_out: "The branch is checked out in another worktree, so it can't be deleted.",
  branch_gone: "The branch no longer exists, so there is nothing to restore.",
  not_archived: "This task isn't archived.",
};

const GENERIC: Record<PrOperation, string> = {
  preview: "Couldn't load the pull request preview.",
  create: "Couldn't create the pull request.",
  update: "Couldn't update the pull request.",
  push: "Couldn't push the branch.",
  sync: "Couldn't update the branch from its base.",
  token: "Couldn't sign in to GitHub.",
  logout: "Couldn't log out of GitHub.",
  archive: "Couldn't archive the task.",
  restore: "Couldn't restore the task.",
};

const MAX_MESSAGE = 300;
const MAX_DETAIL = 2000;

/** Command lines, exit statuses and stack frames are for the daemon log, never for a dialog. */
const UNSAFE = /(\bgh \[|\bgit \[|\bexit status\b|--[a-z][\w-]*(?:[ =]|$)|\n|^\s*at .+:\d+|token|authorization)/im;

function isSafeSentence(text: string): boolean {
  return text.trim() !== "" && text.length <= MAX_MESSAGE && !UNSAFE.test(text);
}

function record(value: unknown): Record<string, unknown> | undefined {
  return typeof value === "object" && value !== null ? (value as Record<string, unknown>) : undefined;
}

/**
 * Maps any thrown value to the one sentence (and optional detail) the Create/Update PR UI shows. A typed error
 * (the ws client's `RpcError`: `code`, `data.detail`) keeps the daemon's fixed sentence; anything else, or a message
 * that looks like it carries argv or a stack, becomes a sentence we wrote. Untyped errors get no detail: their raw
 * text is exactly what must not reach the screen.
 */
export function describePrError(err: unknown, op: PrOperation): PrErrorView {
  const e = record(err);
  const code = typeof e?.code === "string" && e.code !== "" ? e.code : undefined;
  const data = record(e?.data);
  const message = typeof e?.message === "string" ? e.message : "";

  const view: PrErrorView = {
    message: code && isSafeSentence(message) ? message.trim() : ((code && FALLBACK[code]) ?? GENERIC[op]),
  };
  if (code) {
    view.code = code;
    const detail = data?.detail;
    if (typeof detail === "string" && detail.trim() !== "") view.detail = detail.slice(0, MAX_DETAIL);
  }
  if (code === "pr_exists") {
    const { url, number } = (data ?? {}) as Partial<PrExistsErrorData>;
    if (typeof url === "string" && typeof number === "number") view.existing = { url, number };
  }
  return view;
}

/**
 * Archive and Restore: typed errors map as `describePrError` does. An untyped refusal (a task with a running run,
 * the daemon's own sentence since before the typed codes) stays readable when it carries no argv or stack.
 */
export function describeArchiveError(err: unknown, op: "archive" | "restore"): PrErrorView {
  const e = record(err);
  const message = typeof e?.message === "string" ? e.message.trim() : "";
  if (typeof e?.code !== "string" && isSafeSentence(message)) return { message };
  return describePrError(err, op);
}
