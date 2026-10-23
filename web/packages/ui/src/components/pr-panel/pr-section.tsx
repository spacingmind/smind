import { useState, type ReactNode } from "react";

import { Icons } from "@/lib/icons";
import { cn } from "@/lib/utils";

/** A collapsible panel section (default open): chevron, title, a muted summary, and an optional action beside the toggle. */
export function PRSection({
  id,
  title,
  summary,
  action,
  children,
}: {
  id: string;
  title: string;
  summary?: ReactNode;
  action?: ReactNode;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(true);
  return (
    <section data-testid={`pr-section-${id}`} className="min-w-0 border-b last:border-b-0">
      <div className="flex min-w-0 items-center gap-2 px-4 py-2">
        <button
          type="button"
          aria-expanded={open}
          aria-controls={`pr-section-body-${id}`}
          onClick={() => setOpen((v) => !v)}
          className="flex min-w-0 flex-1 items-center gap-1.5 rounded-md text-left text-ui-base font-medium outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
        >
          <Icons.chevronRight aria-hidden className={cn("size-3.5 shrink-0 text-foreground-subtle transition-transform", open && "rotate-90")} />
          <span className="shrink-0">{title}</span>
          {summary && <span className="min-w-0 truncate text-ui-sm font-normal text-foreground-muted">{summary}</span>}
        </button>
        {action}
      </div>
      {open && (
        <div id={`pr-section-body-${id}`} className="min-w-0 px-4 pb-3">
          {children}
        </div>
      )}
    </section>
  );
}
