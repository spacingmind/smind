import { memo } from "react";
import { Check, Copy, Pencil, RotateCcw } from "lucide-react";

import { groupTimeline, type DetailLevel } from "@/components/timeline/detail-level";
import { TimelineRow, type TimelineRowContext } from "@/components/timeline/timeline-row";
import { ToolGroupRow } from "@/components/timeline/tool-group-row";
import { assistantMarkdown, formatElapsed, timelineToText } from "@/components/timeline/timeline-text";
import {
  CopyAction,
  MESSAGE_ACTION_CLASS,
  MessageAction,
  MessageActions,
  useCopyFeedback,
} from "@/components/timeline/message-actions";
import { RunUsagePill } from "@/components/run-usage-pill";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger, useMenuTooltip } from "@/components/ui/tooltip";
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
  onRerun,
  onEditPrompt,
  providerLabels,
}: {
  run: RunEntry;
  detailLevel?: DetailLevel;
  onRetry?: (run: RunEntry) => void;
  /** Re-submits this run's prompt with its own provider/model/permission; omitted hides the action. */
  onRerun?: (run: RunEntry) => void;
  /** Loads this run's prompt into the composer; omitted hides the action. */
  onEditPrompt?: (run: RunEntry) => void;
  /** provider.list's id→label map ("label ?? id", run-config IA); omitted leaves raw ids. */
  providerLabels?: Record<string, string>;
} & TimelineRowContext) {
  const elapsed = formatElapsed(run.startedAt, run.finishedAt);
  const stop = run.stopReason ? describeStopReason(run.stopReason) : null;
  const groups = groupTimeline(run.items, detailLevel);

  return (
    <li data-testid="run-entry" data-run-id={run.id} className="group flex flex-col gap-2">
      <div className="group/prompt flex flex-col items-end gap-0.5">
        <p
          className="max-w-[85%] rounded-2xl rounded-tr-sm bg-surface px-4 py-3 text-ui-base break-words whitespace-pre-wrap"
          data-testid="run-prompt"
        >
          {run.prompt}
        </p>
        <MessageActions group="prompt" data-testid="run-prompt-actions">
          <CopyAction getText={() => run.prompt} data-testid="run-prompt-copy" />
          {onEditPrompt && (
            <MessageAction
              label="Edit & resend"
              icon={<Pencil aria-hidden />}
              data-testid="run-prompt-edit"
              onClick={() => onEditPrompt(run)}
            />
          )}
        </MessageActions>
      </div>

      {run.items.length > 0 && (
        <ol className="space-y-1.5" data-testid="run-timeline">
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

      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-ui-sm text-foreground-muted" data-testid="run-footer">
        <div
          className={cn(
            "flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 transition-opacity",
            run.status === "done" &&
              "opacity-70 group-focus-within:opacity-100 group-hover:opacity-100 [@media(hover:none)]:opacity-100",
          )}
          data-testid="run-meta"
        >
          <span className="flex items-center gap-1.5">
            <StatusDot status={RUN_STATUS_DOT[run.status] ?? "neutral"} />
            <span data-testid="run-status">{run.status}</span>
          </span>
          <span className="truncate">{providerLabels?.[run.provider] ?? run.provider}</span>
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
          {elapsed && <span data-testid="run-elapsed">{elapsed}</span>}
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
          <RunUsagePill usage={run.usage} provider={run.provider} />
        </div>
        <MessageActions group="run" className="ml-auto" data-testid="run-actions">
          <RunCopyMenu run={run} />
          {onRerun && run.status !== "running" && (
            <MessageAction
              label="Re-run this prompt"
              icon={<RotateCcw aria-hidden />}
              data-testid="run-rerun"
              onClick={() => onRerun(run)}
            />
          )}
        </MessageActions>
      </div>
    </li>
  );
});

/**
 * Copy menu for the turn: the answer as Markdown, or the whole turn as plain
 * text. Clipboard failures are silent (see `useCopyFeedback`).
 */
function RunCopyMenu({ run }: { run: RunEntry }) {
  const { copied, copy } = useCopyFeedback();
  const { tooltip, onMenuOpenChange } = useMenuTooltip();
  const answer = assistantMarkdown(run.items);

  return (
    <DropdownMenu onOpenChange={onMenuOpenChange}>
      <Tooltip {...tooltip}>
        <TooltipTrigger asChild>
          <DropdownMenuTrigger asChild>
            <Button
              type="button"
              variant="ghost"
              size="icon-xs"
              className={MESSAGE_ACTION_CLASS}
              data-testid="run-copy-button"
              aria-label="Copy"
            >
              {copied ? <Check aria-hidden /> : <Copy aria-hidden />}
            </Button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent side="bottom">{copied ? "Copied" : "Copy"}</TooltipContent>
      </Tooltip>
      <DropdownMenuContent align="end" data-testid="run-copy-menu">
        {answer && (
          <DropdownMenuItem data-testid="run-copy-answer" onSelect={() => void copy(answer)}>
            Copy answer (Markdown)
          </DropdownMenuItem>
        )}
        <DropdownMenuItem
          data-testid="run-copy-turn"
          aria-label="Copy turn"
          onSelect={() => void copy(timelineToText(run.prompt, run.items))}
        >
          Copy full turn (plain text)
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
