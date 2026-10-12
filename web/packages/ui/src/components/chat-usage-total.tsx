import { Gauge, Sigma } from "lucide-react";

import { ChatPlanUsage } from "@/components/account-usage/chat-plan-usage";
import { UsagePillCard } from "@/components/usage-pill-card";
import { useChatPlanUsage } from "@/hooks/use-chat-plan-usage";
import { hasPlanWindows } from "@/lib/account-usage/chat-provider";
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
 *
 * ADR-0028 PR-E: for a plan-backed provider (`provider`, the chat's) the
 * card also carries a Plan usage section, and a chat with no reported tokens
 * still shows the pill when the cached plan data has something to say.
 */
export function ChatUsageTotal({
  runs,
  provider,
}: {
  runs: ReadonlyArray<{ usage?: RunUsage }> | null | undefined;
  /** The chat's provider id; omitted (or one with no plan windows) leaves the token card unchanged. */
  provider?: string | null;
}) {
  const plan = useChatPlanUsage(provider);
  if (runs === null || runs === undefined) return null;

  const totals = sumUsage(runs.map((r) => r.usage));
  if (totals.reportedRuns === 0) {
    return hasPlanWindows(plan.accounts) ? <PlanOnlyPill plan={plan} /> : null;
  }

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
    <UsagePillCard
      contentTestId="chat-usage-total-popover"
      testId="chat-usage-total"
      label={label}
      pill={
        <>
          <Sigma className="size-3" />
          <span className="truncate">{parts.join(" · ")}</span>
        </>
      }
    >
      {(close) => (
        <>
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
          <ChatPlanUsage plan={plan} onOpenUsage={close} />
        </>
      )}
    </UsagePillCard>
  );
}

/** A chat with plan data but no reported tokens: the pill is just the Plan usage card. */
function PlanOnlyPill({ plan }: { plan: ReturnType<typeof useChatPlanUsage> }) {
  return (
    <UsagePillCard
      contentTestId="chat-usage-total-popover"
      testId="chat-usage-total"
      label="Plan usage"
      pill={
        <>
          <Gauge className="size-3" />
          <span className="truncate">Plan usage</span>
        </>
      }
    >
      {(close) => <ChatPlanUsage plan={plan} onOpenUsage={close} standalone />}
    </UsagePillCard>
  );
}
