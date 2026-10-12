import { GitCompare, GitBranch } from "lucide-react";

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
 * ADR-0016 P3's concurrency notice: two chats of the same task share one
 * worktree, so a run in another chat can touch the same files this one is
 * looking at -- surfaced, not blocked (ADR-0016 §4). A chip beside Working;
 * clicking jumps to that chat's tab.
 */
function ConcurrencyChip({ chat, onJump }: { chat: { id: number; title: string }; onJump: () => void }) {
  return (
    <button
      type="button"
      data-testid="composer-concurrency-chip"
      aria-label={`Another chat is running in this worktree: ${chat.title}. Open it.`}
      title={`Another chat is running in this worktree: ${chat.title}`}
      onClick={onJump}
      className={`${CHIP_CLASS} min-w-0 transition-colors hover:bg-hover hover:text-foreground`}
    >
      <GitBranch aria-hidden className="size-3 shrink-0 text-warning" />
      <span className="shrink-0">Also running ·</span>
      <span className="max-w-48 truncate">{chat.title}</span>
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
  otherRunningChat,
  onJumpToChat,
}: {
  running: boolean;
  /** Another chat of this task with a live run (shared worktree) -- renders the concurrency chip. */
  otherRunningChat?: { id: number; title: string } | null;
  /** Jumps to otherRunningChat's tab. */
  onJumpToChat?: (chatId: number) => void;
  diffStat?: DiffStat | null;
  onOpenDiff?: () => void;
}) {
  const hasChanges =
    diffStat !== null &&
    diffStat !== undefined &&
    (diffStat.files > 0 || diffStat.additions > 0 || diffStat.deletions > 0);
  const showDiff = hasChanges && onOpenDiff !== undefined;
  const showOther = otherRunningChat !== null && otherRunningChat !== undefined;
  if (!running && !showDiff && !showOther) return null;
  return (
    <div data-testid="composer-status-chips" className="flex flex-wrap items-center gap-2">
      {running && (
        <span role="status" data-testid="composer-working-chip" className={CHIP_CLASS}>
          <span aria-hidden className="size-2 animate-pulse rounded-full bg-primary" />
          Working
        </span>
      )}
      {showOther && <ConcurrencyChip chat={otherRunningChat} onJump={() => onJumpToChat?.(otherRunningChat.id)} />}
      {showDiff && <DiffStatChip stat={diffStat} onOpenDiff={onOpenDiff} />}
    </div>
  );
}
