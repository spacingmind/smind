import type { RunStatusValue, TaskGit } from "@/lib/types";

/**
 * The status word for a task's hover card. The task's lifecycle column
 * (`Task.Status`) stays "created" after runs finish, so the label is derived
 * from the same signals the row uses: the latest run state, the rate-limit
 * flag, and git/PR state. Priority: running > failed/rate limited > merged >
 * PR state > done/stopped > the lifecycle value (so a task with no run
 * still reads as its lifecycle status).
 */
export function describeTaskStatus({
  runState,
  limited,
  git,
  lifecycle,
}: {
  runState: RunStatusValue | undefined;
  limited?: boolean;
  git?: TaskGit;
  lifecycle: string;
}): string {
  if (runState === "running") return "Running";
  if (runState === "error") return limited ? "Rate limited" : "Failed";
  if (git?.merged?.state === "merged" || git?.pr?.state === "merged") return "Merged";
  const pr = git?.pr;
  if (pr?.state === "open") return pr.draft ? `Draft PR #${pr.number}` : `PR #${pr.number} open`;
  if (pr?.state === "closed") return `PR #${pr.number} closed`;
  if (runState === "done") return "Done";
  if (runState === "stopped") return "Stopped";
  return lifecycle;
}
