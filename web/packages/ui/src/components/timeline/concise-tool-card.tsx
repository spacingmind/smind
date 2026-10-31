import type { TimelineToolCallItem } from "@/hooks/use-run-timeline";
import { toolResultText } from "@/components/timeline/tool-call-detail";
import type { ToolIntent } from "@/components/timeline/tool-renderers";
import { summarizeError } from "@/lib/error-summary";
import { Icons, type IconComponent } from "@/lib/icons";
import type { ToolCallStatus } from "@/lib/types";
import { cn } from "@/lib/utils";

/** Office mode (ADR-0049 §1.5) simplifies only these intents; everything else keeps its full card. */
export function isConciseIntent(intent: ToolIntent): boolean {
  return intent === "terminal" || intent === "edit";
}

const STATUS: Record<ToolCallStatus, { icon: IconComponent; className: string; text: string }> = {
  success: { icon: Icons.check, className: "text-success", text: "done" },
  failure: { icon: Icons.close, className: "text-destructive", text: "failed" },
  running: { icon: Icons.spinner, className: "animate-spin text-warning", text: "running" },
};

function baseName(path: string): string {
  return path.split(/[\/]/).filter(Boolean).pop() ?? path;
}

/** The one plain-language line: never the command, never a diff. */
export function conciseTitle(intent: ToolIntent, filePath: string | undefined): string {
  if (intent === "terminal") return "Ran a command";
  return filePath ? `Edited ${baseName(filePath)}` : "Edited a file";
}

/**
 * The office-mode card for a terminal or edit call: icon, one line, status. No expand control and
 * no command text, output or diff. A failure adds the one-line error summary so it is never silent.
 */
export function ConciseToolCard({
  item,
  icon: Icon,
  intent,
  filePath,
  onOpenPath,
}: {
  item: TimelineToolCallItem;
  icon: IconComponent;
  intent: ToolIntent;
  filePath?: string;
  onOpenPath?: () => void;
}) {
  const title = conciseTitle(intent, filePath);
  const { icon: StatusIcon, className, text } = STATUS[item.status];
  const failure = item.status === "failure" ? summarizeError(toolResultText(item)).summary : "";

  return (
    <div
      data-testid="timeline-tool-call"
      data-tool-call-id={item.toolCallId}
      data-tool-name={item.toolName ?? ""}
      data-status={item.status}
      data-concise="true"
      className="-mx-2"
    >
      <div className="flex items-center gap-2 rounded-md px-2 py-1 text-ui-sm text-foreground-muted">
        <Icon className="size-3 shrink-0" />
        {onOpenPath ? (
          <button
            type="button"
            onClick={onOpenPath}
            data-testid="tool-call-open-path"
            className="min-w-0 flex-1 truncate text-left font-medium text-foreground underline-offset-2 outline-none hover:underline focus-visible:ring-3 focus-visible:ring-ring/50"
          >
            <span data-testid="tool-call-summary" data-chat-find-text="true">
              {title}
            </span>
          </button>
        ) : (
          <span className="min-w-0 flex-1 truncate font-medium text-foreground" data-testid="tool-call-summary" data-chat-find-text="true">
            {title}
          </span>
        )}
        <span className="flex shrink-0 items-center gap-1">
          <span aria-hidden="true">
            <StatusIcon className={cn("size-3.5", className)} />
          </span>
          <span data-testid="tool-call-status" className={cn(item.status === "success" && "sr-only")}>
            {text}
          </span>
        </span>
      </div>
      {failure && (
        <p data-testid="tool-call-error-summary" className="px-2 pb-1 pl-7 text-ui-sm text-destructive" data-chat-find-text="true">
          {failure}
        </p>
      )}
    </div>
  );
}
