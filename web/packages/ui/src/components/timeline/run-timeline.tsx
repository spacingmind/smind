import { RunAttachments } from "@/components/timeline/run-attachments";
import { DeliverableCards, type DeliverableContext } from "@/components/timeline/deliverable-card";
import { memo } from "react";
import { Icons } from "@/lib/icons";

import { groupTimeline, type DetailLevel } from "@/components/timeline/detail-level";
import { TimelineRow, type TimelineRowContext } from "@/components/timeline/timeline-row";
import { ToolGroupRow } from "@/components/timeline/tool-group-row";
import { stripErrorEcho } from "@/lib/run-error";
import { assistantMarkdown, timelineToText } from "@/components/timeline/timeline-text";
import {
  CopyAction,
  MESSAGE_ACTION_CLASS,
  MessageAction,
  MessageActions,
  useCopyFeedback,
} from "@/components/timeline/message-actions";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger, useMenuTooltip } from "@/components/ui/tooltip";
import { ChildUpdateRow } from "@/components/timeline/child-update-row";
import { RunErrorBox } from "@/components/timeline/run-error-box";
import { RunFooter } from "@/components/timeline/run-footer";
import type { RunFailover } from "@/components/timeline/run-failover-actions";
import type { RunModelLabels } from "@/components/timeline/run-footer-model";
import type { RunEntry } from "@/hooks/use-run-timeline";

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
  onContinue,
  failover,
  providerLabels,
  modelLabels,
  isLatest = false,
  disconnected = false,
  deliverables,
}: {
  run: RunEntry;
  detailLevel?: DetailLevel;
  onRetry?: (run: RunEntry) => void;
  /** Re-submits this run's prompt with its own provider/model/permission; omitted hides the action. */
  onRerun?: (run: RunEntry) => void;
  /** Loads this run's prompt into the composer; omitted hides the action. */
  onEditPrompt?: (run: RunEntry) => void;
  /** Continue (max_tokens / turn-limit endings): submits "Continue" on the run's own config; omitted hides the button. */
  onContinue?: (run: RunEntry) => void;
  /** ADR-0041 F2: rate-limit footer actions and failover links (see RunFailover); omitted hides them. */
  failover?: RunFailover<RunEntry>;
  /** provider.list's id→label map ("label ?? id", run-config IA); omitted leaves raw ids. */
  providerLabels?: Record<string, string>;
  /** `${provider}/${modelId}` → human model label; `${provider}/` → the provider's default-model label (run footer). */
  modelLabels?: RunModelLabels;
  /** Continue/Retry are offered on the chat's latest run only. */
  isLatest?: boolean;
  /** The daemon connection is down: the run footer freezes its live state. */
  disconnected?: boolean;
  /** What the deliverable cards under the answer need (client, task, local-path actions); omitted renders no cards. */
  deliverables?: DeliverableContext;
} & TimelineRowContext) {
  const items = run.status === "error" ? withoutErrorEcho(run.items, run.err) : run.items;
  const groups = groupTimeline(items, detailLevel);

  return (
    <li data-testid="run-entry" data-run-id={run.id} className="flex flex-col gap-2">
      {run.origin === "child_event" ? (
        <ChildUpdateRow prompt={run.prompt} />
      ) : (
        <div className="group/prompt flex flex-col items-end gap-0.5">
          <p
            className="max-w-[85%] rounded-2xl rounded-tr-sm bg-surface px-4 py-3 text-ui-base break-words whitespace-pre-wrap"
            data-testid="run-prompt"
          >
            {run.prompt}
          </p>
          <RunAttachments runId={run.id} attachments={run.attachments} />
          <MessageActions group="prompt" data-testid="run-prompt-actions">
            <CopyAction getText={() => run.prompt} data-testid="run-prompt-copy" />
            {onEditPrompt && (
              <MessageAction
                label="Edit & resend"
                icon={<Icons.edit aria-hidden />}
                data-testid="run-prompt-edit"
                onClick={() => onEditPrompt(run)}
              />
            )}
          </MessageActions>
        </div>
      )}

      {/* The answer side (items, error, footer) is its own hover group, so hovering the prompt reveals only the prompt bar. */}
      <div className="group/answer flex flex-col gap-2">
        {items.length > 0 && (
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

        <DeliverableCards run={run} ctx={deliverables} worktreePath={worktreePath} onOpenFile={onOpenFile} />

        <RunErrorBox run={run} onRetry={onRetry} />

        <RunFooter run={run} providerLabels={providerLabels} modelLabels={modelLabels} isLatest={isLatest} waiting={Boolean(run.pendingPermission)} disconnected={disconnected} onContinue={onContinue} onRerun={onRerun} failover={failover}>
          <MessageActions group="run" className="ml-auto" data-testid="run-actions">
            <RunCopyMenu run={run} />
            {onRerun && run.status !== "running" && (
              <MessageAction
                label="Re-run this prompt"
                icon={<Icons.retry aria-hidden />}
                data-testid="run-rerun"
                onClick={() => onRerun(run)}
              />
            )}
          </MessageActions>
        </RunFooter>
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
  const hasAnswer = run.items.some((item) => item.kind === "assistant" && item.text.trim() !== "");

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
              {copied ? <Icons.check aria-hidden /> : <Icons.copy aria-hidden />}
            </Button>
          </DropdownMenuTrigger>
        </TooltipTrigger>
        <TooltipContent side="bottom">{copied ? "Copied" : "Copy"}</TooltipContent>
      </Tooltip>
      {/* The check on the trigger is silent for assistive tech; this announces it. */}
      <span role="status" className="sr-only">
        {copied ? "Copied" : ""}
      </span>
      <DropdownMenuContent align="end" data-testid="run-copy-menu">
        {hasAnswer && (
          <DropdownMenuItem data-testid="run-copy-answer" onSelect={() => void copy(assistantMarkdown(run.items))}>
            Copy answer (Markdown)
          </DropdownMenuItem>
        )}
        <DropdownMenuItem
          data-testid="run-copy-turn"
          onSelect={() => void copy(timelineToText(run.prompt, run.items))}
        >
          Copy full turn (plain text)
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

/** The transcript minus the agent's "[error] <cause>" line when the run's error box states the same cause. */
function withoutErrorEcho(items: RunEntry["items"], err: string | undefined): RunEntry["items"] {
  const last = items[items.length - 1];
  if (!last || last.kind !== "assistant") return items;
  const text = stripErrorEcho(last.text, err);
  if (text === last.text) return items;
  return text === "" ? items.slice(0, -1) : [...items.slice(0, -1), { ...last, text }];
}
