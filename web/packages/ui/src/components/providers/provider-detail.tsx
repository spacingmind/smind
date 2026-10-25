import { Fragment, useEffect, useMemo, type JSX } from "react";
import { useQueryClient } from "@tanstack/react-query";

import type { ProviderNavItem } from "@/lib/providers-view";
import { providerConsumer } from "@/lib/providers-groups";
import { providerLabel } from "@/lib/provider-catalog";
import type { ProviderInfo } from "@/lib/types";
import { buildProviderDiagnostic } from "@/lib/provider-diagnostic";
import { ProviderIcon } from "@/components/provider-icon";
import { AccountsCard } from "@/components/providers/accounts-card";
import { ProviderDefaultModel } from "@/components/providers/provider-default-model";
import { ProviderCustomInfo } from "@/components/providers/provider-custom-info";
import { ProviderEnvField } from "@/components/providers/provider-env";
import { ProviderModels } from "@/components/providers/provider-models";
import { ProviderDiagnostics } from "@/components/providers/provider-diagnostics";
import { TestButton } from "@/components/providers/account-actions";
import { ConnectAccountPanel } from "@/components/accounts-dialog";
import { Button } from "@/components/ui/button";
import { StatusDot } from "@/components/ui/status-dot";
import { useRunProviderTest } from "@/hooks/use-accounts";
import { useAccountRows } from "@/hooks/use-account-rows";
import { useProviderCatalog } from "@/hooks/use-provider-catalog";
import { useProviderHealth } from "@/hooks/use-provider-health";
import { useProvidersViewStore } from "@/stores/providers-view-store";
import { queryKeys } from "@/lib/query-keys";
import type { WsClient } from "@/lib/ws-client";

const KIND_LABEL: Record<ProviderNavItem["kind"], string> = {
  oauth: "OAuth account",
  "api-key": "API key account",
  cli: "Managed by its own CLI",
  other: "Other accounts",
};

/** The detail's subtitle: how the provider authenticates, or what a providers.custom entry is. */
function kindLine(item: ProviderNavItem, info: ProviderInfo | null, providers: readonly ProviderInfo[]): string {
  if (info?.extends) return `Instance of ${providerLabel(info.extends, providers)} · its own config directory`;
  if (info?.custom) return "Custom ACP provider";
  return KIND_LABEL[item.kind];
}

function ReadinessLine({
  id,
  result,
  failed,
  usesCliLogin,
}: {
  id: string;
  result: { ok: boolean; detail: string } | undefined;
  failed: boolean;
  usesCliLogin: boolean;
}): JSX.Element {
  let status: "success" | "danger" | "neutral" = "neutral";
  let text = "Checking…";
  if (failed) {
    text = "Couldn't check";
  } else if (result) {
    status = result.ok ? "success" : "danger";
    text = result.ok ? (usesCliLogin ? "Ready (CLI login)" : "Ready") : `Not ready — ${result.detail}`;
  }
  return (
    <p className="flex items-center gap-1.5 text-ui-sm text-muted-foreground" data-testid={`provider-readiness-${id}`}>
      <StatusDot status={status} className="size-2" />
      {text}
    </p>
  );
}

