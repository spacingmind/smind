import { useState } from "react";
import { ChevronRight, Wrench } from "lucide-react";

import { StatusDot } from "@/components/ui/status-dot";
import type { TimelineToolCallItem } from "@/hooks/use-run-timeline";
import type { StatusDotStatus } from "@/components/ui/status-dot";
import type { ToolCallStatus } from "@/lib/types";
import { cn } from "@/lib/utils";

/** Tool-call status -> the StatusDot variant that signals it. */
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
 * The generic tool-call card: icon, display name, one-line summary,
 * lifecycle status, and a disclosure for the raw input/result.
 *
 * This is the fallback every unregistered tool name lands on (Item 9's
 * registry picks a specialised body where one exists) and the frame the
 * specialised ones render inside, so the chrome is identical across all
 * of them.
 */
export function ToolCallCard({
  item,
  summary,
  children,
}: {
  item: TimelineToolCallItem;
  /** The one-line summary shown next to the name. Defaults to the title the daemon sent. */
  summary?: string;
  /** A specialised detail body; the raw input/result JSON is shown when absent. */
  children?: React.ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const name = item.toolName || item.toolCallId;
  const line = summary ?? item.title ?? "";
  const input = stringifyToolPayload(item.input);
  const result = stringifyToolPayload(item.result);

  return (
    <div
      data-testid="timeline-tool-call"
      data-tool-call-id={item.toolCallId}
      data-tool-name={item.toolName ?? ""}
      data-status={item.status}
      className="rounded-lg border bg-surface-1"
    >
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((prev) => !prev)}
        data-testid="tool-call-toggle"
        className="flex w-full items-center gap-2 px-2.5 py-1.5 text-left text-xs outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
      >
        <ChevronRight className={cn("size-3 shrink-0 transition-transform", open && "rotate-90")} />
        <Wrench className="size-3 shrink-0 text-foreground-muted" />
        <span className="shrink-0 font-medium">{name}</span>
        {line && (
          <span className="min-w-0 flex-1 truncate text-foreground-muted" data-testid="tool-call-summary">
            {line}
          </span>
        )}
        <span className="ml-auto flex shrink-0 items-center gap-1 text-foreground-muted">
          <StatusDot status={STATUS_DOT[item.status]} />
          <span data-testid="tool-call-status">{item.status}</span>
        </span>
      </button>

      {open && (
        <div className="border-t px-2.5 py-2 text-xs" data-testid="tool-call-detail">
          {children ?? (
            <>
              {input && <ToolPayload label="Input" body={input} testId="tool-call-input" />}
              {result && <ToolPayload label="Result" body={result} testId="tool-call-result" />}
              {!input && !result && <p className="text-foreground-muted">No detail recorded</p>}
            </>
          )}
        </div>
      )}
    </div>
  );
}

export function ToolPayload({ label, body, testId }: { label: string; body: string; testId?: string }) {
  return (
    <div className="mt-1 first:mt-0">
      <p className="text-[0.7rem] uppercase text-foreground-muted">{label}</p>
      <pre data-testid={testId} className="mt-0.5 overflow-x-auto rounded bg-surface-2 p-2 whitespace-pre-wrap">
        {body}
      </pre>
    </div>
  );
}
