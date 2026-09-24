import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";
import { StatusDot, type StatusDotStatus } from "@/components/ui/status-dot";

export type StatusBadgeStatus = Exclude<StatusDotStatus, "neutral">;

const STATUS_BADGE_CLASSES: Record<StatusBadgeStatus, string> = {
  success: "border-status-success/30 bg-status-success/10 text-status-success",
  danger: "border-status-danger/30 bg-status-danger/10 text-status-danger",
  warning: "border-status-warning/30 bg-status-warning/10 text-status-warning",
  running: "border-status-running/30 bg-status-running/10 text-status-running",
};

/**
 * A status pill: the status token for text on a neutral tinted shell
 * (refs/paseo/docs/design.md §13 -- "the neutral shell keeps the signal
 * legible without manufacturing translucent colors outside the theme").
 * The canonical pill primitive -- a new status surface uses this rather
 * than a bespoke `<span>` with its own color classes.
 */
export function StatusBadge({
  status,
  dot = false,
  className,
  children,
  ...props
}: {
  status: StatusBadgeStatus;
  /** Renders a matching StatusDot before the label -- for a pill that also needs the louder dot-band signal (e.g. a running indicator), not just the text tier. */
  dot?: boolean;
  className?: string;
} & Omit<ComponentProps<"span">, "className">) {
  return (
    <span
      data-slot="status-badge"
      data-status={status}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-ui-xs font-medium transition-colors duration-(--duration-hover)",
        STATUS_BADGE_CLASSES[status],
        className,
      )}
      {...props}
    >
      {dot && <StatusDot status={status} />}
      {children}
    </span>
  );
}
