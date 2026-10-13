
import { ChatPlanUsage } from "@/components/account-usage/chat-plan-usage";
import { UsagePillCard } from "@/components/usage-pill-card";
import { useChatPlanUsage } from "@/hooks/use-chat-plan-usage";
import { EM_DASH, formatCost, formatTokens, usageSourceLabel } from "@/lib/usage-format";
import type { RunUsage } from "@/lib/types";
import { Icons } from "@/lib/icons";

/**
 * The footer's compact cost: like formatCost, but two decimals once the
 * cost is at least a cent -- the exact value lives in the card.
 */
function formatCompactCost(usd: number): string {
  return usd >= 0.01 ? `$${usd.toFixed(2)}` : formatCost(usd);
}

/**
 * The run footer's per-run usage pill (ADR-0020 UI follow-up). The inline
 * text is the one number a reading footer wants -- the cost (`$0.03`),
 * else the compact in+out total (`17.6k tokens`). A run that reported no
 * number at all keeps an icon-only pill (not nothing): its card is the
 * only place the run's ADR-0028 plan-usage section is reachable from, and
 * `usage === undefined` alone renders nothing (an old run, or one that has
 * not reported yet).
 *
 * The popover lists every field at full precision (null fields show as an
 * em dash there -- never 0, which is a real reported value), and the
 * pill's accessible name carries the full in/out/cost detail
 * (ADR-0028 PR-E keeps the card reachable by hover, focus and click).
 *
 * ADR-0028 PR-E: the card also carries the Plan usage section for the run's
 * provider (`provider`; omitted or one with no plan windows adds nothing).
 */
export function RunUsagePill({ usage, provider }: { usage: RunUsage | undefined; provider?: string | null }) {
  const plan = useChatPlanUsage(provider);
  if (usage === undefined) return null;

  const hasInput = usage.inputTokens !== null && usage.inputTokens !== undefined;
  const hasOutput = usage.outputTokens !== null && usage.outputTokens !== undefined;
  const hasCost = usage.costUsd !== null && usage.costUsd !== undefined;

  const inline = hasCost
    ? formatCompactCost(usage.costUsd!)
    : hasInput || hasOutput
      ? `${formatTokens((usage.inputTokens ?? 0) + (usage.outputTokens ?? 0), "compact")} tokens`
      : null;

  const labelParts: string[] = [];
  if (hasInput) labelParts.push(`${formatTokens(usage.inputTokens, "compact")} in`);
  if (hasOutput) labelParts.push(`${formatTokens(usage.outputTokens, "compact")} out`);
  if (hasCost) labelParts.push(formatCost(usage.costUsd));
  if (labelParts.length === 0) labelParts.push("Usage not reported");
  const label = `Usage: ${labelParts.join(", ")}`;

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

  return (
    <UsagePillCard
      testId="run-usage-pill"
      label={label}
      pill={
        <>
          <Icons.tokens className="size-3" />
          {inline !== null && <span className="truncate">{inline}</span>}
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
