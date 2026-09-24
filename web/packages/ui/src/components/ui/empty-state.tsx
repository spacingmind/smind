import type { ReactNode } from "react";

import { cn } from "@/lib/utils";

/**
 * The shared empty-state block (ui-redesign-parity plan, Item 2):
 * centered, muted, a short noun-phrase title with an optional one-line
 * description and a single action -- refs/paseo/docs/design.md §11's
 * "the maximum elaboration" rule (no illustrations, no CTA stacks). Copy
 * convention (docs/design.md): sentence case, no trailing period --
 * "No runs yet", not "No runs yet." or "No Runs Yet.".
 */
export function EmptyState({
  icon,
  title,
  description,
  action,
  className,
  testId,
}: {
  icon?: ReactNode;
  title: ReactNode;
  description?: ReactNode;
  action?: ReactNode;
  className?: string;
  testId?: string;
}) {
  return (
    <div
      data-testid={testId}
      data-slot="empty-state"
      className={cn("flex flex-1 flex-col items-center justify-center gap-2 px-4 py-8 text-center", className)}
    >
      {icon && <div className="text-foreground-muted">{icon}</div>}
      <p className="text-ui-sm font-medium text-foreground">{title}</p>
      {description && <p className="max-w-sm text-ui-sm text-foreground-muted">{description}</p>}
      {action && <div className="mt-2">{action}</div>}
    </div>
  );
}
