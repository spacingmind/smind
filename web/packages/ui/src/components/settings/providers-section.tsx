import { useEffect, useState } from "react";

import {
  registerSettingsSection,
  type SettingsSectionContext,
} from "@/components/settings/settings-registry";
import { Button } from "@/components/ui/button";
import { StatusDot } from "@/components/ui/status-dot";
import type {
  Account,
  ProviderInfo,
  ProviderListResult,
  ProviderTestResult,
} from "@/lib/types";

/**
 * Settings -> Providers (docs/plans/active/providers-settings.md, sketch
 * §5): the standalone Accounts dialog's content as a Settings section,
 * with accounts grouped by runtime provider (Claude Code / GLM / Codex /
 * Kimi) rather than a flat list.
 *
 * Grouping uses provider.list's accountProvider mapping -- the one
 * existing account<->runner bridge (internal/taskrunner.ProviderInfo):
 * an Account row's `provider` is an internal/accounts vocabulary string
 * (anthropic/openai/kimi/...), and the runtime group it belongs to is
 * whichever ProviderInfo has accountProvider === that string. Providers
 * with no matching account render a "not connected" row instead of an
 * empty group; kind:"cli" providers (GLM) have no credential rows at all
 * and render their "managed externally" row; accounts whose provider no
 * ProviderInfo maps (xai, antigravity today -- see accounts-dialog.tsx's
 * known-gap note) fall through to the "Other accounts" group rather than
 * being silently hidden.
 *
 * This first pass renders rows and the provider.test diagnostic only;
 * the ⋯ actions (rename / update credential / remove, ADR-0015) and the
 * Connect flows land in the plan's next steps.
 */

/** One runtime-provider group: the ProviderInfo row plus the accounts mapped to it (empty => the group renders its not-connected row). */
export interface ProviderGroup {
  info: ProviderInfo;
  accounts: Account[];
}

/**
 * Partitions accounts into per-runtime-provider groups (in provider.list's
 * own display order) plus the "other" leftovers no runtime provider maps.
 * Exported for the grouping tests; the component itself is thin over it.
 */
export function groupAccountsByProvider(
  providers: ProviderInfo[],
  accounts: Account[],
): { groups: ProviderGroup[]; other: Account[] } {
  const groups = providers.map((info) => ({
    info,
    accounts: info.accountProvider
      ? accounts.filter((a) => a.provider === info.accountProvider)
      : [],
  }));
  const mapped = new Set(
    providers.filter((p) => p.accountProvider).map((p) => p.accountProvider),
  );
  return { groups, other: accounts.filter((a) => !mapped.has(a.provider)) };
}

function authTypeLabel(credentialType: string): string {
  return credentialType === "oauth" ? "OAuth" : "API key";
}

