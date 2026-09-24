import { memo } from "react";
import { CircleHelp, ShieldCheck } from "lucide-react";

import { PERMISSION_REASON_LABEL } from "@/components/timeline/permission-reason";
import { TimelineMarkdown } from "@/components/timeline/timeline-markdown";
import { ToolCall } from "@/components/timeline/tool-call";
import { StatusBadge } from "@/components/ui/status-badge";
import type { TimelineItem, TimelinePermissionItem } from "@/hooks/use-run-timeline";

/**
 * The file-opening context a tool-call card needs. Both must be stable
 * across renders or TimelineRow's memo stops bailing out -- TaskDetailPane
 * pins `onOpenFile` through a ref for exactly that reason.
 */
export interface TimelineRowContext {
  /** The task's worktree root, used to turn an absolute tool-call path into the relative wire path a file tab wants. */
  worktreePath?: string;
  onOpenFile?: (path: string) => void;
}

/**
 * One transcript row, dispatched by item kind.
 *
 * Memoized on the item's object identity, which is the whole point of the
 * reducer in use-run-timeline.ts keeping untouched items' references
 * stable: a streamed chunk rebuilds only the tail item, so React
 * re-renders exactly one row no matter how long the run is. Item 8's
 * "appending a chunk must not re-render the whole transcript" is this
 * `memo` plus that reducer, and nothing else.
 */
export const TimelineRow = memo(function TimelineRow({
  item,
  worktreePath,
  onOpenFile,
}: { item: TimelineItem } & TimelineRowContext) {
  switch (item.kind) {
    case "user":
      return (
        <li data-testid="timeline-user" data-item-kind="user" className="flex justify-end">
          <div
            data-chat-find-text="true"
            className="max-w-[85%] rounded-lg bg-surface-2 px-2.5 py-1.5 text-sm whitespace-pre-wrap"
          >
            {item.text}
          </div>
        </li>
      );

    case "assistant":
      return (
        <li data-testid="timeline-assistant" data-item-kind="assistant">
          <TimelineMarkdown content={item.text} />
        </li>
      );

    case "thinking":
      return (
        <li data-testid="timeline-thinking" data-item-kind="thinking">
          {/* Collapsed by default (Item 8): reasoning is available, not in the way. */}
          <details className="rounded-lg border border-dashed">
            <summary className="cursor-pointer px-2.5 py-1 text-xs text-foreground-muted select-none">
              Thinking
            </summary>
            <div
              data-chat-find-text="true"
              className="border-t border-dashed px-2.5 py-1.5 text-xs whitespace-pre-wrap text-foreground-muted"
            >
              {item.text}
            </div>
          </details>
        </li>
      );

    case "tool_call":
      return (
        <li data-item-kind="tool_call">
          <ToolCall item={item} worktreePath={worktreePath} onOpenFile={onOpenFile} />
        </li>
      );

    case "permission":
      return (
        <li data-testid="timeline-permission" data-item-kind="permission">
          <PermissionRow item={item} />
        </li>
      );

    default:
      // An event type this build has never heard of. The daemon's enum is
      // append-only (ADR 0008), so this is a supported state: say what
      // arrived and keep the rest of the transcript intact.
      //
      // A "raw" event (ADR 0010) has a `rawKind` -- the ACP session-update
      // kind the daemon's normalizer didn't recognize (e.g. "plan") -- which
      // is far more informative than the wire `type` itself (always "raw").
      // Shown with the same collapsed-details pattern as "thinking" above,
      // since the payload is a debugging aid, not something to read by
      // default.
      return (
        <li data-testid="timeline-unknown" data-item-kind="unknown">
          {item.rawKind ? (
            <details className="rounded-lg border border-dashed">
              <summary className="flex cursor-pointer items-center gap-1.5 px-2.5 py-1 text-xs text-foreground-muted select-none">
                <CircleHelp className="size-3 shrink-0" />
                Unrecognised event: {item.rawKind}
              </summary>
              <pre className="overflow-x-auto border-t border-dashed px-2.5 py-1.5 text-xs whitespace-pre-wrap text-foreground-muted">
                {JSON.stringify(item.rawPayload, null, 2)}
              </pre>
            </details>
          ) : (
            <p className="flex items-center gap-1.5 rounded-lg border border-dashed px-2.5 py-1 text-xs text-foreground-muted">
              <CircleHelp className="size-3 shrink-0" />
              Unrecognised event: {item.eventType}
            </p>
          )}
        </li>
      );
  }
});

/**
 * A resolved permission's inline trace: unobtrusive text plus a
 * `StatusBadge` for the reason, not a new card -- a dogfood run's trust
 * question is "how was this decided", answered in one line, not a whole
 * component of its own weight (ui-redesign-parity.md's Validation note).
 * No badge at all when `reason` is absent or unrecognised, rather than a
 * misleading default -- an older server's payload (or a future reason
 * this build has never heard of) still renders, just without that detail.
 */
function PermissionRow({ item }: { item: TimelinePermissionItem }) {
  const resolution = item.reason ? PERMISSION_REASON_LABEL[item.reason] : undefined;
  return (
    <p className="flex items-center gap-1.5 px-0.5 text-xs text-foreground-muted">
      <ShieldCheck className="size-3 shrink-0" />
      Permission resolved
      {resolution && <StatusBadge status={resolution.status}>{resolution.label}</StatusBadge>}
    </p>
  );
}
