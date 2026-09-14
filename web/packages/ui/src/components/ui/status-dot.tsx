import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

export type StatusDotStatus = "success" | "danger" | "warning" | "running" | "neutral";

const STATUS_DOT_CLASSES: Record<StatusDotStatus, string> = {
  success: "bg-status-dot-success",
  danger: "bg-status-dot-danger",
  warning: "bg-status-dot-warning",
  running: "bg-status-dot-running animate-pulse",
  neutral: "bg-foreground-muted/40",
};

/**
 * A small filled status dot -- refs/paseo/docs/design.md §13's exception
 * to "one token per signal": a dot is 6px of solid color with no shape or
 * label, so it reads dimmer than the metadata beside it at the regular
 * status-family chroma, and needs its own higher-chroma `status-dot-*`
 * token band (see index.css). `neutral` (not tested/unknown) isn't a
 * signal at all, so it stays on the plain muted-foreground scale rather
 * than getting a fifth status hue.
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
