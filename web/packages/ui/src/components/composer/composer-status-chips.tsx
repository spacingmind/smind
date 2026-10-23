import { PrChip } from "@/components/git/pr-chip";
import { type DiffStat } from "@/lib/diff-stat";
import { Icons } from "@/lib/icons";
import type { TaskGit } from "@/lib/types";

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
 * click jumps to the task's Diff tab. The numbers are the task's
 * `TaskGit.diff` (ADR-0039), handed in by the parent.
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
      <Icons.diff aria-hidden className="size-3 opacity-70" />
      <span className={`font-medium text-success${stat.additions === 0 ? " opacity-50" : ""}`}>+{formatCompactCount(stat.additions)}</span>
      <span className={`font-medium text-destructive${stat.deletions === 0 ? " opacity-50" : ""}`}>−{formatCompactCount(stat.deletions)}</span>
    </button>
  );
}

/** The task's branch cannot merge cleanly into its base (ADR-0039): destructive text, click opens the Diff tab. */
function ConflictsChip({ base, onOpenDiff }: { base: string; onOpenDiff: () => void }) {
  return (
    <button
      type="button"
      data-testid="composer-conflicts-chip"
      onClick={onOpenDiff}
      className={`${CHIP_CLASS} text-destructive transition-colors hover:bg-hover`}
    >
      <Icons.warning aria-hidden className="size-3" />
      <span>Conflicts with {base}</span>
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
      aria-label={`Also running ${chat.title} - another chat is running in this worktree; open it`}
      title={`Another chat is running in this worktree: ${chat.title}`}
      onClick={onJump}
      className={`${CHIP_CLASS} min-w-0 transition-colors hover:bg-hover hover:text-foreground`}
    >
      <Icons.branch aria-hidden className="size-3 shrink-0 text-warning" />
      <span className="shrink-0">Also running ·</span>
      <span className="max-w-48 truncate">{chat.title}</span>
    </button>
  );
}

/**
 * Paseo's chip row above the composer: a "Working" chip while the chat has a
 * live run, the diff-stat chip when the task has changes and there is a
 * Diff tab to jump to, the PR chip, and a conflicts chip. Only real data --
 * the run state is the same `runningRunId` that drives Stop, the rest is
 * the task's `TaskGit` -- and the row renders nothing when none applies.
 */
export function ComposerStatusChips({
  running,
  waiting = false,
  git,
  onOpenDiff,
  otherRunningChat,
  onJumpToChat,
}: {
  running: boolean;
  /** The live run is blocked on the user (an unanswered permission): the chip says so, like the timeline footer. */
  waiting?: boolean;
  /** Another chat of this task with a live run (shared worktree) -- renders the concurrency chip. */
  otherRunningChat?: { id: number; title: string } | null;
  /** Jumps to otherRunningChat's tab. */
  onJumpToChat?: (chatId: number) => void;
  /** The task's git/PR state (`useTaskGit`); undefined until it is known. */
  git?: TaskGit;
  onOpenDiff?: () => void;
}) {
  const diffStat: DiffStat | null = git?.diff
    ? { files: git.diff.files, additions: git.diff.insertions, deletions: git.diff.deletions }
    : null;
  const hasChanges =
    diffStat !== null &&
    diffStat !== undefined &&
    (diffStat.files > 0 || diffStat.additions > 0 || diffStat.deletions > 0);
  const showDiff = hasChanges && onOpenDiff !== undefined;
  const showConflicts = git?.conflict?.state === "conflicts" && onOpenDiff !== undefined;
  const showOther = otherRunningChat !== null && otherRunningChat !== undefined;
  if (!running && !showDiff && !git?.pr && !showConflicts && !showOther) return null;
  return (
    <div data-testid="composer-status-chips" className="flex flex-wrap items-center gap-2">
      {running && (
        <span role="status" data-testid="composer-working-chip" data-waiting={waiting || undefined} className={`${CHIP_CLASS}${waiting ? " text-warning" : ""}`}>
          {/* Static while waiting: nothing is progressing, so nothing pulses. */}
          <span aria-hidden className={`size-2 rounded-full ${waiting ? "bg-warning" : "animate-pulse bg-primary"}`} />
          {waiting ? "Waiting for you" : "Working"}
        </span>
      )}
      {showOther && <ConcurrencyChip chat={otherRunningChat} onJump={() => onJumpToChat?.(otherRunningChat.id)} />}
      {showDiff && <DiffStatChip stat={diffStat} onOpenDiff={onOpenDiff} />}
      {git?.pr && <PrChip pr={git.pr} />}
      {showConflicts && <ConflictsChip base={git?.conflict?.against || git?.base?.ref || "base"} onOpenDiff={onOpenDiff} />}
    </div>
  );
}
