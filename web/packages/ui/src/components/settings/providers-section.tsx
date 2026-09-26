import { useEffect, useState } from "react";
import { Info, MoreHorizontal, Plus } from "lucide-react";

import { ConnectAccountPanel, useProviderTest } from "@/components/accounts-dialog";
import {
  registerSettingsSection,
  type SettingsSectionContext,
} from "@/components/settings/settings-registry";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import { StatusDot } from "@/components/ui/status-dot";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";
import type {
  Account,
  ProviderInfo,
  ProviderListResult,
  ProviderTestResult,
  RunSummary,
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
 * and render their "managed externally" row regardless of any accounts
 * that happen to share their id -- account.add has no enum validation, so
 * a "glm"-labeled account is possible but has no ProviderInfo.accountProvider
 * to match against and falls through to "Other accounts" like any other
 * unmapped provider (xai, antigravity today -- see accounts-dialog.tsx's
 * known-gap note), rather than being silently hidden. providerConsumer
 * distinguishes, for the groups that do exist, whether the mapped
 * provider is actually read by smind's /v1 proxy today (anthropic,
 * openai) or by nothing at all (kimi is mapped but not yet wired to
 * either the proxy or a task runner) -- see its doc comment.
 *
 * Row actions (ADR-0015): ⋯ offers Rename (inline edit), Update
 * credential (a small inline form reusing account.add's credential
 * input shape), and Remove (a confirm dialog whose warning is computed
 * client-side per the plan's Decision 4 -- run.list's running runs plus
 * provider.list's accountProvider bridge, since runs carry a runner
 * provider, never an account id). account.updated/account.removed
 * events keep the list live without a manual refresh.
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

/**
 * The Remove-confirmation warning (ADR-0015's accepted condition 1, the
 * plan's Decision 4): how many running runs sit on the account's runtime
 * provider, and whether a sibling account exists to fail over to. Runs
 * carry a runner provider -- never an account id -- so the count is
 * provider-level by construction, which is exactly what the accepted
 * warning copy states. Exported for the warning tests.
 */
export function removeWarning(
  account: Account,
  providers: ProviderInfo[],
  accounts: Account[],
  runs: RunSummary[],
): { runningCount: number; lastAccount: boolean; providerLabel: string } {
  const label =
    providers.find((p) => p.accountProvider === account.provider)?.label ?? account.provider;
  const siblings = accounts.filter((a) => a.provider === account.provider).length;
  const runnerIds = providers
    .filter((p) => p.accountProvider === account.provider)
    .map((p) => p.id);
  const runningCount = runs.filter(
    (r) => r.Status === "running" && runnerIds.includes(r.Provider),
  ).length;
  return { runningCount, lastAccount: siblings <= 1, providerLabel: label };
}

function authTypeLabel(credentialType: string): string {
  return credentialType === "oauth" ? "OAuth" : "API key";
}

export type ProviderConsumer = "proxy" | "none";

/**
 * Which real consumer, if any, reads an internal/accounts provider kind's
 * credential today. internal/server/proxy.go hardcodes exactly two routed
 * account.provider strings (its providerAnthropic/providerOpenAI
 * constants -- the only ones POST /v1/messages and /v1/chat/completions
 * serve, per proxy.serve's exact-match filter). No internal/taskrunner
 * backend reads an account credential at all -- acp.GLMCommand/KimiCommand
 * spawn bare subprocesses with no credential injection, and Claude
 * Code/Codex's native backends use their own out-of-band CLI logins -- so
 * nothing is runner-consumed. Anything outside the proxy-routed set (kimi,
 * xai, antigravity, or any other account.add-able string, including a
 * "glm"-labeled account, which the backend's own ProviderInfo never maps
 * to a credential row at all) has no consumer.
 */
const PROXY_ROUTED_PROVIDERS = new Set(["anthropic", "openai"]);

export function providerConsumer(accountProvider: string): ProviderConsumer {
  return PROXY_ROUTED_PROVIDERS.has(accountProvider) ? "proxy" : "none";
}

type RowAction = { kind: "rename" | "credential" | "remove"; account: Account };

