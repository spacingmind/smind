import { Icons } from "@/lib/icons";
import { cn } from "@/lib/utils";
import type { Task } from "@/lib/types";

/**
 * The pill that marks a task an automation started (ADR-0044): the automation
 * glyph plus the word "Automation", with the automation's name on hover (the
 * row itself stays one line). Below 18rem of sidebar width the label hides and
 * only the glyph stays, so the task title is never the thing that shrinks first. Renders nothing for a task with no link, so a
 * caller can drop it in unconditionally.
 */
export function AutomationChip({
  task,
  className,
}: {
  task: Pick<Task, "automationId" | "automationName">;
  className?: string;
}) {
  if (task.automationId === undefined) return null;
  const label = task.automationName ? `Started by automation: ${task.automationName}` : "Started by an automation";
  return (
    <span
      data-testid="automation-chip"
      title={label}
      aria-label={label}
      className={cn(
        "inline-flex shrink-0 items-center gap-1 rounded-full border border-border bg-muted px-1.5 text-ui-xs font-medium text-muted-foreground",
        className,
      )}
    >
      <Icons.automation aria-hidden className="size-3" />
      <span className="@max-[18rem]/sb:hidden">Automation</span>
    </span>
  );
}
