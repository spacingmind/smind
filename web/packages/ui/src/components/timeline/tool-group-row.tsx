import { useState } from "react";
import { ChevronRight, Layers } from "lucide-react";

import { ToolCall } from "@/components/timeline/tool-call";
import type { TimelineRowContext } from "@/components/timeline/timeline-row";
import { StatusDot, type StatusDotStatus } from "@/components/ui/status-dot";
import type { TimelineToolCallItem } from "@/hooks/use-run-timeline";
import { cn } from "@/lib/utils";

/**
 * The aggregate signal for a collapsed run of tool calls: any failure
 * dominates, then anything still running, else success. One row must not
 * be able to hide a failed call behind a green dot.
 */
export function groupStatus(items: TimelineToolCallItem[]): StatusDotStatus {
  if (items.some((i) => i.status === "failure")) return "danger";
  if (items.some((i) => i.status === "running")) return "running";
  return "success";
}

/** A run of consecutive tool calls collapsed into one row (`overview` detail level). */
export function ToolGroupRow({ items, worktreePath, onOpenFile }: { items: TimelineToolCallItem[] } & TimelineRowContext) {
  const [open, setOpen] = useState(false);
  const names = [...new Set(items.map((i) => i.toolName).filter((n): n is string => !!n))];

  return (
    <li data-testid="timeline-tool-group" data-item-kind="tool-group" data-count={items.length}>
      <div className="rounded-xl border bg-card">
        <button
          type="button"
          aria-expanded={open}
          onClick={() => setOpen((prev) => !prev)}
          data-testid="tool-group-toggle"
          className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-ui-sm outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
        >
          <ChevronRight className={cn("size-3 shrink-0 transition-transform", open && "rotate-90")} />
          <Layers className="size-3 shrink-0 text-foreground-muted" />
          <span className="shrink-0 font-medium">{items.length} tool calls</span>
          {names.length > 0 && (
            <span className="min-w-0 flex-1 truncate text-foreground-muted">{names.join(", ")}</span>
          )}
          <span className="ml-auto shrink-0">
            <StatusDot status={groupStatus(items)} />
          </span>
        </button>

        {open && (
          <div className="space-y-1.5 border-t px-2.5 py-2" data-testid="tool-group-detail">
            {items.map((item) => (
              <ToolCall key={item.id} item={item} worktreePath={worktreePath} onOpenFile={onOpenFile} />
            ))}
          </div>
        )}
      </div>
    </li>
  );
}
