import { memo, useState } from "react";
import { Check, Copy } from "lucide-react";

import { groupTimeline, type DetailLevel } from "@/components/timeline/detail-level";
import { TimelineRow, type TimelineRowContext } from "@/components/timeline/timeline-row";
import { ToolGroupRow } from "@/components/timeline/tool-group-row";
import { formatElapsed, timelineToText } from "@/components/timeline/timeline-text";
import { Button } from "@/components/ui/button";
import { StatusDot, type StatusDotStatus } from "@/components/ui/status-dot";
import { canRetryWithHigherEffort, type RunEntry } from "@/hooks/use-run-timeline";
import type { RunStatusValue } from "@/lib/types";

const RUN_STATUS_DOT: Record<RunStatusValue, StatusDotStatus> = {
  running: "running",
  done: "success",
  error: "danger",
  stopped: "warning",
};

/**
 * One run rendered as a turn: the prompt that started it, the typed
 * transcript, and a footer with elapsed time and a copy action
 * (`audit-paseo.md` §2). Replaces the single `<pre>` of collected text
 * the run card used to be.
 *
 * Memoized on `run`'s object identity: use-run-timeline.ts's `appendEvent`
 * only replaces the touched run's object in the `runs` array (every other
 * run keeps its prior reference), so without this memo a chunk landing in
 * one run would re-run `groupTimeline` -- an O(items) pass -- for every
 * other run in the task's history on every single chunk, not the O(1)
 * per-chunk cost TimelineRow's own memo (see timeline-row.tsx) is supposed
 * to buy the transcript as a whole.
 */
export const RunTimeline = memo(function RunTimeline({
  run,
  detailLevel = "detailed",
  worktreePath,
  onOpenFile,
  onRetry,
}: { run: RunEntry; detailLevel?: DetailLevel; onRetry?: (run: RunEntry) => void } & TimelineRowContext) {
  const elapsed = formatElapsed(run.startedAt, run.finishedAt);
  const groups = groupTimeline(run.items, detailLevel);

  return (
    <li data-testid="run-entry" data-run-id={run.id} className="rounded-lg border">
      <div className="flex items-center justify-between gap-2 border-b px-3 py-1.5 text-xs text-foreground-muted">
        <span className="truncate">{run.provider}</span>
        <span className="flex shrink-0 items-center gap-1.5">
          <StatusDot status={RUN_STATUS_DOT[run.status]} />
          <span className="uppercase" data-testid="run-status">
            {run.status}
          </span>
        </span>
      </div>

      <div className="px-3 py-2">
        <p className="text-sm font-medium whitespace-pre-wrap" data-testid="run-prompt">
          {run.prompt}
        </p>

        {run.items.length > 0 && (
          <ol className="mt-2 space-y-2" data-testid="run-timeline">
            {groups.map((group) =>
              group.kind === "tool-group" ? (
                <ToolGroupRow key={group.id} items={group.items} worktreePath={worktreePath} onOpenFile={onOpenFile} />
              ) : (
                <TimelineRow key={group.id} item={group.item} worktreePath={worktreePath} onOpenFile={onOpenFile} />
              ),
            )}
          </ol>
        )}

        {run.err && (
          <div className="mt-2 flex items-center gap-2">
            <p className="text-xs text-status-danger" data-testid="run-error">
              {run.err}
            </p>
            {onRetry && canRetryWithHigherEffort(run) && (
              <Button
                type="button"
                variant="outline"
                size="xs"
                data-testid="run-retry-higher-effort"
                onClick={() => onRetry(run)}
              >
                Retry with higher effort
              </Button>
            )}
          </div>
        )}
      </div>

      <div className="flex items-center gap-2 border-t px-3 py-1 text-xs text-foreground-muted" data-testid="run-footer">
        {elapsed && <span data-testid="run-elapsed">{elapsed}</span>}
        {run.stopReason && <span data-testid="run-stop-reason">{run.stopReason}</span>}
        <CopyTurnButton run={run} />
      </div>
    </li>
  );
});

/**
 * Copies the whole turn as plain text. Clipboard access can be absent
 * (no secure context, jsdom) or rejected (permission denied) -- either
 * way the button reports nothing rather than throwing into the
 * transcript.
 */
function CopyTurnButton({ run }: { run: RunEntry }) {
  const [copied, setCopied] = useState(false);

  async function handleCopy() {
    const text = timelineToText(run.prompt, run.items);
    try {
      await navigator.clipboard?.writeText(text);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1500);
    } catch {
      // Nothing useful to say to the user here; the transcript is still
      // selectable by hand.
    }
  }

  return (
    <Button
      type="button"
      variant="ghost"
      size="xs"
      className="ml-auto"
      data-testid="run-copy-button"
      aria-label="Copy turn"
      onClick={handleCopy}
    >
      {copied ? <Check className="size-3" /> : <Copy className="size-3" />}
      {copied ? "Copied" : "Copy"}
    </Button>
  );
}
