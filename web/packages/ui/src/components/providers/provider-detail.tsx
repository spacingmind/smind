import { useState, type JSX } from "react";
import { useQueryClient } from "@tanstack/react-query";

import type { ProviderNavItem } from "@/lib/providers-view";
import { providerConsumer } from "@/lib/providers-groups";
import { ProviderGlyph } from "@/components/composer/provider-glyph";
import { AccountsCard } from "@/components/providers/accounts-card";
import { TestButton } from "@/components/providers/account-actions";
import { ConnectAccountPanel } from "@/components/accounts-dialog";
import { Button } from "@/components/ui/button";
import { StatusDot } from "@/components/ui/status-dot";
import { useRunProviderTest } from "@/hooks/use-accounts";
import { useAccountRows } from "@/hooks/use-account-rows";
import { useProviderCatalog } from "@/hooks/use-provider-catalog";
import { useProvidersViewStore } from "@/stores/providers-view-store";
import { queryKeys } from "@/lib/query-keys";
import type { WsClient } from "@/lib/ws-client";

const KIND_LABEL: Record<ProviderNavItem["kind"], string> = {
  oauth: "OAuth account",
  "api-key": "API key account",
  cli: "Managed by its own CLI",
  other: "Other accounts",
};

export function ProviderDetail({
  item,
  client,
}: {
  item: ProviderNavItem | null;
  client: WsClient | null;
}): JSX.Element {
  const { providers } = useProviderCatalog();
  const { rows } = useAccountRows();
  const { runTest, isTesting } = useRunProviderTest();
  const queryClient = useQueryClient();
  const connectProviderId = useProvidersViewStore((s) => s.connectProviderId);
  const reloginAccountId = useProvidersViewStore((s) => s.reloginAccountId);
  const clearConnect = useProvidersViewStore((s) => s.clearConnect);
  const clearRelogin = useProvidersViewStore((s) => s.clearRelogin);
  const requestRelogin = useProvidersViewStore((s) => s.requestRelogin);
  const [showConnect, setShowConnect] = useState(false);
  if (!item) {
    return (
      <div data-testid="provider-detail" className="p-6">
        <p className="text-ui-base text-muted-foreground">Select a provider</p>
      </div>
    );
  }

  const idSet = new Set(item.accountIds);
  const itemRows = rows.filter((r) => idSet.has(r.account.id));
  const attention = itemRows.filter((r) => r.badge.kind === "relogin" || r.badge.kind === "cooldown" || r.badge.kind === "error").length;
  // The connect panel never renders for the other bucket or cli items; the
  // three ways it can open are mutually exclusive (the store flags win over
  // the local + Connect toggle).
  const canConnect = item.kind !== "other" && item.kind !== "cli" && item.accountProvider !== undefined;
  const connectOpen = canConnect && connectProviderId === item.id;
  const reloginAccount = canConnect ? itemRows.find((r) => r.account.id === reloginAccountId)?.account ?? null : null;
  const manualConnectOpen = canConnect && !connectOpen && !reloginAccount && showConnect;
  const onConnected = () => void queryClient.invalidateQueries({ queryKey: queryKeys.accountList() });

  return (
    <div data-testid="provider-detail" className="flex flex-col gap-3 p-4">
      <div className="flex flex-col gap-1">
        <div className="flex items-center gap-2">
          {item.id === "other" ? null : <ProviderGlyph provider={item.id} />}
          <h3 className="text-ui-lg font-medium">{item.label}</h3>
          {canConnect ? (
            <>
              <span className="flex-1" />
              <Button
              variant="outline"
              size="sm"
              data-testid="provider-detail-connect"
              onClick={() => setShowConnect((v) => !v)}
            >
                + Connect account
              </Button>
            </>
          ) : null}
        </div>
        <p className="text-ui-sm text-muted-foreground">
          {KIND_LABEL[item.kind]}
          {itemRows.length > 0 ? (
            <span data-testid="provider-detail-summary">
              {` · ${itemRows.length} account${itemRows.length === 1 ? "" : "s"}`}
              {attention > 0
                ? ` · ${attention} need${attention === 1 ? "s" : ""} attention`
                : " · all healthy"}
            </span>
          ) : null}
        </p>
        {item.kind === "other" ? (
          <p className="text-ui-sm text-muted-foreground">
            These accounts' providers have no sMind consumer — no agent runner and no proxy endpoint uses them today.
          </p>
        ) : null}
        {item.accountProvider && providerConsumer(item.accountProvider) === "proxy" ? (
          <p className="text-ui-xs text-muted-foreground" data-testid={`provider-group-note-${item.id}`}>
            Accounts here route sMind's /v1 proxy; agent runs use the CLI's own login.
          </p>
        ) : null}
      </div>

      {connectOpen ? (
        <ConnectAccountPanel
          client={client}
          providers={providers}
          initialProviderId={item.accountProvider}
          autoStart
          onClose={clearConnect}
          onConnected={onConnected}
        />
      ) : null}
      {reloginAccount ? (
        <ConnectAccountPanel
          client={client}
          providers={providers}
          relogin={reloginAccount}
          autoStart
          onClose={clearRelogin}
          onConnected={onConnected}
        />
      ) : null}
      {manualConnectOpen ? (
        <ConnectAccountPanel
          client={client}
          providers={providers}
          initialProviderId={item.accountProvider}
          onClose={() => setShowConnect(false)}
          onConnected={onConnected}
        />
      ) : null}

      {item.kind === "cli" ? (
        <div
          className="flex items-center justify-between gap-2 rounded-lg px-2 py-1.5"
          data-testid={`provider-external-${item.id}`}
        >
          <span className="flex flex-col gap-0.5">
            <span className="text-ui-base text-muted-foreground">Managed externally via CLI</span>
            <span className="text-ui-xs text-muted-foreground">
              Authenticates itself via its own CLI login — accounts added here aren't read by it.
            </span>
          </span>
          <TestButton providerId={item.id} testing={isTesting(item.id)} onTest={runTest} />
        </div>
      ) : itemRows.length === 0 ? (
        <div
          className="flex items-center justify-between gap-2 rounded-lg px-2 py-1.5"
          data-testid={`provider-not-connected-${item.id}`}
        >
          <span className="flex items-center gap-1.5 text-ui-base text-muted-foreground">
            <StatusDot status="neutral" className="size-2" />
            Not connected
          </span>
          <Button
            variant="default"
            size="xs"
            data-testid={`provider-connect-${item.id}`}
            onClick={() => useProvidersViewStore.getState().requestConnect(item.id)}
          >
            {`Connect ${item.label}`}
          </Button>
        </div>
      ) : (
        <AccountsCard
          item={item}
          rows={itemRows}
          providers={providers}
          allAccounts={rows.map((r) => r.account)}
          client={client}
          onRelogin={(a) => requestRelogin(a.id, item.id)}
        />
      )}
    </div>
  );
}
