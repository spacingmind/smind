import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

export type StatusDotStatus = "success" | "danger" | "warning" | "running" | "neutral";

const STATUS_DOT_CLASSES: Record<StatusDotStatus, string> = {
  success: "bg-success",
  danger: "bg-destructive",
  warning: "bg-warning",
  running: "bg-warning animate-pulse",
  neutral: "bg-foreground-muted/40",
};

/**
 * A small filled status dot. `running` shares ZCode's warning hue (no
 * distinct "running" color in refs/zcode/DESIGN.md's palette -- its own
 * workflow-timeline station lamps use `--color-warning` for running too).
 * `neutral` (not tested/unknown) isn't a signal at all, so it stays on the
 * plain muted-foreground scale rather than getting a status hue.
 */
export function StatusDot({
  status,
  className,
  ...props
}: { status: StatusDotStatus; className?: string } & Omit<ComponentProps<"span">, "className">) {
  return (
    <span
      data-slot="status-dot"
      data-status={status}
      className={cn("inline-block size-1.5 shrink-0 rounded-full", STATUS_DOT_CLASSES[status], className)}
      {...props}
    />
  );
}
