import { act } from "@testing-library/react";

import { PrAction } from "@/components/git/pr-action";
import type { FakeWsClient } from "@/test/fake-ws-client";
import type { PrPreview, TaskGit, TaskGitPr } from "@/lib/types";

/** `renderWithClientProp` finds the client through this prop. */
export function Harness({ taskId = 1, workspaceId = 7 }: { client: FakeWsClient; taskId?: number; workspaceId?: number }) {
  return <PrAction taskId={taskId} workspaceId={workspaceId} />;
}

export async function flush(): Promise<void> {
  await act(async () => {
    for (let i = 0; i < 4; i++) await Promise.resolve();
  });
}

export function gitState(over: Partial<TaskGit> = {}): TaskGit {
  return {
    taskId: 1,
    untrusted: false,
    computedAt: "2026-01-01T00:00:00Z",
    branch: { name: "smind/task-x", owned: true },
    base: { ref: "develop", known: true },
    diff: { files: 3, insertions: 12, deletions: 4 },
    actions: { createPr: { enabled: true }, updatePr: { enabled: false, code: "no_pr", reason: "Update PR isn't available yet because this task has no pull request." } },
    ...over,
  };
}

export function openPr(over: Partial<TaskGitPr> = {}): TaskGitPr {
  return {
    number: 42,
    repo: "owner/repo",
    url: "https://github.com/owner/repo/pull/42",
    title: "A title",
    state: "open",
    draft: false,
    headRef: "smind/task-x",
    headSha: "abc",
    baseRef: "develop",
    fromFork: false,
    mergeable: "mergeable",
    reviewDecision: "none",
    fetchedAt: "2026-01-01T00:00:00Z",
    stale: "",
    refreshAvailableAt: "",
    ...over,
  };
}

export function preview(over: Partial<PrPreview> = {}): PrPreview {
  return {
    base: { ref: "develop", sha: "b".repeat(40) },
    head: { branch: "smind/task-x", ref: "refs/heads/smind/task-x", strategy: "direct" },
    commits: [
      { sha: "a".repeat(40), subject: "Add the thing" },
      { sha: "c".repeat(40), subject: "Fix the other thing" },
    ],
    commitCount: 2,
    diff: { files: 3, insertions: 12, deletions: 4 },
    uncommitted: { staged: 0, unstaged: 0, untracked: 0 },
    defaults: { title: "Task X", body: "## Summary\n- Add the thing\n- Fix the other thing\n\nOpened from smind task #1.", draft: true },
    blockers: [],
    ...over,
  };
}
