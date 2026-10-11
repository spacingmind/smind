import { Sigma } from "lucide-react";

import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { formatCost, formatTokens, sumUsage } from "@/lib/usage-format";
import type { RunUsage } from "@/lib/types";

/**
 * The chat header's usage total (ADR-0020 UI follow-up): sumUsage over
 * the chat's own runs -- the same state the per-run pills read, so the
 * total always equals the sum of its runs' pills and never double counts.
 * Null fields are omitted from the pill and shown as an em dash in the
 * popover, never 0. Renders nothing when no run reported usage. There is
 * deliberately no "total tokens" figure: input and cached overlap
 * differently per provider, so tokens are only ever shown per field.
 */
export function ChatUsageTotal({ runs }: { runs: ReadonlyArray<{ usage?: RunUsage }> | null | undefined }) {
  if (runs === null || runs === undefined) return null;

  const totals = sumUsage(runs.map((r) => r.usage));
  if (totals.reportedRuns === 0) return null;

  const parts: string[] = [];
  if (totals.inputTokens !== null) parts.push(`${formatTokens(totals.inputTokens, "compact")} in`);
  if (totals.outputTokens !== null) parts.push(`${formatTokens(totals.outputTokens, "compact")} out`);
  if (totals.costUsd !== null) parts.push(formatCost(totals.costUsd));

  const rows: Array<[string, string]> = [
    ["Input", formatTokens(totals.inputTokens)],
    ["Cached", formatTokens(totals.cachedInputTokens)],
    ["Cache write", formatTokens(totals.cacheWriteTokens)],
    ["Output", formatTokens(totals.outputTokens)],
    ["Reasoning", formatTokens(totals.reasoningTokens)],
    ["Cost", formatCost(totals.costUsd)],
  ];

  const label = `Usage total: ${parts.join(", ")}`;

  return (
    <Popover>
      <PopoverTrigger
        type="button"
        data-testid="chat-usage-total"
        aria-label={label}
        className="flex shrink-0 items-center gap-1 truncate rounded-full border px-2 py-0.5 text-ui-sm text-foreground-muted transition-colors hover:bg-hover hover:text-foreground"
      >
        <Sigma className="size-3" />
        <span className="truncate">{parts.join(" · ")}</span>
      </PopoverTrigger>
      <PopoverContent data-testid="chat-usage-total-popover" align="start" className="w-64 p-3">
        <p className="text-ui-sm font-medium text-foreground">Usage total</p>
        <ul className="mt-2 space-y-1">
          {rows.map(([name, value]) => (
            <li key={name} className="flex items-baseline justify-between gap-2 text-ui-sm">
              <span className="text-foreground-muted">{name}</span>
              <span className="truncate text-foreground">{value}</span>
            </li>
          ))}
        </ul>
        {totals.reportedRuns < totals.totalRuns && (
          <p className="mt-2 text-ui-sm text-foreground-muted">
            {totals.reportedRuns} of {totals.totalRuns} runs reported usage
          </p>
        )}
      </PopoverContent>
    </Popover>
  );
}
