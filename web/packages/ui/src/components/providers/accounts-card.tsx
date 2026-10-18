import { useMemo, useState, type JSX } from "react";

import { useRunProviderTest } from "@/hooks/use-accounts";
import type { AccountRowData } from "@/hooks/use-account-rows";
import { useUsagePreferences } from "@/hooks/use-usage-preferences";
import { useUsageReports } from "@/hooks/use-usage-reports";
import {
  filterAccountRows,
  mostHeadroomAccountIds,
  sortAccountRows,
  type ProviderNavItem,
} from "@/lib/providers-view";
import type { Account, ProviderInfo } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";
import { useProvidersViewStore } from "@/stores/providers-view-store";
import { AccountRow } from "@/components/providers/account-row";
import { RemoveAccountDialog, type RowAction } from "@/components/providers/account-actions";
import { Input } from "@/components/ui/input";
import { SegmentedControl } from "@/components/ui/segmented-control";

export function AccountsCard(props: {
  item: ProviderNavItem;
  rows: AccountRowData[];
  providers: ProviderInfo[];
  allAccounts: Account[];
  client: WsClient | null;
  onRelogin: (a: Account) => void;
}): JSX.Element {
  const { item, rows: allRows, providers, allAccounts, client, onRelogin } = props;
  const [action, setAction] = useState<RowAction | null>(null);
  const { runTest, isTesting } = useRunProviderTest();
  const { snapshot, refreshOne } = useUsageReports();
  const { displayAs } = useUsagePreferences();

  const query = useProvidersViewStore((s) => s.query);
  const filter = useProvidersViewStore((s) => s.filter);
  const sort = useProvidersViewStore((s) => s.sort);
  const setQuery = useProvidersViewStore((s) => s.setQuery);
  const setFilter = useProvidersViewStore((s) => s.setFilter);
  const setSort = useProvidersViewStore((s) => s.setSort);

  const idSet = useMemo(() => new Set(item.accountIds), [item.accountIds]);
  const rows = useMemo(() => allRows.filter((r) => idSet.has(r.account.id)), [allRows, idSet]);
  const shown = useMemo(
    () => sortAccountRows(filterAccountRows(rows, { query, filter }), sort),
    [rows, query, filter, sort],
  );
  const headroom = useMemo(
    () => mostHeadroomAccountIds({ rows: rows.map((r) => ({ accountId: r.account.id, provider: r.account.provider, badge: r.badge, usage: r.usage })) }),
    [rows],
  );

  return (
    <div className="flex flex-col gap-3">
      {rows.length >= 4 ? (
        <div className="flex flex-wrap items-center gap-2">
          <Input
            aria-label="Search accounts"
            placeholder="Search accounts"
            data-testid="accounts-search"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            className="h-7 w-44"
          />
          <SegmentedControl
            label="Filter accounts"
            value={filter}
            onChange={setFilter}
            options={[
              { value: "all", label: "All" },
              { value: "attention", label: "Needs attention" },
              { value: "active", label: "Active" },
            ]}
          />
          <SegmentedControl
            label="Sort accounts"
            value={sort}
            onChange={setSort}
            options={[
              { value: "status", label: "Status" },
              { value: "name", label: "Name" },
            ]}
          />
        </div>
      ) : null}
      {shown.length === 0 ? (
        <p data-testid="accounts-no-match" className="text-ui-sm text-muted-foreground">
          No accounts match.
        </p>
      ) : (
        <ul className="flex flex-col gap-2" data-testid="accounts-list">
          {shown.map((row) => (
            <AccountRow
              key={row.account.id}
              data={row}
              mostHeadroom={headroom.has(row.account.id)}
              displayAs={displayAs}
              testing={isTesting(row.account.provider)}
              onTest={runTest}
              refresh={{
                state: snapshot.refreshing.has(row.account.id) ? "pending" : snapshot.failed.has(row.account.id) ? "failed" : "idle",
                pressedAt: snapshot.pressedAt[row.account.id],
                onRefresh: () => void refreshOne(row.account.id),
              }}
              action={action?.account.id === row.account.id ? action : null}
              onAction={setAction}
              onRelogin={onRelogin}
            />
          ))}
        </ul>
      )}
      {action?.kind === "remove" ? (
        <RemoveAccountDialog
          client={client}
          account={action.account}
          providers={providers}
          accounts={allAccounts}
          onRemoved={() => setAction(null)}
          onOpenChange={(open) => !open && setAction(null)}
        />
      ) : null}
    </div>
  );
}
