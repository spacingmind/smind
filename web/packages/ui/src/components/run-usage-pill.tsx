import { Coins } from "lucide-react";

import { ChatPlanUsage } from "@/components/account-usage/chat-plan-usage";
import { UsagePillCard } from "@/components/usage-pill-card";
import { useChatPlanUsage } from "@/hooks/use-chat-plan-usage";
import { EM_DASH, formatCost, formatTokens, usageSourceLabel } from "@/lib/usage-format";
import type { RunUsage } from "@/lib/types";

/**
 * The run footer's per-run usage pill (ADR-0020 UI follow-up): a compact,
 * muted one-liner of the run's reported usage, with a popover listing
 * every field at full precision. Null fields are omitted from the pill
 * and shown as an em dash in the popover -- never 0, which is a real
 * reported value. Renders nothing when the run has no usage at all
 * (an old run, or one that has not reported yet).
 *
 * ADR-0028 PR-E: the card also carries the Plan usage section for the run's
 * provider (`provider`; omitted or one with no plan windows adds nothing).
 */
export function RunUsagePill({ usage, provider }: { usage: RunUsage | undefined; provider?: string | null }) {
  const plan = useChatPlanUsage(provider);
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
    <UsagePillCard
      testId="run-usage-pill"
      label={label}
      pill={
        <>
          <Coins className="size-3" />
          <span className="truncate">{parts.join(" · ")}</span>
        </>
      }
      contentTestId="run-usage-popover"
      borderless
    >
      {(close) => (
        <>
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
          <ChatPlanUsage plan={plan} onOpenUsage={close} />
        </>
      )}
    </UsagePillCard>
  );
}