function ProvidersSection({ client }: SettingsSectionContext) {
  const [accounts, setAccounts] = useState<Account[] | null>(null);
  const [providers, setProviders] = useState<ProviderInfo[]>([]);
  const [error, setError] = useState<string | null>(null);
  // provider.test results/pending per provider id -- the same map shape
  // accounts-dialog.tsx uses: undefined = never tested (neutral dot).
  const [testResults, setTestResults] = useState<Record<string, ProviderTestResult>>({});
  const [testing, setTesting] = useState<Record<string, boolean>>({});

  useEffect(() => {
    if (!client) return;
    let cancelled = false;
    client
      .call<Account[]>("account.list")
      .then((list) => {
        if (!cancelled) setAccounts(list ?? []);
      })
      .catch((err) => setError(err instanceof Error ? err.message : String(err)));
    return () => {
      cancelled = true;
    };
  }, [client]);

  useEffect(() => {
    if (!client) return;
    let cancelled = false;
    client
      .call<ProviderListResult>("provider.list")
      .then((result) => {
        if (!cancelled && result.providers.length > 0) setProviders(result.providers);
      })
      .catch((err) => console.error("provider.list failed, hiding provider groups", err));
    return () => {
      cancelled = true;
    };
  }, [client]);

  async function testProvider(providerId: string) {
    if (!client) return;
    setTesting((t) => ({ ...t, [providerId]: true }));
    try {
      const result = await client.call<ProviderTestResult>("provider.test", { provider: providerId });
      setTestResults((r) => ({ ...r, [providerId]: result }));
    } catch (err) {
      setTestResults((r) => ({
        ...r,
        [providerId]: { ok: false, detail: err instanceof Error ? err.message : String(err) },
      }));
    } finally {
      setTesting((t) => ({ ...t, [providerId]: false }));
    }
  }

  const { groups, other } = groupAccountsByProvider(providers, accounts ?? []);

  return (
    <div className="flex flex-col gap-4" data-testid="settings-section-providers">
      <h3 className="text-ui-base font-medium text-foreground">Providers</h3>
      {error && (
        <p role="alert" className="text-ui-base text-destructive">
          {error}
        </p>
      )}
      {accounts === null ? (
        <p className="text-ui-base text-muted-foreground">Loading…</p>
      ) : (
        <div className="flex flex-col gap-3 rounded-xl border bg-card p-3">
          {groups.map(({ info, accounts: groupAccounts }) => (
            <section
              key={info.id}
              className="flex flex-col gap-1"
              data-testid={`provider-group-${info.id}`}
            >
              <h4 className="text-ui-sm font-medium text-foreground-subtle">
                {(info.label ?? info.id).toUpperCase()}
              </h4>
              {info.kind === "cli" ? (
                // A cli-kind provider runs as a subprocess and handles its
                // own login -- no credential row can exist for it, so its
                // group is always this single explanatory row (the same
                // row the old Accounts dialog rendered).
                <div
                  className="flex items-center justify-between gap-2 rounded-lg px-2 py-1.5"
                  data-testid={`provider-external-${info.id}`}
                >
                  <span className="text-ui-base text-muted-foreground">
                    Managed externally via CLI
                  </span>
                  <TestButton
                    providerId={info.id}
                    testing={testing[info.id]}
                    onTest={testProvider}
                  />
                </div>
              ) : groupAccounts.length === 0 ? (
                <div
                  className="flex items-center justify-between gap-2 rounded-lg px-2 py-1.5"
                  data-testid={`provider-not-connected-${info.id}`}
                >
                  <span className="flex items-center gap-1.5 text-ui-base text-muted-foreground">
                    <StatusDot status="neutral" className="size-2" />
                    Not connected
                  </span>
                </div>
              ) : (
                <ul className="flex flex-col gap-1">
                  {groupAccounts.map((account) => {
                    const result = testResults[account.provider];
                    const state = result === undefined ? "unknown" : result.ok ? "ok" : "failed";
                    return (
                      <li
                        key={account.id}
                        className="flex items-center justify-between gap-2 rounded-lg px-2 py-1.5 hover:bg-hover"
                        data-testid={`provider-row-${account.id}`}
                      >
                        <span className="flex min-w-0 items-center gap-1.5">
                          <StatusDot
                            status={state === "ok" ? "success" : state === "failed" ? "danger" : "neutral"}
                            className="size-2"
                            title={
                              state === "ok"
                                ? "Connection ok"
                                : state === "failed"
                                  ? "Connection failed"
                                  : "Not tested yet"
                            }
                            data-testid={`provider-row-dot-${account.id}`}
                            data-status={state}
                          />
                          <span className="min-w-0 truncate text-ui-base font-medium text-foreground">
                            {account.label}
                          </span>
                          <span className="rounded-full bg-muted px-2 py-0.5 text-ui-xs text-muted-foreground">
                            {authTypeLabel(account.credentialType)}
                          </span>
                        </span>
                        <span className="flex shrink-0 items-center gap-2">
                          {result && (
                            <span
                              className={`max-w-48 truncate text-ui-sm ${
                                result.ok ? "text-success" : "text-destructive"
                              }`}
                              data-testid={`provider-row-test-result-${account.id}`}
                            >
                              {result.detail}
                            </span>
                          )}
                          <TestButton
                            providerId={account.provider}
                            testing={testing[account.provider]}
                            onTest={testProvider}
                          />
                        </span>
                      </li>
                    );
                  })}
                </ul>
              )}
            </section>
          ))}

          {other.length > 0 && (
            <section className="flex flex-col gap-1 border-t pt-3" data-testid="provider-other-group">
              <h4 className="flex items-center gap-1 text-ui-sm font-medium text-foreground-subtle">
                Other accounts
                <span
                  className="cursor-help text-ui-sm text-muted-foreground"
                  title="These accounts' providers have no agent runner mapped yet — they are kept for routing but no agent uses them today."
                >
                  ⓘ
                </span>
              </h4>
              <ul className="flex flex-col gap-1">
                {other.map((account) => (
                  <li
                    key={account.id}
                    className="flex items-center justify-between gap-2 rounded-lg px-2 py-1.5 hover:bg-hover"
                    data-testid={`provider-other-row-${account.id}`}
                  >
                    <span className="flex min-w-0 items-center gap-1.5">
                      <span className="min-w-0 truncate text-ui-base font-medium text-foreground">
                        {account.label}
                      </span>
                      <span className="rounded-full bg-muted px-2 py-0.5 text-ui-xs text-muted-foreground">
                        {account.provider} · {authTypeLabel(account.credentialType)}
                      </span>
                    </span>
                    <TestButton
                      providerId={account.provider}
                      testing={testing[account.provider]}
                      onTest={testProvider}
                    />
                  </li>
                ))}
              </ul>
            </section>
          )}
        </div>
      )}
    </div>
  );
}

function TestButton({
  providerId,
  testing,
  onTest,
}: {
  providerId: string;
  testing: boolean | undefined;
  onTest: (providerId: string) => void;
}) {
  return (
    <Button
      type="button"
      variant="outline"
      size="xs"
      disabled={testing}
      data-testid={`provider-test-${providerId}`}
      onClick={() => void onTest(providerId)}
    >
      {testing ? "Testing…" : "Test"}
    </Button>
  );
}

registerSettingsSection({
  id: "providers",
  label: "Providers",
  order: 165,
  render: (ctx) => <ProvidersSection {...ctx} />,
});
