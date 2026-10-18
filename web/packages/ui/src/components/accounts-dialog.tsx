import { Icons } from "@/lib/icons";
import { useEffect, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { useAccounts, useProviderTestResult, useRunProviderTest } from "@/hooks/use-accounts";
import { useAccountLogin } from "@/hooks/use-account-login";
import { useLoginRemote } from "@/hooks/use-login-remote";
import { DeviceCodePanel } from "@/components/connect/device-code-panel";
import { LoginDoneNotice, LoginErrorNotice } from "@/components/connect/login-notices";
import { PasteRedirectPanel } from "@/components/connect/paste-redirect-panel";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { SegmentedControl } from "@/components/ui/segmented-control";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { StatusDot as SharedStatusDot } from "@/components/ui/status-dot";
import { externalLinkProps } from "@/lib/external-link";
import { connectMethodsFor, defaultConnectMethod, type ConnectMethod } from "@/lib/login-methods";
import { isDesktop } from "@/lib/platform";
import type { Account, ProviderInfo, ProviderListResult, ProviderTestResult } from "@/lib/types";

import type { WsClient } from "@/lib/ws-client";
import { queryKeys } from "@/lib/query-keys";

/**
 * This dialog used to source its Connect buttons and manual-add dropdown
 * from hand-maintained MANUAL_PROVIDERS/OAUTH_PROVIDERS constants (a
 * frontend-only copy of internal/accounts' provider vocabulary) that GLM
 * showed no way to add for -- see Item 7b. Item 7d replaces those constants
 * entirely: every row below is derived from provider.list
 * (internal/taskrunner.SupportedProviders), the same RPC/registry
 * task-detail.tsx's provider dropdown already renders from.
 *
 * provider.list's ProviderInfo now carries, per provider,
 * CredentialKind ("oauth" | "api-key", absent for kind: "cli") and
 * accountProvider -- the id to actually call account.add/
 * account.oauthStart with. accountProvider is *not* the same string as
 * ProviderInfo.id: internal/accounts has its own provider vocabulary
 * (anthropic/openai/kimi/xai/antigravity) that predates and differs from
 * taskrunner.Provider (claude-native/glm/kimi/codex-native) -- proxy.go and
 * LoginCoordinator only ever match against the former, so every add/connect
 * call below must use accountProvider, never id.
 *
 * Known gap (see docs/plans/active/task-permission-ux.md's Item 7d note):
 * xai and antigravity are accounts-only providers with no taskrunner
 * counterpart at all, so provider.list has no entry for them and this
 * dialog can no longer offer to add one -- unifying that would mean
 * changing the accounts/credential data model itself, out of scope here.
 */

/** Providers with a credential row at all (kind is unset) -- feeds the manual-add dropdown. */
export function credentialProviders(providers: ProviderInfo[]): ProviderInfo[] {
  return providers.filter((p) => p.credentialKind);
}

/** Friendly display label for an accounts-vocabulary provider id (Account.provider), falling back to the raw id for anything provider.list didn't describe (see the gap noted above). */
export function providerLabel(providers: ProviderInfo[], id: string): string {
  return providers.find((p) => p.accountProvider === id)?.label ?? id;
}

/** A small connection-status dot: green once provider.test reports ok, red once it reports not-ok, neutral (gray) until tested at all -- deepseek-harness's credential-configured-dot pattern, now on the shared StatusDot primitive (ui-redesign-parity plan, Item 2). */
export function StatusDot({ result }: { result: ProviderTestResult | undefined }) {
  const state = result === undefined ? "unknown" : result.ok ? "ok" : "failed";
  const status = state === "ok" ? "success" : state === "failed" ? "danger" : "neutral";
  const label = state === "ok" ? "Connection ok" : state === "failed" ? "Connection failed" : "Not tested yet";
  return (
    <SharedStatusDot
      status={status}
      className="size-2"
      title={label}
      aria-label={label}
      data-testid="accounts-status-dot"
      data-status={state}
    />
  );
}

/**
 * ConnectAccountPanel is the shared connect/login + manual-paste flow
 * (providers-settings plan Item 3): the OAuth Connect buttons with their
 * start/show-URL/poll/cancel state machine (account.oauthStart), and the
 * collapsible "Paste a credential instead" manual form (account.add) with
 * its provider dropdown, optional base_url, and per-state validation.
 * Extracted verbatim from the Accounts dialog so Settings -> Providers
 * embeds the same flows without duplication; the dialog is now a thin
 * wrapper over this panel plus the account list.
 *
 * props.providers is provider.list's result (each row derives from it --
 * see the file's top doc comment for the accountProvider indirection).
 * onConnected fires after a successful connect/add so the host can
 * refresh its account list (or rely on the account.updated event).
 */
/** How long a local browser login waits before offering the paste-the-address fallback. */
const PASTE_HINT_DELAY_MS = 15000;

export function ConnectAccountPanel({
  client,
  providers,
  onConnected,
}: {
  client: WsClient | null;
  providers: ProviderInfo[];
  onConnected: () => void;
}) {
  const [provider, setProvider] = useState<string>("");
  const [label, setLabel] = useState("");
  const [credential, setCredential] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  // The base_url field is an advanced, api-key-only knob (self-hosted/local
  // proxy endpoints) -- collapsed behind its own disclosure so it doesn't
  // compete with the credential field for attention in the common case.
  const [showBaseUrl, setShowBaseUrl] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  const [method, setMethod] = useState<ConnectMethod>("browser");
  const [methodTouched, setMethodTouched] = useState(false);
  const [labelTouched, setLabelTouched] = useState(false);
  const [pasteHint, setPasteHint] = useState(false);

  const { remote, derivedRemote, setOverride } = useLoginRemote();
  const login = useAccountLogin({
    client,
    onConnected: () => onConnected(),
  });
  const accounts = useAccounts().data ?? [];

  const manualProviders = credentialProviders(providers);
  const selectedProvider = manualProviders.find((p) => p.accountProvider === provider);
  const methods = selectedProvider
    ? connectMethodsFor(selectedProvider)
    : [{ value: "api-key" as ConnectMethod, label: "API key" }];

  // Keep the provider selection valid as provider.list loads in (it starts
  // empty until the caller's fetch resolves): default to the first
  // credential-bearing provider, and only reset it if the current selection
  // stops being one of the options.
  useEffect(() => {
    setProvider((current) => {
      const options = credentialProviders(providers);
      if (options.length === 0) return current;
      if (options.some((p) => p.accountProvider === current)) return current;
      return options[0].accountProvider ?? current;
    });
  }, [providers]);

  // The default method follows the provider and whether the daemon is
  // remote (device code when signing in from another device); the user's
  // explicit choice survives a location flip but resets on provider change.
  useEffect(() => {
    setMethodTouched(false);
  }, [provider]);

  useEffect(() => {
    if (methodTouched) return;
    if (!selectedProvider) return;
    setMethod(defaultConnectMethod(selectedProvider, remote));
  }, [selectedProvider, remote, methodTouched]);

  // The default label (account-N) follows the provider until the user
  // edits the field; a finished login re-arms the prefill.
  const defaultLabel = `account-${accounts.filter((a) => a.provider === provider).length + 1}`;

  useEffect(() => {
    if (!labelTouched) setLabel(defaultLabel);
  }, [defaultLabel, labelTouched]);

  useEffect(() => {
    if (login.state.phase === "done") setLabelTouched(false);
  }, [login.state.phase]);

  // A local browser login offers the paste fallback only after it's had a
  // fair chance to finish on its own; remote/pasteOnly logins show it at
  // once (rendered below).
  useEffect(() => {
    setPasteHint(false);
    if (login.state.phase !== "waiting" || remote || login.state.pasteOnly) return;
    const timer = setTimeout(() => setPasteHint(true), PASTE_HINT_DELAY_MS);
    return () => clearTimeout(timer);
  }, [login.state.phase, login.state.phase === "waiting" ? login.state.loginId : null, remote]);

  async function add() {
    if (!provider || !label.trim() || !credential.trim()) {
      setError("Provider, label, and credential are all required.");
      return;
    }
    setPending(true);
    setError(null);
    try {
      await client!.call("account.add", {
        provider,
        label: label.trim(),
        credential: credential.trim(),
        ...(baseUrl.trim() ? { baseUrl: baseUrl.trim() } : {}),
      });
      setLabelTouched(false);
      setCredential("");
      setBaseUrl("");
      onConnected();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setPending(false);
    }
  }

  function startLogin() {
    if (!label.trim()) {
      setError("Label is required.");
      return;
    }
    setError(null);
    login.start({
      provider,
      label: label.trim(),
      method: method === "api-key" ? "browser" : method,
    });
  }

  const state = login.state;
  const showPastePanel =
    state.phase === "waiting" && (remote || state.pasteOnly || pasteHint);
  const connecting = state.phase === "starting" || state.phase === "waiting" || state.phase === "device";
  const selectedLabel = selectedProvider?.label ?? selectedProvider?.accountProvider ?? provider;

  return (
    <div className="grid gap-3 border-t pt-4">
      <p className="text-ui-base font-medium">Connect an account</p>
      <div className="grid gap-1">
        <label htmlFor="account-oauth-label" className="text-ui-base font-medium">
          Label
        </label>
        <Input
          id="account-oauth-label"
          value={label}
          onChange={(e) => {
            setLabelTouched(true);
            setLabel(e.target.value);
          }}
          placeholder="e.g. work, personal — however you'll tell accounts apart"
        />
      </div>
      {manualProviders.length > 1 && (
        <div className="grid gap-1">
          <label htmlFor="account-provider" className="text-ui-base font-medium">
            Provider
          </label>
          <Select value={provider} onValueChange={setProvider}>
            <SelectTrigger id="account-provider" className="w-full">
              <SelectValue placeholder="Select provider" />
            </SelectTrigger>
            <SelectContent>
              {manualProviders.map((p) => (
                <SelectItem key={p.accountProvider} value={p.accountProvider!}>
                  {p.label ?? p.accountProvider}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
      )}
      {methods.length > 1 && (
        <div className="grid gap-1">
          <span className="text-ui-base font-medium">Sign-in method</span>
          <SegmentedControl
            label="Sign-in method"
            options={methods}
            value={method}
            onChange={(v) => {
              setMethodTouched(true);
              setMethod(v);
            }}
          />
        </div>
      )}
      <div data-testid="login-location" className="text-ui-sm text-muted-foreground">
        {remote ? (
          <p className="grid gap-1">
            <span>
              This daemon is on another machine — finish by device code or by pasting the
              sign-in address.
            </span>
            <button
              type="button"
              data-testid="login-location-toggle"
              onClick={() => setOverride(derivedRemote ? "this-device" : null)}
              className="text-left underline hover:text-foreground"
            >
              I&rsquo;m on the daemon&rsquo;s machine
            </button>
          </p>
        ) : (
          <button
            type="button"
            data-testid="login-location-toggle"
            onClick={() => setOverride(derivedRemote ? null : "other-device")}
            className="underline hover:text-foreground"
          >
            Signing in from another device?
          </button>
        )}
      </div>
      {method === "api-key" ? (
        <div className="grid gap-3">
          {error && <p className="text-ui-base text-destructive">{error}</p>}
          <div className="grid gap-2">
            <label htmlFor="account-credential" className="text-ui-base font-medium">
              Credential
            </label>
            <textarea
              id="account-credential"
              className="h-24 w-full resize-none rounded-lg border border-input bg-transparent px-2.5 py-1 font-mono text-ui-base outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
              placeholder="Paste the credential JSON (or API key) — same blob the CLI reads on stdin."
              value={credential}
              onChange={(e) => setCredential(e.target.value)}
            />
          </div>
          {selectedProvider?.credentialKind === "api-key" && (
            <div>
              <button
                type="button"
                aria-expanded={showBaseUrl}
                data-testid="accounts-base-url-toggle"
                onClick={() => setShowBaseUrl((v) => !v)}
                className="flex items-center gap-1 text-ui-base text-muted-foreground hover:text-foreground"
              >
                {showBaseUrl ? <Icons.chevronDown className="size-3" /> : <Icons.chevronRight className="size-3" />}
                Base URL (optional)
              </button>
              {showBaseUrl && (
                <div className="mt-2 grid gap-1 pl-4.5">
                  <Input
                    id="account-base-url"
                    aria-label="Base URL (optional)"
                    placeholder="e.g. http://localhost:8080"
                    value={baseUrl}
                    onChange={(e) => setBaseUrl(e.target.value)}
                  />
                  <p className="text-ui-sm text-muted-foreground">
                    Override the upstream endpoint for this account — advanced, for
                    self-hosted or local proxy endpoints only.
                  </p>
                </div>
              )}
            </div>
          )}
          <div className="flex justify-end">
            <Button onClick={add} disabled={pending} data-testid="accounts-add-submit">
              {pending ? "Adding…" : "Add account"}
            </Button>
          </div>
        </div>
      ) : (
        <div className="grid gap-3">
          {error && (
            <p className="text-ui-base text-destructive">{error}</p>
          )}
          <div>
            <Button
              data-testid={`accounts-connect-${provider}`}
              disabled={connecting}
              onClick={startLogin}
            >
              {connecting ? "Connecting…" : `Connect ${selectedLabel}`}
            </Button>
          </div>
          {method === "browser" && (
            <p className="text-ui-sm text-muted-foreground">
              Adding a second account for a provider you&rsquo;re already signed into in this
              browser may just reconnect the same one — sign out first, or use a private
              window, to pick a different account.
            </p>
          )}
          {state.phase === "starting" && (
            <div className="flex items-center justify-between gap-2 rounded-md border bg-muted/50 px-2.5 py-1.5 text-ui-base text-muted-foreground">
              <p>Starting login…</p>
              <Button
                type="button"
                variant="outline"
                size="xs"
                className="shrink-0"
                data-testid="accounts-connect-cancel"
                onClick={login.cancel}
              >
                Cancel
              </Button>
            </div>
          )}
          {state.phase === "waiting" && (
            <div className="grid gap-2 rounded-md border bg-muted/50 px-2.5 py-1.5 text-ui-base text-muted-foreground">
              <div className="flex items-center justify-between gap-2">
                <p>
                  Waiting for login — {isDesktop ? "if your browser didn't open, " : ""}
                  <a {...externalLinkProps(state.url)} className="underline">
                    open the login page
                  </a>
                  .
                </p>
                <Button
                  type="button"
                  variant="outline"
                  size="xs"
                  className="shrink-0"
                  data-testid="accounts-connect-cancel"
                  onClick={login.cancel}
                >
                  Cancel
                </Button>
              </div>
              {showPastePanel ? (
                <PasteRedirectPanel
                  onSubmit={login.complete}
                  pending={login.pasting}
                  error={login.pasteError}
                  autoFocus={remote}
                />
              ) : (
                <div>
                  <button
                    type="button"
                    data-testid="login-trouble-toggle"
                    onClick={() => setPasteHint(true)}
                    className="underline hover:text-foreground"
                    hidden={!pasteHint}
                  >
                    Having trouble? Paste the address instead
                  </button>
                </div>
              )}
            </div>
          )}
          {state.phase === "device" && (
            <DeviceCodePanel
              userCode={state.userCode}
              verificationUrl={state.verificationUrl}
              expiresAt={state.expiresAt}
              onCancel={login.cancel}
            />
          )}
          {state.phase === "done" && (
            <LoginDoneNotice
              result={{
                label: state.result.label,
                created: state.result.created,
                adopted: state.result.adopted,
              }}
              onDismiss={login.reset}
            />
          )}
          {state.phase === "error" && (
            <LoginErrorNotice message={state.message} soft={state.soft} onDismiss={login.reset} />
          )}
        </div>
      )}
    </div>
  );
}

/**
 * One row's provider.test state, from the shared cache (ADR-0032): the
 * cached result plus the pending flag of the run-mutation, replacing the
 * per-dialog `useProviderTest` state.
 */
function useRowTest(providerKey: string) {
  const result = useProviderTestResult(providerKey);
  const { isTesting } = useRunProviderTest();
  return { result, testing: isTesting(providerKey) };
}

/**
 * Accounts settings dialog -- now a thin wrapper (providers-settings plan
 * Item 3): the account list and managed-externally rows plus the shared
 * ConnectAccountPanel, all extracted so Settings -> Providers embeds the
 * same flows. No entry point opens this dialog anymore; it stays for
 * direct callers and its tests until those migrate.
 */
export function AccountsDialog({
  client,
  open,
  onOpenChange,
}: {
  client: WsClient | null;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const queryClient = useQueryClient();
  const accountsQuery = useAccounts();
  const accounts = accountsQuery.data;
  // The daemon's full provider catalog (internal/taskrunner.SupportedProviders,
  // served by provider.list) -- every row this dialog renders (managed-
  // externally, Connect buttons, manual-add dropdown) derives from this,
  // rather than a hand-maintained frontend constant. See the doc comment
  // above for the accountProvider indirection and its known gap.
  const [providers, setProviders] = useState<ProviderInfo[]>([]);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (accountsQuery.error) {
      setError(accountsQuery.error instanceof Error ? accountsQuery.error.message : String(accountsQuery.error));
    }
  }, [accountsQuery.error]);

  const externalProviders = providers.filter((p) => p.kind === "cli");

  // Separately fetch provider.list -- kept out of the account list since it's a
  // different RPC feeding purely display/config-driven sections, not the
  // account list itself. Non-fatal on failure (falls back to showing none
  // of the derived sections) since it's not the primary content of this
  // dialog.
  useEffect(() => {
    if (!open || !client) return;
    let cancelled = false;
    client
      .call<ProviderListResult>("provider.list")
      .then((result) => {
        if (cancelled) return;
        setProviders(result.providers);
      })
      .catch((err) => console.error("provider.list failed, hiding provider-derived sections", err));
    return () => {
      cancelled = true;
    };
  }, [open, client]);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Accounts</DialogTitle>
          <DialogDescription>
            Provider accounts used for routing. No edit or removal yet.
          </DialogDescription>
        </DialogHeader>
        {error && <p className="text-ui-base text-destructive">{error}</p>}
        <div className="max-h-40 overflow-y-auto">
          {accounts === undefined ? (
            <p className="text-ui-base text-muted-foreground">Loading…</p>
          ) : accounts.length === 0 ? (
            <p className="text-ui-base text-muted-foreground">
              No accounts yet — connect one below.
            </p>
          ) : (
            <ul className="grid gap-1 text-ui-base">
              {accounts.map((a) => (
                <AccountListRow key={a.id} account={a} providers={providers} />
              ))}
            </ul>
          )}
        </div>

        {externalProviders.length > 0 && (
          <div className="grid gap-1 border-t pt-4" data-testid="accounts-external-providers">
            <p className="text-ui-base font-medium">Managed externally</p>
            <ul className="grid gap-1 text-ui-base">
              {externalProviders.map((p) => (
                <ExternalProviderRow key={p.id} provider={p} />
              ))}
            </ul>
            <p className="text-ui-sm text-muted-foreground">
              These run as a CLI subprocess and handle their own login — nothing to
              connect or paste here.
            </p>
          </div>
        )}

        <ConnectAccountPanel
          client={client}
          providers={providers}
          onConnected={() => void queryClient.invalidateQueries({ queryKey: queryKeys.accountList() })}
        />
      </DialogContent>
    </Dialog>
  );
}

/** One account row: the label (provider.list's name for its provider), a Test button and the cached test result. */
function AccountListRow({ account, providers }: { account: Account; providers: ProviderInfo[] }) {
  const { result, testing } = useRowTest(account.provider);
  return (
    <li key={account.id} className="flex flex-col gap-1 rounded-md px-2 py-1 hover:bg-hover" data-testid={`accounts-row-${account.provider}`}>
      <div className="flex items-center justify-between gap-2">
        <span className="flex min-w-0 items-center gap-1.5">
          <StatusDot result={result} />
          <span className="min-w-0 truncate font-medium">{account.label}</span>
        </span>
        <span className="flex shrink-0 items-center gap-2">
          <span className="rounded-full bg-muted px-2 py-0.5 text-ui-xs text-muted-foreground">
            {providerLabel(providers, account.provider)} · {account.credentialType}
          </span>
          <TestRowButton providerId={account.provider} testing={testing} />
        </span>
      </div>
      {result && (
        <p
          className={`pl-3.5 text-ui-sm ${result.ok ? "text-success" : "text-destructive"}`}
          data-testid={`accounts-test-result-${account.provider}`}
        >
          {result.detail}
        </p>
      )}
    </li>
  );
}

/** One cli-kind provider row: its cached test state and a Test button, like the account rows. */
function ExternalProviderRow({ provider }: { provider: ProviderInfo }) {
  const { result, testing } = useRowTest(provider.id);
  return (
    <li
      key={provider.id}
      className="flex flex-col gap-1 rounded-md px-2 py-1"
      data-testid={`accounts-external-provider-${provider.id}`}
    >
      <div className="flex items-center justify-between gap-2">
        <span className="flex min-w-0 items-center gap-1.5">
          <StatusDot result={result} />
          <span className="min-w-0 truncate font-medium">{provider.label ?? provider.id}</span>
        </span>
        <span className="flex shrink-0 items-center gap-2">
          <span className="rounded-full bg-muted px-2 py-0.5 text-ui-xs text-muted-foreground">
            Managed externally via CLI
          </span>
          <TestRowButton providerId={provider.id} testing={testing} />
        </span>
      </div>
      {result && (
        <p
          className={`pl-3.5 text-ui-sm ${result.ok ? "text-success" : "text-destructive"}`}
          data-testid={`accounts-test-result-${provider.id}`}
        >
          {result.detail}
        </p>
      )}
    </li>
  );
}

function TestRowButton({ providerId, testing }: { providerId: string; testing: boolean }) {
  const { runTest } = useRunProviderTest();
  return (
    <Button
      type="button"
      variant="outline"
      size="xs"
      disabled={testing}
      data-testid={`accounts-test-${providerId}`}
      onClick={() => runTest(providerId)}
    >
      {testing ? "Testing…" : "Test"}
    </Button>
  );
}
