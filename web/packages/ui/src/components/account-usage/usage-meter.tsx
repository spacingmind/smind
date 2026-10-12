import { cn } from "@/lib/utils";
import type { UsageTone } from "@/lib/account-usage/types";

const FILL_CLASSES: Record<UsageTone, string> = {
  ok: "bg-success",
  warning: "bg-warning",
  danger: "bg-destructive",
  default: "bg-foreground-muted",
};

function clampPct(percent: number): number {
  if (!Number.isFinite(percent)) return 0;
  return Math.min(100, Math.max(0, percent));
}

/**
 * A window's share as a bar, coloured by its tone (Paseo's UsageMeter).
 * `quiet` fills it with a surface colour instead, for summaries where a row
 * of coloured bars would be noise. The caller sizes the track's width.
 */
export function UsageMeter({
  percent,
  tone,
  quiet = false,
  className,
}: {
  percent: number;
  tone: UsageTone;
  quiet?: boolean;
  className?: string;
}) {
  const width = clampPct(percent);
  return (
    <div
      data-slot="usage-meter"
      data-tone={tone}
      className={cn("h-1 overflow-hidden rounded-full bg-muted", className)}
    >
      <div
        data-slot="usage-meter-fill"
        className={cn("h-full rounded-full", quiet ? "bg-foreground/25" : FILL_CLASSES[tone])}
        style={{ width: `${width}%` }}
      />
    </div>
  );
}
