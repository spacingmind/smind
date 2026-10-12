import { Coins } from "lucide-react";

import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { EM_DASH, formatCost, formatTokens, usageSourceLabel } from "@/lib/usage-format";
import type { RunUsage } from "@/lib/types";

/**
 * The run footer's per-run usage pill (ADR-0020 UI follow-up): a compact,
 * muted one-liner of the run's reported usage, with a popover listing
 * every field at full precision. Null fields are omitted from the pill
 * and shown as an em dash in the popover -- never 0, which is a real
 * reported value. Renders nothing when the run has no usage at all
 * (an old run, or one that has not reported yet).
 */
export function RunUsagePill({ usage }: { usage: RunUsage | undefined }) {
  if (usage === undefined) return null;

  const parts: string[] = [];
  if (usage.inputTokens !== null && usage.inputTokens !== undefined) {
    parts.push(`${formatTokens(usage.inputTokens, "compact")} in`);
  }
  if (usage.outputTokens !== null && usage.outputTokens !== undefined) {
    parts.push(`${formatTokens(usage.outputTokens, "compact")} out`);
  }
  if (parts.length === 0) parts.push("Usage not reported");
  if (usage.costUsd !== null && usage.costUsd !== undefined) parts.push(formatCost(usage.costUsd));

  const rows: Array<[string, string]> = [
    ["Input", formatTokens(usage.inputTokens)],
    ["Cached", formatTokens(usage.cachedInputTokens)],
    ["Cache write", formatTokens(usage.cacheWriteTokens)],
    ["Output", formatTokens(usage.outputTokens)],
    ["Reasoning", formatTokens(usage.reasoningTokens)],
    ["Cost", formatCost(usage.costUsd)],
    ["Model", usage.model ?? EM_DASH],
    ["Context", `${formatTokens(usage.contextUsed)} / ${formatTokens(usage.contextSize)}`],
  ];

  const label = `Usage: ${parts.join(", ")}`;

  return (
    <Popover>
      <PopoverTrigger
        type="button"
        data-testid="run-usage-pill"
        aria-label={label}
        className="flex shrink-0 items-center gap-1 truncate rounded-full px-2 py-0.5 text-ui-sm text-foreground-muted transition-colors hover:bg-hover hover:text-foreground"
      >
        <Coins className="size-3" />
        <span className="truncate">{parts.join(" · ")}</span>
      </PopoverTrigger>
      <PopoverContent data-testid="run-usage-popover" align="start" className="w-64 p-3">
        <p className="text-ui-sm font-medium text-foreground">Usage</p>
        <ul className="mt-2 space-y-1">
          {rows.map(([name, value]) => (
            <li key={name} className="flex items-baseline justify-between gap-2 text-ui-sm">
              <span className="text-foreground-muted">{name}</span>
              <span className="truncate text-foreground">{value}</span>
            </li>
          ))}
        </ul>
        <p className="mt-2 text-ui-sm text-foreground-muted">Source: {usageSourceLabel(usage.source)}</p>
      </PopoverContent>
    </Popover>
  );
}
