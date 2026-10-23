import { describe, expect, it } from "vitest";

import { describeArchiveError, describePrError } from "@/components/git/pr-error";
import { RpcError } from "@/lib/ws-client";

describe("describePrError", () => {
  it("keeps a typed error's sentence and scrubbed detail", () => {
    const view = describePrError(new RpcError("Git couldn't sign in to push.", "push_auth", { detail: "fatal: bad" }), "create");
    expect(view).toEqual({ message: "Git couldn't sign in to push.", detail: "fatal: bad", code: "push_auth" });
  });

  it("keeps the daemon's push and update sentences and puts the stderr in Details", () => {
    const sentence =
      "Merging `origin/develop` into this branch hit conflicts, and the worktree is now in the middle of the merge. Resolve the conflicts and commit, or ask the agent to; `git merge --abort` in the worktree undoes it.";
    const conflict = describePrError(new RpcError(sentence, "conflict", { files: ["a.go"], detail: "CONFLICT (content): a.go" }), "sync");
    expect(conflict).toEqual({ message: sentence, detail: "CONFLICT (content): a.go", code: "conflict" });
    for (const code of ["non_fast_forward", "merge_in_progress", "worktree_dirty", "nothing_to_push", "invalid_strategy", "forge_unauthenticated"]) {
      expect(describePrError(new Error("git [push origin x]: exit status 1"), "push").message).toBe("Couldn't push the branch.");
      expect(describePrError(new RpcError("git [push --force]", code), "push").message).not.toMatch(/git \[/);
    }
    expect(describePrError(undefined, "sync").message).toBe("Couldn't update the branch from its base.");
  });

  it("extracts pr_exists's PR", () => {
    const view = describePrError(new RpcError("Already open.", "pr_exists", { url: "https://x/pull/1", number: 1 }), "create");
    expect(view.existing).toEqual({ url: "https://x/pull/1", number: 1 });
  });

  it("replaces a typed message that carries argv with the fixed sentence", () => {
    const view = describePrError(new RpcError("git [push origin a:b --force]: exit status 1", "push_rejected"), "update");
    expect(view.message).toMatch(/^GitHub rejected the push/);
  });

  it("never shows an untyped error's text, and has no detail", () => {
    const view = describePrError(new Error("gh [pr create --body secret]: exit status 1\n  at x.js:1:1"), "create");
    expect(view).toEqual({ message: "Couldn't create the pull request." });
    expect(describePrError("boom", "preview").message).toBe("Couldn't load the pull request preview.");
    expect(describePrError(undefined, "update").message).toBe("Couldn't update the pull request.");
  });

  it("falls back to a generic sentence for an unknown code with no message", () => {
    expect(describePrError(new RpcError("", "weird"), "create").message).toBe("Couldn't create the pull request.");
  });
});

describe("describeArchiveError", () => {
  it("maps each typed archive code to one sentence, never the daemon's raw text", () => {
    for (const code of ["branch_unmerged", "branch_dirty", "branch_not_owned", "branch_checked_out", "branch_gone", "not_archived"]) {
      const view = describeArchiveError(new RpcError("git [branch -D x]: exit status 1", code), code === "branch_gone" || code === "not_archived" ? "restore" : "archive");
      expect(view.code).toBe(code);
      expect(view.message).not.toMatch(/exit status|branch -D/);
    }
  });

  it("keeps a readable untyped refusal and scrubs an unsafe one", () => {
    expect(describeArchiveError(new Error("task.archive: task 7: a run is still running"), "archive").message).toBe("task.archive: task 7: a run is still running");
    expect(describeArchiveError(new Error("git [worktree remove x]: exit status 1"), "archive").message).toBe("Couldn't archive the task.");
  });
});