export function ProviderDetail({
  item,
  client,
}: {
  item: ProviderNavItem | null;
  client: WsClient | null;
}): JSX.Element {
  const { providersOrFallback: providers } = useProviderCatalog();
  const { rows } = useAccountRows();
  const { runTest, isTesting } = useRunProviderTest();
  const queryClient = useQueryClient();
  const { health, ensure, rpcFailed } = useProviderHealth();
  // Run readiness is provider-id based; ensure() is single-flight and cached, so asking on mount is safe.
  useEffect(() => {
    if (client) ensure();
  }, [client, ensure, providers]);
  const connectProviderId = useProvidersViewStore((s) => s.connectProviderId);
  const reloginAccountId = useProvidersViewStore((s) => s.reloginAccountId);
  const clearConnect = useProvidersViewStore((s) => s.clearConnect);
  const clearRelogin = useProvidersViewStore((s) => s.clearRelogin);
  const requestRelogin = useProvidersViewStore((s) => s.requestRelogin);
  const diagnosticText = useMemo(
    () =>
      !item || item.kind === "other"
        ? null
        : buildProviderDiagnostic({
            provider: providers.find((p) => p.id === item.id) ?? null,
            label: item.label,
            navKind: item.kind,
            rows: rows.filter((r) => item.accountIds.includes(r.account.id)),
          }),
    [item, providers, rows],
  );

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
  const info = item.kind === "other" ? null : providers.find((p) => p.id === item.id) ?? null;
  // The connect panel never renders for the other bucket or cli items; the
  // three ways it can open are mutually exclusive (the store flags win over
  // the local + Connect toggle).
  const canConnect = item.kind !== "other" && item.kind !== "cli" && item.accountProvider !== undefined;
  const connectOpen = canConnect && connectProviderId === item.id;
  const reloginAccount = canConnect ? itemRows.find((r) => r.account.id === reloginAccountId)?.account ?? null : null;
  const proxyConsumer = item.accountProvider !== undefined && providerConsumer(item.accountProvider) === "proxy";
  // Runs on these providers use the CLI's own login, not an smind account.
  const usesCliLogin = info !== null && (info.kind === "cli" || info.extends !== undefined || proxyConsumer);
  const onConnected = () => void queryClient.invalidateQueries({ queryKey: queryKeys.accountList() });

  return (
    <div data-testid="provider-detail" className="flex flex-col gap-3 p-4">
      <div className="flex flex-col gap-1">
        <div className="flex items-center gap-2">
          {item.id === "other" ? null : <ProviderIcon provider={item.id} />}
          <h3 className="text-ui-lg font-medium">{item.label}</h3>
        </div>
        <p className="text-ui-sm text-muted-foreground">
          {kindLine(item, info, providers)}
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
        {info ? (
          <ReadinessLine
            id={item.id}
            result={health[item.id]}
            failed={rpcFailed.has(item.id)}
            usesCliLogin={usesCliLogin}
          />
        ) : null}
      </div>

      {/* The body is keyed by provider: per-provider UI state (models search,
          diagnostics open, the accounts card's action) resets when the
          selection changes; the panels have their own identity keys. */}
      <Fragment key={item.id}>
      {connectOpen ? (
        <ConnectAccountPanel
          key={`connect-${item.id}`}
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
          key={`relogin-${reloginAccount.id}`}
          client={client}
          providers={providers}
          relogin={reloginAccount}
          autoStart
          onClose={clearRelogin}
          onConnected={onConnected}
        />
      ) : null}

      {info && !info.custom ? <ProviderDefaultModel info={info} hasClient={client !== null} /> : null}

      {info && !info.custom ? <ProviderEnvField info={info} hasClient={client !== null} /> : null}

      {info?.custom ? <ProviderCustomInfo info={info} hasClient={client !== null} /> : null}

      {item.kind === "cli" ? (
        <div
          className="flex items-center justify-between gap-2 rounded-lg px-2 py-1.5"
          data-testid={`provider-external-${item.id}`}
        >
          <span className="flex flex-col gap-0.5">
            <span className="text-ui-base text-muted-foreground">Managed externally via CLI</span>
            <span className="text-ui-xs text-muted-foreground">
              {info?.extends
                ? "Uses the CLI login kept in its own config directory — accounts added here aren't read by it."
                : info?.custom
                  ? "The agent handles its own sign-in — accounts added here aren't read by it."
                  : "Authenticates itself via its own CLI login — accounts added here aren't read by it."}
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
            {proxyConsumer ? "No proxy account" : "Not connected"}
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

      {info ? <ProviderModels provider={info} /> : null}
      {diagnosticText ? <ProviderDiagnostics text={diagnosticText} /> : null}
      </Fragment>
    </div>
  );
}
