import { ChevronDown, ChevronRight } from "lucide-react";
import { useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { StatusDot as SharedStatusDot } from "@/components/ui/status-dot";
import type { Account, ProviderInfo, ProviderListResult, ProviderTestResult } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";

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
  // proxy endpoints) -- collapsed behind its own disclosure, nested inside
  // the manual form, so it doesn't compete with the credential field for
  // attention in the common case.
  const [showBaseUrl, setShowBaseUrl] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  const [oauthLabel, setOauthLabel] = useState("");
  const [connecting, setConnecting] = useState<string | null>(null);
  const [authorizeUrl, setAuthorizeUrl] = useState<string | null>(null);
  const [oauthError, setOauthError] = useState<string | null>(null);
  // The in-flight account.oauthStart call's own AbortController (ui-redesign-
  // parity Item 14: "start -> show URL -> poll -> cancel" rather than a
  // fire-and-forget button). WsClient.callStream already treats `signal` as
  // "send task.cancel for this request's own id and keep waiting for its
  // terminal response" (lib/ws-client.ts's CallOptions doc comment) -- no
  // daemon change needed, cancellation of an arbitrary in-flight request by
  // id already exists at the wire layer (internal/wsapi/conn.go's inflight
  // map is keyed by request id, not by method).
  const connectAbortRef = useRef<AbortController | null>(null);

  // Manual paste is the secondary path (only path for api-key-only
  // providers, a fallback for oauth ones) -- collapsed by default so the
  // primary Connect flow isn't competing with a full form for attention.
  const [showManual, setShowManual] = useState(false);

  const manualProviders = credentialProviders(providers);
  const oauthProviders = manualProviders.filter((p) => p.credentialKind === "oauth");
  const isAPIKeyProvider =
    manualProviders.find((p) => p.accountProvider === provider)?.credentialKind === "api-key";

  // Keep the manual-add dropdown's selection valid as provider.list loads
  // in (it starts empty until the caller's fetch resolves): default to the
  // first credential-bearing provider, and only reset it if the current
  // selection stops being one of the options.
  useEffect(() => {
    setProvider((current) => {
      const options = credentialProviders(providers);
      if (options.length === 0) return current;
      if (options.some((p) => p.accountProvider === current)) return current;
      return options[0].accountProvider ?? current;
    });
  }, [providers]);

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
      setLabel("");
      setCredential("");
      setBaseUrl("");
      onConnected();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setPending(false);
    }
  }

  async function connect(providerId: string) {
    if (!oauthLabel.trim()) {
      setOauthError("Label is required.");
      return;
    }
    setOauthError(null);
    setAuthorizeUrl(null);
    setConnecting(providerId);
    const controller = new AbortController();
    connectAbortRef.current = controller;
    try {
      await client!.callStream(
        "account.oauthStart",
        { provider: providerId, label: oauthLabel.trim() },
        (event, params) => {
          if (event !== "authorizeUrl") return;
          const url = (params as { url?: string } | undefined)?.url;
          if (url) setAuthorizeUrl(url);
        },
        { signal: controller.signal },
      );
      setOauthLabel("");
      setAuthorizeUrl(null);
      onConnected();
    } catch (err) {
      // A user-initiated cancel already reflects itself in the UI by
      // clearing `connecting` below -- surfacing the resulting "cancelled"
      // error on top of that would read as a failure the user didn't cause.
      if (!controller.signal.aborted) {
        setOauthError(err instanceof Error ? err.message : String(err));
      }
    } finally {
      setConnecting(null);
      connectAbortRef.current = null;
    }
  }

  /** Cancels the in-flight OAuth login -- the "cancel" step of the start/show-URL/poll/cancel state machine. */
  function cancelConnect() {
    connectAbortRef.current?.abort();
  }

  return (
    <>
        <div className="grid gap-3 border-t pt-4">
          <p className="text-ui-base font-medium">Connect an account</p>
          <div className="grid gap-1">
            <label htmlFor="account-oauth-label" className="text-ui-base font-medium">
              Label
            </label>
            <Input
              id="account-oauth-label"
              value={oauthLabel}
              onChange={(e) => setOauthLabel(e.target.value)}
              placeholder="e.g. work, personal — however you'll tell accounts apart"
            />
          </div>
          <div className="flex flex-wrap gap-2">
            {oauthProviders.map((p) => (
              <Button
                key={p.accountProvider}
                variant="outline"
                disabled={connecting !== null}
                data-testid={`accounts-connect-${p.accountProvider}`}
                onClick={() => void connect(p.accountProvider!)}
              >
                {connecting === p.accountProvider ? "Connecting…" : `Connect ${p.label ?? p.accountProvider}`}
              </Button>
            ))}
          </div>
          <p className="text-ui-sm text-muted-foreground">
            Adding a second account for a provider you're already signed into in this
            browser may just reconnect the same one — sign out first, or use a private
            window, to pick a different account.
          </p>
          {oauthError && (
            <p className="rounded-md border border-destructive/30 bg-destructive/10 px-2.5 py-1.5 text-ui-base text-destructive">
              {oauthError}
            </p>
          )}
          {connecting && (
            <div className="flex items-center justify-between gap-2 rounded-md border bg-muted/50 px-2.5 py-1.5 text-ui-base text-muted-foreground">
              <p>
                {authorizeUrl ? (
                  <>
                    Waiting for login — if a browser didn't open automatically,{" "}
                    <a href={authorizeUrl} target="_blank" rel="noreferrer" className="underline">
                      open the login page
                    </a>
                    .
                  </>
                ) : (
                  "Starting login…"
                )}
              </p>
              <Button
                type="button"
                variant="outline"
                size="xs"
                className="shrink-0"
                data-testid="accounts-connect-cancel"
                onClick={cancelConnect}
              >
                Cancel
              </Button>
            </div>
          )}
        </div>

        <div className="border-t pt-4">
          <button
            type="button"
            aria-expanded={showManual}
            data-testid="accounts-manual-toggle"
            onClick={() => setShowManual((v) => !v)}
            className="flex items-center gap-1 text-ui-base text-muted-foreground hover:text-foreground"
          >
            {showManual ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
            Paste a credential instead
          </button>
          <p className="mt-1 pl-4.5 text-ui-sm text-muted-foreground">
            For providers with no Connect flow yet, or to paste a credential
            obtained elsewhere.
          </p>

          {showManual && (
            <div className="mt-3 grid gap-3">
              {error && <p className="text-ui-base text-destructive">{error}</p>}
              <div className="grid gap-2">
                <div className="grid grid-cols-2 gap-2">
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
                  <div className="grid gap-1">
                    <label htmlFor="account-label" className="text-ui-base font-medium">
                      Label
                    </label>
                    <Input
                      id="account-label"
                      value={label}
                      onChange={(e) => setLabel(e.target.value)}
                    />
                  </div>
                </div>
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

              {isAPIKeyProvider && (
                <div>
                  <button
                    type="button"
                    aria-expanded={showBaseUrl}
                    data-testid="accounts-base-url-toggle"
                    onClick={() => setShowBaseUrl((v) => !v)}
                    className="flex items-center gap-1 text-ui-base text-muted-foreground hover:text-foreground"
                  >
                    {showBaseUrl ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
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
          )}
        </div>
    </>
  );
}

/**
 * useProviderTest runs the provider.test diagnostic on demand and tracks
 * per-provider results/pending state -- the shared "Test" flow behind both
 * the old Accounts dialog's rows and Settings -> Providers' rows. Returns
 * the call function plus the two maps (undefined result = never tested,
 * the neutral dot).
 */
export function useProviderTest(client: WsClient | null) {
  const [testResults, setTestResults] = useState<Record<string, ProviderTestResult>>({});
  const [testing, setTesting] = useState<Record<string, boolean>>({});

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

  return { testResults, testing, testProvider };
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
  const [accounts, setAccounts] = useState<Account[] | null>(null);
  // The daemon's full provider catalog (internal/taskrunner.SupportedProviders,
  // served by provider.list) -- every row this dialog renders (managed-
  // externally, Connect buttons, manual-add dropdown) derives from this,
  // rather than a hand-maintained frontend constant. See the doc comment
  // above for the accountProvider indirection and its known gap.
  const [providers, setProviders] = useState<ProviderInfo[]>([]);
  const [error, setError] = useState<string | null>(null);
  const { testResults, testing, testProvider } = useProviderTest(client);

  const externalProviders = providers.filter((p) => p.kind === "cli");

  async function refresh() {
    if (!client) return;
    try {
      const list = (await client.call<Account[]>("account.list")) ?? [];
      setAccounts(list);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  useEffect(() => {
    if (open) void refresh();
  }, [open, client]);

  // Separately fetch provider.list -- kept out of `refresh()` since it's a
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
          {accounts === null ? (
            <p className="text-ui-base text-muted-foreground">Loading…</p>
          ) : accounts.length === 0 ? (
            <p className="text-ui-base text-muted-foreground">
              No accounts yet — connect one below.
            </p>
          ) : (
            <ul className="grid gap-1 text-ui-base">
              {accounts.map((a) => (
                <li key={a.id} className="flex flex-col gap-1 rounded-md px-2 py-1 hover:bg-hover" data-testid={`accounts-row-${a.provider}`}>
                  <div className="flex items-center justify-between gap-2">
                    <span className="flex min-w-0 items-center gap-1.5">
                      <StatusDot result={testResults[a.provider]} />
                      <span className="min-w-0 truncate font-medium">{a.label}</span>
                    </span>
                    <span className="flex shrink-0 items-center gap-2">
                      <span className="rounded-full bg-muted px-2 py-0.5 text-ui-xs text-muted-foreground">
                        {providerLabel(providers, a.provider)} · {a.credentialType}
                      </span>
                      <Button
                        type="button"
                        variant="outline"
                        size="xs"
                        disabled={testing[a.provider]}
                        data-testid={`accounts-test-${a.provider}`}
                        onClick={() => void testProvider(a.provider)}
                      >
                        {testing[a.provider] ? "Testing…" : "Test"}
                      </Button>
                    </span>
                  </div>
                  {testResults[a.provider] && (
                    <p
                      className={`pl-3.5 text-ui-sm ${testResults[a.provider].ok ? "text-success" : "text-destructive"}`}
                      data-testid={`accounts-test-result-${a.provider}`}
                    >
                      {testResults[a.provider].detail}
                    </p>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>

        {externalProviders.length > 0 && (
          <div className="grid gap-1 border-t pt-4" data-testid="accounts-external-providers">
            <p className="text-ui-base font-medium">Managed externally</p>
            <ul className="grid gap-1 text-ui-base">
              {externalProviders.map((p) => (
                <li
                  key={p.id}
                  className="flex flex-col gap-1 rounded-md px-2 py-1"
                  data-testid={`accounts-external-provider-${p.id}`}
                >
                  <div className="flex items-center justify-between gap-2">
                    <span className="flex min-w-0 items-center gap-1.5">
                      <StatusDot result={testResults[p.id]} />
                      <span className="min-w-0 truncate font-medium">{p.label ?? p.id}</span>
                    </span>
                    <span className="flex shrink-0 items-center gap-2">
                      <span className="rounded-full bg-muted px-2 py-0.5 text-ui-xs text-muted-foreground">
                        Managed externally via CLI
                      </span>
                      <Button
                        type="button"
                        variant="outline"
                        size="xs"
                        disabled={testing[p.id]}
                        data-testid={`accounts-test-${p.id}`}
                        onClick={() => void testProvider(p.id)}
                      >
                        {testing[p.id] ? "Testing…" : "Test"}
                      </Button>
                    </span>
                  </div>
                  {testResults[p.id] && (
                    <p
                      className={`pl-3.5 text-ui-sm ${testResults[p.id].ok ? "text-success" : "text-destructive"}`}
                      data-testid={`accounts-test-result-${p.id}`}
                    >
                      {testResults[p.id].detail}
                    </p>
                  )}
                </li>
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
          onConnected={() => void refresh()}
        />
      </DialogContent>
    </Dialog>
  );
}
