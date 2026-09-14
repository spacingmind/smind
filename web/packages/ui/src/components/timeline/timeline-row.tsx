import { memo } from "react";
import { CircleHelp } from "lucide-react";

import { TimelineMarkdown } from "@/components/timeline/timeline-markdown";
import { ToolCallCard } from "@/components/timeline/tool-call-card";
import type { TimelineItem } from "@/hooks/use-run-timeline";

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
export const TimelineRow = memo(function TimelineRow({ item }: { item: TimelineItem }) {
  switch (item.kind) {
    case "user":
      return (
        <li data-testid="timeline-user" data-item-kind="user" className="flex justify-end">
          <div className="max-w-[85%] rounded-lg bg-surface-2 px-2.5 py-1.5 text-sm whitespace-pre-wrap">
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
            <div className="border-t border-dashed px-2.5 py-1.5 text-xs whitespace-pre-wrap text-foreground-muted">
              {item.text}
            </div>
          </details>
        </li>
      );

    case "tool_call":
      return (
        <li data-item-kind="tool_call">
          <ToolCallCard item={item} />
        </li>
      );

    default:
      // An event type this build has never heard of. The daemon's enum is
      // append-only (ADR 0008), so this is a supported state: say what
      // arrived and keep the rest of the transcript intact.
      return (
        <li data-testid="timeline-unknown" data-item-kind="unknown">
          <p className="flex items-center gap-1.5 rounded-lg border border-dashed px-2.5 py-1 text-xs text-foreground-muted">
            <CircleHelp className="size-3 shrink-0" />
            Unrecognised event: {item.eventType}
          </p>
        </li>
      );
  }
});
