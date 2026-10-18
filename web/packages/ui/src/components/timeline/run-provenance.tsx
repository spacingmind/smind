import { Icons } from "@/lib/icons";
import { useCopyFeedback } from "@/components/timeline/message-actions";
import type { RunStatusValue } from "@/lib/types";

/**
 * The run footer's provenance segment (O1): the worktree commit the run
 * started from (or ended at, once terminal), a quiet dirty marker, and
 * click-to-copy of the full sha shown. Renders nothing when neither sha
 * is known (an old run, a non-git worktree).
 */
export function RunProvenance({
  baseSha,
  headSha,
  dirty,
  status,
}: {
  baseSha?: string;
  headSha?: string;
  dirty?: boolean;
  status: RunStatusValue;
}) {
  const terminal = status !== "running";
  const shownSha = (terminal && headSha) || baseSha;
  if (!shownSha) return null;

  const { copied, copy } = useCopyFeedback();
  const title = [
    baseSha ? `Base ${baseSha}` : null,
    headSha ? `Head ${headSha}` : null,
    dirty === true ? "Uncommitted changes" : null,
  ]
    .filter(Boolean)
    .join(" / ");

  return (
    <button
      type="button"
      data-testid="run-provenance"
      title={title}
      aria-label={`Copy commit sha ${shownSha}${dirty === true ? " (uncommitted changes)" : ""}`}
      className="flex items-center gap-1 rounded-sm text-foreground-muted hover:text-foreground"
      onClick={() => void copy(shownSha)}
    >
      {copied ? (
        <Icons.check aria-hidden className="size-3" />
      ) : (
        <Icons.commit aria-hidden className="size-3" />
      )}
      <span className="font-mono">{shownSha.slice(0, 7)}</span>
      {dirty === true && (
        <span aria-hidden className="inline-block size-1.5 rounded-full bg-warning" />
      )}
      {dirty === true && <span className="sr-only">uncommitted changes</span>}
    </button>
  );
}