function ProvidersSection({ client, events }: SettingsSectionContext) {
  const [accounts, setAccounts] = useState<Account[] | null>(null);
  const [providers, setProviders] = useState<ProviderInfo[]>([]);
  const [error, setError] = useState<string | null>(null);
  // The connect/login + manual-paste flows live in the shared
  // ConnectAccountPanel (accounts-dialog.tsx) -- the same flows the old
  // dialog hosted -- shown behind the header's "Connect account"
  // disclosure so the grouped list stays the surface's primary content.
  const [connecting, setConnecting] = useState(false);
  const [action, setAction] = useState<RowAction | null>(null);
  const { testResults, testing, testProvider } = useProviderTest(client);

  async function refreshAccounts() {
    if (!client) return;
    try {
      const list = (await client.call<Account[]>("account.list")) ?? [];
      setAccounts(list);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }

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

  // Live updates (ADR-0009 shape): a rename/credential swap from a second
  // tab or the CLI arrives as account.updated, a removal as
  // account.removed -- no manual refresh needed. The acting tab applies
  // mutations through its own optimistic upsert too (see each action's
  // handler), so a null events surface never blocks the flow.
  useEffect(() => {
    if (!events) return;
    const offUpdated = events.subscribe("account.updated", (payload) => {
      const a = (payload as { account?: Account } | undefined)?.account;
      if (!a) return;
      setAccounts((prev) => {
        if (!prev) return prev;
        const index = prev.findIndex((existing) => existing.id === a.id);
        if (index === -1) return [...prev, a];
        const next = prev.slice();
        next[index] = a;
        return next;
      });
    });
    const offRemoved = events.subscribe("account.removed", (payload) => {
      const id = (payload as { id?: number } | undefined)?.id;
      if (id === undefined) return;
      setAccounts((prev) => (prev ? prev.filter((a) => a.id !== id) : prev));
    });
    return () => {
      offUpdated();
      offRemoved();
    };
  }, [events]);

  /** Applies a mutation's result immediately (the event may lag or events be null) -- upsert-by-id, mirroring profiles-section's handler. */
  function upsertAccount(a: Account) {
    setAccounts((prev) => {
      if (!prev) return [a];
      const index = prev.findIndex((existing) => existing.id === a.id);
      if (index === -1) return [...prev, a];
      const next = prev.slice();
      next[index] = a;
      return next;
    });
  }

  function dropAccount(id: number) {
    setAccounts((prev) => (prev ? prev.filter((a) => a.id !== id) : prev));
  }

  const { groups, other } = groupAccountsByProvider(providers, accounts ?? []);

  return (
    <div className="flex flex-col gap-4" data-testid="settings-section-providers">
      <div className="flex items-center justify-between gap-2">
        <h3 className="text-ui-base font-medium text-foreground">Providers</h3>
        <Button
          variant="outline"
          size="sm"
          aria-expanded={connecting}
          data-testid="providers-connect-toggle"
          onClick={() => setConnecting((v) => !v)}
        >
          <Plus aria-hidden className="size-3.5" />
          Connect account
        </Button>
      </div>
      {connecting && (
        <ConnectAccountPanel
          client={client}
          providers={providers}
          onConnected={() => void refreshAccounts()}
        />
      )}
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
              {info.accountProvider && providerConsumer(info.accountProvider) === "proxy" && (
                <p
                  className="text-ui-xs text-muted-foreground"
                  data-testid={`provider-group-note-${info.id}`}
                >
                  Used by smind's /v1 proxy, not by a task runner
                </p>
              )}
              {info.kind === "cli" ? (
                // A cli-kind provider runs as a subprocess and handles its
                // own login -- no credential row can exist for it, so its
                // group is always this single explanatory row (the same
                // row the old Accounts dialog rendered).
                <div
                  className="flex items-center justify-between gap-2 rounded-lg px-2 py-1.5"
                  data-testid={`provider-external-${info.id}`}
                >
                  <span className="flex flex-col gap-0.5">
                    <span className="text-ui-base text-muted-foreground">
                      Managed externally via CLI
                    </span>
                    <span className="text-ui-xs text-muted-foreground">
                      Authenticates itself via its own CLI login — accounts added here aren't
                      read by it.
                    </span>
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
                  <Button
                    variant="ghost"
                    size="xs"
                    data-testid={`provider-connect-${info.id}`}
                    onClick={() => setConnecting(true)}
                  >
                    Connect
                  </Button>
                </div>
              ) : (
                <ul className="flex flex-col gap-1">
                  {groupAccounts.map((account) => (
                    <AccountRow
                      key={account.id}
                      account={account}
                      testResult={testResults[account.provider]}
                      testing={testing[account.provider]}
                      onTest={testProvider}
                      action={action?.account.id === account.id ? action : null}
                      onAction={setAction}
                      onUpsert={upsertAccount}
                      client={client}
                    />
                  ))}
                </ul>
              )}
            </section>
          ))}

          {other.length > 0 && (
            <section className="flex flex-col gap-1 border-t pt-3" data-testid="provider-other-group">
              <h4 className="flex items-center gap-1 text-ui-sm font-medium text-foreground-subtle">
                Other accounts
                <TooltipProvider>
                  <Tooltip>
                    <TooltipTrigger asChild>
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-xs"
                        aria-label="About other accounts"
                        className="text-foreground-subtle"
                      >
                        <Info aria-hidden className="size-3.5" />
                      </Button>
                    </TooltipTrigger>
                    <TooltipContent>
                      These accounts' providers have no smind consumer — no agent runner and no proxy endpoint uses them today.
                    </TooltipContent>
                  </Tooltip>
                </TooltipProvider>
              </h4>
              <ul className="flex flex-col gap-1">
                {other.map((account) => (
                  <AccountRow
                    key={account.id}
                    account={account}
                    testResult={testResults[account.provider]}
                    testing={testing[account.provider]}
                    onTest={testProvider}
                    action={action?.account.id === account.id ? action : null}
                    onAction={setAction}
                    onUpsert={upsertAccount}
                    client={client}
                  />
                ))}
              </ul>
            </section>
          )}
        </div>
      )}
      {action?.kind === "remove" && (
        <RemoveAccountDialog
          client={client}
          account={action.account}
          providers={providers}
          accounts={accounts ?? []}
          onRemoved={() => {
            dropAccount(action.account.id);
            setAction(null);
          }}
          onOpenChange={(open) => !open && setAction(null)}
        />
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

function AccountRow({
  account,
  testResult,
  testing,
  onTest,
  action,
  onAction,
  onUpsert,
  client,
}: {
  account: Account;
  testResult: ProviderTestResult | undefined;
  testing: boolean | undefined;
  onTest: (providerId: string) => void;
  action: RowAction | null;
  onAction: (action: RowAction | null) => void;
  onUpsert: (account: Account) => void;
  client: SettingsSectionContext["client"];
}) {
  const state = testResult === undefined ? "unknown" : testResult.ok ? "ok" : "failed";
  return (
    <li
      className="flex flex-col gap-1 rounded-lg px-2 py-1.5 hover:bg-hover"
      data-testid={`provider-row-${account.id}`}
    >
      {action?.kind === "rename" ? (
        <RenameForm
          account={account}
          client={client}
          onSaved={(updated) => {
            onUpsert(updated);
            onAction(null);
          }}
          onCancel={() => onAction(null)}
        />
      ) : (
        <>
          <div className="flex items-center justify-between gap-2">
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
              {testResult && (
                <span
                  className={`max-w-48 truncate text-ui-sm ${
                    testResult.ok ? "text-success" : "text-destructive"
                  }`}
                  data-testid={`provider-row-test-result-${account.id}`}
                >
                  {testResult.detail}
                </span>
              )}
              <TestButton providerId={account.provider} testing={testing} onTest={onTest} />
              <DropdownMenu>
                <DropdownMenuTrigger asChild>
                  <Button
                    variant="ghost"
                    size="icon-sm"
                    aria-label={`Account actions for ${account.label}`}
                    data-testid={`provider-row-menu-${account.id}`}
                  >
                    <MoreHorizontal />
                  </Button>
                </DropdownMenuTrigger>
                <DropdownMenuContent align="end">
                  <DropdownMenuItem
                    data-testid={`provider-row-rename-${account.id}`}
                    onSelect={() => onAction({ kind: "rename", account })}
                  >
                    Rename
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    data-testid={`provider-row-update-credential-${account.id}`}
                    onSelect={() => onAction({ kind: "credential", account })}
                  >
                    Update credential
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    className="text-destructive focus:bg-menu-hover"
                    data-testid={`provider-row-remove-${account.id}`}
                    onSelect={() => onAction({ kind: "remove", account })}
                  >
                    Remove
                  </DropdownMenuItem>
                </DropdownMenuContent>
              </DropdownMenu>
            </span>
          </div>
          {action?.kind === "credential" && (
            <UpdateCredentialForm
              account={account}
              client={client}
              onSaved={(updated) => {
                onUpsert(updated);
                onAction(null);
              }}
              onCancel={() => onAction(null)}
            />
          )}
        </>
      )}
    </li>
  );
}

/** Inline label edit (sketch §4's "edit opens inline in the row"): one input + Save/Cancel, saving via account.rename. */
function RenameForm({
  account,
  client,
  onSaved,
  onCancel,
}: {
  account: Account;
  client: SettingsSectionContext["client"];
  onSaved: (updated: Account) => void;
  onCancel: () => void;
}) {
  const [label, setLabel] = useState(account.label);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function save() {
    if (!client || !label.trim()) return;
    setPending(true);
    setError(null);
    try {
      const updated = await client.call<Account>("account.rename", { id: account.id, label: label.trim() });
      onSaved(updated);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="flex flex-col gap-1" data-testid={`provider-rename-form-${account.id}`}>
      <div className="flex items-center gap-2">
        <Input
          autoFocus
          aria-label="Account label"
          value={label}
          onChange={(e) => setLabel(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") void save();
            if (e.key === "Escape") onCancel();
          }}
          data-testid={`provider-rename-input-${account.id}`}
          className="h-6"
        />
        <Button
          size="xs"
          disabled={pending || !label.trim()}
          data-testid={`provider-rename-save-${account.id}`}
          onClick={() => void save()}
        >
          {pending ? "Saving…" : "Save"}
        </Button>
        <Button
          variant="ghost"
          size="xs"
          disabled={pending}
          data-testid={`provider-rename-cancel-${account.id}`}
          onClick={onCancel}
        >
          Cancel
        </Button>
      </div>
      {error && <p className="text-ui-sm text-destructive">{error}</p>}
    </div>
  );
}

/**
 * The credential-swap form: the same credential textarea (JSON blob or
 * bare API key) and optional base_url account.add's manual form uses --
 * pasted into a row's ⋯ menu instead of a new-account dialog. Sent
 * verbatim via account.updateCredential; the daemon parses it exactly
 * like account.add (ADR-0015), and the response never contains the
 * credential back.
 */
function UpdateCredentialForm({
  account,
  client,
  onSaved,
  onCancel,
}: {
  account: Account;
  client: SettingsSectionContext["client"];
  onSaved: (updated: Account) => void;
  onCancel: () => void;
}) {
  const [credential, setCredential] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function save() {
    if (!client || !credential.trim()) return;
    setPending(true);
    setError(null);
    try {
      const updated = await client.call<Account>("account.updateCredential", {
        id: account.id,
        credential: credential.trim(),
        ...(baseUrl.trim() ? { baseUrl: baseUrl.trim() } : {}),
      });
      onSaved(updated);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="flex flex-col gap-1.5 pl-3.5" data-testid={`provider-credential-form-${account.id}`}>
      <textarea
        aria-label="New credential"
        placeholder="Paste the new credential JSON (or API key) — replaces the current one"
        value={credential}
        onChange={(e) => setCredential(e.target.value)}
        className="h-16 w-full resize-none rounded-lg border border-input-border bg-input px-2.5 py-1 font-mono text-ui-base outline-none focus-visible:border-input-border-focused focus-visible:bg-input-focused"
        data-testid={`provider-credential-input-${account.id}`}
      />
      <div className="flex items-center gap-2">
        <Input
          aria-label="Base URL (optional)"
          placeholder="Base URL (optional, api-key only)"
          value={baseUrl}
          onChange={(e) => setBaseUrl(e.target.value)}
          className="h-6"
          data-testid={`provider-credential-base-url-${account.id}`}
        />
        <Button
          size="xs"
          disabled={pending || !credential.trim()}
          data-testid={`provider-credential-save-${account.id}`}
          onClick={() => void save()}
        >
          {pending ? "Saving…" : "Save"}
        </Button>
        <Button
          variant="ghost"
          size="xs"
          disabled={pending}
          data-testid={`provider-credential-cancel-${account.id}`}
          onClick={onCancel}
        >
          Cancel
        </Button>
      </div>
      {error && <p className="text-ui-sm text-destructive">{error}</p>}
    </div>
  );
}

/**
 * The Remove confirmation (ADR-0015's accepted condition 1): not a
 * generic confirm -- it states what happens to runs currently on the
 * account's runtime provider, computed per the plan's Decision 4 from
 * run.list (fetched when the dialog opens; provider.list/account.list
 * are already in hand).
 */
function RemoveAccountDialog({
  client,
  account,
  providers,
  accounts,
  onRemoved,
  onOpenChange,
}: {
  client: SettingsSectionContext["client"];
  account: Account;
  providers: ProviderInfo[];
  accounts: Account[];
  onRemoved: () => void;
  onOpenChange: (open: boolean) => void;
}) {
  const [runs, setRuns] = useState<RunSummary[] | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!client) return;
    let cancelled = false;
    client
      .call<RunSummary[]>("run.list")
      .then((list) => {
        if (!cancelled) setRuns(list ?? []);
      })
      .catch((err) => {
        if (!cancelled) setRuns([]);
        console.error("run.list failed, showing the remove warning without a run count", err);
      });
    return () => {
      cancelled = true;
    };
  }, [client]);

  const warning =
    runs === null ? null : removeWarning(account, providers, accounts, runs);

  return (
    <Dialog open onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Remove account?</DialogTitle>
          <DialogDescription asChild>
            <div className="flex flex-col gap-1">
              <span>
                Removes <span className="font-medium text-foreground">{account.label}</span> and
                its routing affinity, quota history, and workspace links.
              </span>
              {warning === null ? (
                <span>Checking running tasks…</span>
              ) : warning.runningCount > 0 ? (
                warning.lastAccount ? (
                  <span className="text-warning" data-testid={`provider-remove-warning-${account.id}`}>
                    {warning.runningCount} running {warning.runningCount === 1 ? "task" : "tasks"} will
                    fail: this is the last {warning.providerLabel} account
                  </span>
                ) : (
                  <span data-testid={`provider-remove-warning-${account.id}`}>
                    {warning.runningCount} running {warning.runningCount === 1 ? "task" : "tasks"} will
                    switch to another account
                  </span>
                )
              ) : (
                <span>No running tasks use this account.</span>
              )}
            </div>
          </DialogDescription>
        </DialogHeader>
        <FormError message={error} />
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={pending}>
            Cancel
          </Button>
          <Button
            variant="destructive"
            disabled={pending}
            data-testid={`provider-remove-confirm-${account.id}`}
            onClick={async () => {
              if (!client) return;
              setPending(true);
              setError(null);
              try {
                await client.call("account.remove", { id: account.id });
                onRemoved();
              } catch (err) {
                setError(err instanceof Error ? err.message : String(err));
                setPending(false);
              }
            }}
          >
            {pending ? "Removing…" : "Remove"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function FormError({ message }: { message: string | null }) {
  if (!message) return null;
  return <p className="text-ui-base text-destructive">{message}</p>;
}

// Nests under "Agents & providers" (run-config IA plan) alongside Agents
// (order 175) -- order 180 keeps it immediately after Agents, matching
// the plan's "sub-items: Agents, Providers" sequence. This section used
// to be a top-level nav entry on develop (before that plan's nav regroup
// existed); joining the group is this merge's own resolution, not part
// of ADR-0015 itself.
registerSettingsSection({
  id: "providers",
  label: "Providers",
  groupLabel: "Agents & providers",
  order: 180,
  render: (ctx) => <ProvidersSection {...ctx} />,
});
