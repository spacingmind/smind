import { useState, type ReactNode } from "react";
import { ChevronRight } from "lucide-react";

import { StatusDot, type StatusDotStatus } from "@/components/ui/status-dot";
import type { TimelineToolCallItem } from "@/hooks/use-run-timeline";
import type { ToolCallStatus } from "@/lib/types";
import { cn } from "@/lib/utils";

/** Tool-call status -> the StatusDot signal for it. */
const STATUS_DOT: Record<ToolCallStatus, StatusDotStatus> = {
  running: "running",
  success: "success",
  failure: "danger",
};

/** A tool's arguments/result as readable text, whatever shape the provider used. */
export function stringifyToolPayload(value: unknown): string {
  if (value === undefined || value === null) return "";
  if (typeof value === "string") return value;
  try {
    return JSON.stringify(value, null, 2);
  } catch {
    // Cyclic or otherwise unserializable: a tool card must never be the
    // thing that takes the transcript down.
    return String(value);
  }
}

/**
 * The tool-call card frame: icon, display name, one-line summary,
 * lifecycle status, and a disclosure for the detail body. Identical
 * chrome for every intent — only the body differs — so a transcript of
 * mixed tools reads as one list rather than five.
 *
 * The card knows nothing about any specific tool: which icon, summary and
 * body it gets comes from the registry (`tool-renderers.tsx`), which is
 * why adding a tool renderer never touches this file.
 */
export function ToolCallCard({
  item,
  icon: Icon,
  label,
  summary,
  onOpenPath,
  children,
}: {
  item: TimelineToolCallItem;
  icon: React.ComponentType<{ className?: string }>;
  /** Display name; defaults to the wire tool name. */
  label?: string;
  /** The one-line summary. Falls back to the daemon-supplied title. */
  summary?: string;
  /** Set when this call names a file the UI can open -- makes the summary click-through. */
  onOpenPath?: () => void;
  children?: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const name = label || item.toolName || item.toolCallId;
  const line = summary || item.title || "";

  return (
    <div
      data-testid="timeline-tool-call"
      data-tool-call-id={item.toolCallId}
      data-tool-name={item.toolName ?? ""}
      data-status={item.status}
      className="rounded-xl border bg-card"
    >
      <div className="flex items-center gap-2 px-2.5 py-1.5 text-ui-sm">
        <button
          type="button"
          aria-expanded={open}
          aria-label={`${open ? "Collapse" : "Expand"} ${name} detail`}
          onClick={() => setOpen((prev) => !prev)}
          data-testid="tool-call-toggle"
          className="flex min-w-0 flex-1 items-center gap-2 text-left outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
        >
          <ChevronRight className={cn("size-3 shrink-0 transition-transform", open && "rotate-90")} />
          <Icon className="size-3 shrink-0 text-foreground-muted" />
          <span className="shrink-0 font-medium" data-chat-find-text="true">
            {name}
          </span>
          {line && !onOpenPath && (
            <span
              className="min-w-0 flex-1 truncate font-mono text-foreground-muted"
              data-testid="tool-call-summary"
              data-chat-find-text="true"
            >
              {line}
            </span>
          )}
        </button>

        {/* The path is its own button so the card can be expanded *and*
            the file opened -- nesting them would make one unreachable. */}
        {line && onOpenPath && (
          <button
            type="button"
            onClick={onOpenPath}
            data-testid="tool-call-open-path"
            className="min-w-0 flex-1 truncate text-left font-mono text-foreground-muted underline-offset-2 outline-none hover:underline focus-visible:ring-3 focus-visible:ring-ring/50"
          >
            <span data-testid="tool-call-summary" data-chat-find-text="true">
              {line}
            </span>
          </button>
        )}

        <span className="flex shrink-0 items-center gap-1 text-foreground-muted">
          <StatusDot status={STATUS_DOT[item.status]} />
          <span data-testid="tool-call-status">{item.status}</span>
        </span>
      </div>

      {open && (
        <div className="border-t px-2.5 py-2 text-ui-sm" data-testid="tool-call-detail">
          {children}
        </div>
      )}
    </div>
  );
}

export function ToolPayload({ label, body, testId }: { label: string; body: string; testId?: string }) {
  return (
    <div className="mt-1 first:mt-0">
      <p className="text-ui-sm font-medium tracking-wide uppercase text-foreground-muted">{label}</p>
      <pre data-testid={testId} className="mt-0.5 overflow-x-auto rounded-lg bg-surface p-2 font-mono whitespace-pre-wrap">
        {body}
      </pre>
    </div>
  );
}
