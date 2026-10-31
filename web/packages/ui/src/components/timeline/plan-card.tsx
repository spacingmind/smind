import { useState } from "react";

import { Icons } from "@/lib/icons";
import type { PlanEntry } from "@/hooks/use-run-timeline";

const STATUS_LABEL: Record<PlanEntry["status"], string> = {
  pending: "Pending",
  in_progress: "In progress",
  completed: "Completed",
};

/**
 * The run's plan, as the latest ACP plan session-update described it: a
 * collapsible card whose header tracks progress ("2 of 5 done") and whose
 * entries carry a status marker, the step's text (struck through once
 * completed) and a "High" marker for the one priority worth surfacing.
 *
 * `open` is computed once, at first render: entries keep arriving as new
 * `items`, but re-deriving the attribute then would collapse a card the
 * user had deliberately expanded (or reopen one they closed), so the
 * mount-time value is the only thing ever applied and a toggle sticks.
 */
export function PlanCard({ items }: { items: PlanEntry[] }) {
  const [initiallyOpen] = useState(() => !items.every((entry) => entry.status === "completed"));
  const done = items.filter((entry) => entry.status === "completed").length;

  return (
    <div data-testid="timeline-plan">
      <details open={initiallyOpen} className="group/details">
        <summary className="-mx-2 flex cursor-pointer items-center gap-2 rounded-md px-2 py-1 text-ui-sm text-foreground-muted select-none hover:bg-hover [&::-webkit-details-marker]:hidden">
          <Icons.chevronRight className="size-3 shrink-0 transition-transform group-open/details:rotate-90" />
          <Icons.listChecks className="size-3 shrink-0" />
          Plan
          <span data-testid="plan-progress" className="ml-auto">
            {done} of {items.length} done
          </span>
        </summary>
        <ul className="mt-1 ml-7 space-y-1 rounded-lg border border-border bg-surface/50 p-2">
          {items.map((entry, index) => (
            <li key={index} data-testid="plan-entry" data-status={entry.status} data-priority={entry.priority} className="flex items-center gap-2">
              <StatusIcon status={entry.status} />
              <span className="sr-only">{STATUS_LABEL[entry.status]}</span>
              <span
                data-chat-find-text="true"
                className={entry.status === "completed" ? "text-ui-sm line-through text-foreground-muted" : "text-ui-sm"}
              >
                {entry.content}
              </span>
              {entry.priority === "high" && <span className="text-ui-xs text-foreground-muted">High</span>}
            </li>
          ))}
        </ul>
      </details>
    </div>
  );
}

function StatusIcon({ status }: { status: PlanEntry["status"] }) {
  if (status === "completed") return <Icons.check className="size-3 shrink-0 text-success" />;
  if (status === "in_progress") return <Icons.spinner className="size-3 shrink-0 animate-spin text-warning motion-reduce:animate-none" />;
  return <Icons.circle className="size-3 shrink-0 text-foreground-muted" />;
}
