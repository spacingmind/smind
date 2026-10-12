import { memo, useState } from "react";
import { Check, Copy } from "lucide-react";

import { groupTimeline, type DetailLevel } from "@/components/timeline/detail-level";
import { TimelineRow, type TimelineRowContext } from "@/components/timeline/timeline-row";
import { ToolGroupRow } from "@/components/timeline/tool-group-row";
import { formatElapsed, timelineToText } from "@/components/timeline/timeline-text";
import { RunUsagePill } from "@/components/run-usage-pill";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { StatusDot, type StatusDotStatus } from "@/components/ui/status-dot";
import { describeStopReason } from "@/components/timeline/stop-reason";
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
  providerLabels,
}: {
  run: RunEntry;
  detailLevel?: DetailLevel;
  onRetry?: (run: RunEntry) => void;
  /** provider.list's id→label map ("label ?? id", run-config IA); omitted leaves raw ids. */
  providerLabels?: Record<string, string>;
} & TimelineRowContext) {
  const elapsed = formatElapsed(run.startedAt, run.finishedAt);
  const stop = run.stopReason ? describeStopReason(run.stopReason) : null;
  const groups = groupTimeline(run.items, detailLevel);

  return (
    <li data-testid="run-entry" data-run-id={run.id} className="group flex flex-col gap-3">
      <div className="flex justify-end">
        <p
          className="max-w-[85%] rounded-2xl rounded-tr-sm bg-surface px-4 py-3 text-ui-base whitespace-pre-wrap"
          data-testid="run-prompt"
        >
          {run.prompt}
        </p>
      </div>

      {run.items.length > 0 && (
        <ol className="space-y-2" data-testid="run-timeline">
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
        <div className="flex items-center gap-2 rounded-lg bg-destructive/10 px-3 py-2">
          <p className="text-ui-sm text-destructive" data-testid="run-error">
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

      <div
        className={cn(
          "flex flex-wrap items-center gap-x-2 gap-y-1 text-ui-sm text-foreground-muted transition-opacity",
          run.status === "done" && "opacity-60 group-focus-within:opacity-100 group-hover:opacity-100",
        )}
        data-testid="run-footer"
      >
        <span className="flex items-center gap-1.5">
          <StatusDot status={RUN_STATUS_DOT[run.status]} />
          <span data-testid="run-status">{run.status}</span>
        </span>
        <span className="truncate">· {providerLabels?.[run.provider] ?? run.provider}</span>
        {run.model && (
          // ADR-0025: only a *requested* model is shown -- a default run has no chip.
          <span
            data-testid="run-model-chip"
            title={`Requested model: ${run.model}`}
            className="max-w-40 shrink truncate rounded bg-surface px-1.5 py-0.5 font-mono text-ui-xs text-foreground-muted"
          >
            {run.model}
          </span>
        )}
        {elapsed && <span data-testid="run-elapsed">· {elapsed}</span>}
        {stop && (
          <span
            data-testid="run-stop-reason"
            data-tone={stop.tone}
            title={stop.hint}
            className={stop.tone === "warning" ? "text-warning" : undefined}
          >
            {stop.label}
            {stop.hint && <span className="ml-1 text-foreground-muted">· {stop.hint}</span>}
          </span>
        )}
        <RunUsagePill usage={run.usage} />
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
