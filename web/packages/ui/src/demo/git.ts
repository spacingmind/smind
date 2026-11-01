import type { Task, TaskFile } from "@/lib/types";

import type { DemoSession } from "./session";

/** The diff the Diff tab shows for a task: the scenario's, plus the live task's extra file once its permission was allowed. */
export function diffOf(s: DemoSession, taskId: number): string {
  const base = s.scenario.diffs[taskId] ?? "";
  return s.allowedTasks.has(taskId) ? `${base}${s.scenario.liveTaskExtraDiff}` : base;
}

/** Splits a unified diff into per-file chunks keyed by the post-image path (`diff --git a/<x> b/<path>`). */
export function splitDiff(diff: string): { path: string; added: boolean; deleted: boolean; chunk: string }[] {
  const out: { path: string; added: boolean; deleted: boolean; chunk: string }[] = [];
  const parts = diff.split(/^(?=diff --git )/m).filter((p) => p.startsWith("diff --git "));
  for (const chunk of parts) {
    const m = /^diff --git a\/(.+?) b\/(.+)$/m.exec(chunk);
    if (!m) continue;
    out.push({ path: m[2], added: /^new file mode/m.test(chunk), deleted: /^deleted file mode/m.test(chunk), chunk });
  }
  return out;
}

export function filesOf(s: DemoSession, taskId: number): TaskFile[] {
  return splitDiff(diffOf(s, taskId)).map((f) => ({ path: f.path, status: f.added ? "added" : f.deleted ? "deleted" : "modified", staged: false }));
}

/** A task's `task.git` snapshot (what the sidebar's diff counts and the composer's diff chip read). */
export function gitOf(s: DemoSession, task: Task) {
  const files = splitDiff(diffOf(s, task.ID));
  let insertions = 0;
  let deletions = 0;
  for (const f of files) {
    for (const line of f.chunk.split("\n")) {
      if (line.startsWith("+") && !line.startsWith("+++")) insertions++;
      else if (line.startsWith("-") && !line.startsWith("---")) deletions++;
    }
  }
  return {
    taskId: task.ID,
    branch: { name: task.Branch ?? "", owned: true },
    base: { ref: "main", known: true },
    diff: { files: files.length, insertions, deletions },
    worktree: { dirty: files.length > 0, staged: 0, unstaged: files.length, untracked: 0 },
    untrusted: false,
  };
}
