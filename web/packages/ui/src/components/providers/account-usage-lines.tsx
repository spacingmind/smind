import type { JSX } from "react";

import { UsageBalanceBar } from "@/components/account-usage/usage-balance-bar";
import { UsageWindowBar } from "@/components/account-usage/usage-window-bar";
import { formatUsageFreshness } from "@/lib/account-usage/model";
import type { UsageDisplayAs } from "@/lib/account-usage/format";
import type { AccountUsage, UsageBalance, UsageWindow } from "@/lib/account-usage/types";

/** The entries of a list field that are objects; a reshaped payload (null, a number) is skipped, never thrown on (AC39). */
function list<T>(value: T[] | undefined | null): T[] {
  return Array.isArray(value) ? value.filter((entry) => entry !== null && typeof entry === "object") : [];
}

/** The account row's usage summary: the summary windows (or the first two), the first balance, freshness. */
export function AccountUsageLines(props: {
  usage: AccountUsage;
  displayAs: UsageDisplayAs;
  now?: number;
}): JSX.Element | null {
  const { usage, displayAs, now } = props;
  const report = usage.report;
  if (report?.status !== "available") return null;
  const windows = list<UsageWindow>(report.windows);
  const summary = windows.filter((w) => w.summary);
  const shown = summary.length > 0 ? summary : windows.slice(0, 2);
  const balances = list<UsageBalance>(report.balances);
  return (
    <div className="flex flex-col gap-1.5" data-testid="account-usage-lines">
      {shown.map((window, i) => (
        <UsageWindowBar key={window.id ?? i} window={window} displayAs={displayAs} />
      ))}
      {balances[0] ? <UsageBalanceBar balance={balances[0]} /> : null}
      <p className="text-ui-xs text-muted-foreground" data-testid="account-usage-fresh">
        {formatUsageFreshness(report.fetchedAt, now)}
      </p>
    </div>
  );
}
