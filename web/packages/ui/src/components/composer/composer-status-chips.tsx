import { GitCompare } from "lucide-react";

import { type DiffStat } from "@/lib/diff-stat";

const CHIP_CLASS =
  "flex h-7 shrink-0 items-center gap-1.5 rounded-full border border-input-border bg-surface px-3 text-ui-sm text-foreground-muted";

/** 33500 -> "33.5k", 940 -> "940": Paseo's compact diff-stat numbers, so a big refactor doesn't widen the chip. */
export function formatCompactCount(n: number): string {
  if (n < 1000) return String(n);
  const k = n / 1000;
  return `${k >= 100 ? Math.round(k) : Math.round(k * 10) / 10}k`;
}

/**
 * The task's diff stat as a compact chip above the input card
 * (web-ui-dogfood-polish Item 5): additions green, deletions red, and a
 * click jumps to the task's Diff tab. The numbers are the same stat the
 * diff pane renders (hooks/use-task-diff.ts), so nothing new is fetched
 * here -- the parent hands the already-derived stat in.
 */
function DiffStatChip({ stat, onOpenDiff }: { stat: DiffStat; onOpenDiff: () => void }) {
  return (
    <button
      type="button"
      data-testid="composer-diff-stat"
      aria-label={`Open diff: ${stat.additions} additions, ${stat.deletions} deletions`}
      onClick={onOpenDiff}
      className={`${CHIP_CLASS} transition-colors hover:bg-hover hover:text-foreground`}
    >
      <GitCompare aria-hidden className="size-3 opacity-70" />
      <span className="font-medium text-success">+{formatCompactCount(stat.additions)}</span>
      <span className="font-medium text-destructive">−{formatCompactCount(stat.deletions)}</span>
    </button>
  );
}

/**
 * Paseo's chip row above the composer: a "Working" chip while the chat has a
 * live run, plus the diff-stat chip when the task has changes and there is a
 * Diff tab to jump to. Only real data -- the run state is the same
 * `runningRunId` that drives Stop, the stat is use-task-diff's -- and the
 * row renders nothing when neither applies.
 */
export function ComposerStatusChips({
  running,
  diffStat,
  onOpenDiff,
}: {
  running: boolean;
  diffStat?: DiffStat | null;
  onOpenDiff?: () => void;
}) {
  const hasChanges =
    diffStat !== null &&
    diffStat !== undefined &&
    (diffStat.files > 0 || diffStat.additions > 0 || diffStat.deletions > 0);
  const showDiff = hasChanges && onOpenDiff !== undefined;
  if (!running && !showDiff) return null;
  return (
    <div data-testid="composer-status-chips" className="flex flex-wrap items-center gap-2">
      {running && (
        <span role="status" data-testid="composer-working-chip" className={CHIP_CLASS}>
          <span aria-hidden className="size-2 animate-pulse rounded-full bg-primary" />
          Working
        </span>
      )}
      {showDiff && <DiffStatChip stat={diffStat} onOpenDiff={onOpenDiff} />}
    </div>
  );
}
