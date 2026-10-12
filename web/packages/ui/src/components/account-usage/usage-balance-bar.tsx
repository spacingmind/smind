import { UsageMeter } from "@/components/account-usage/usage-meter";
import { formatAmount } from "@/lib/account-usage/format";
import type { UsageBalance } from "@/lib/account-usage/types";

const EM_DASH = "—";

function resolveBalance(balance: UsageBalance): { amountText: string; usedPct: number | null } {
  const { used, remaining, limit, unit } = balance;
  const format = (value: number) => formatAmount(value, unit, "en-US");
  if (limit != null && limit > 0) {
    const usedAmount = used ?? (remaining != null ? limit - remaining : null);
    const usedText = usedAmount != null ? format(usedAmount) : EM_DASH;
    return {
      amountText: `${usedText} / ${format(limit)}`,
      usedPct: usedAmount != null ? (usedAmount / limit) * 100 : null,
    };
  }
  if (remaining != null) return { amountText: `${format(remaining)} left`, usedPct: null };
  if (used != null) return { amountText: format(used), usedPct: null };
  return { amountText: EM_DASH, usedPct: null };
}

/** A balance (credits, dollars) as "used / limit" or "x left"; a meter only when how much of the limit is used is known. */
export function UsageBalanceBar({ balance }: { balance: UsageBalance }) {
  const { amountText, usedPct } = resolveBalance(balance);
  return (
    <div data-testid={`usage-balance-${balance.id}`} className="flex flex-col gap-1 py-1">
      <div className="flex items-center justify-between gap-2 text-ui-sm">
        <span className="min-w-0 flex-1 truncate text-muted-foreground">{balance.label}</span>
        <span className="shrink-0 font-medium text-foreground">{amountText}</span>
      </div>
      {usedPct != null ? <UsageMeter percent={usedPct} tone="default" /> : null}
    </div>
  );
}
