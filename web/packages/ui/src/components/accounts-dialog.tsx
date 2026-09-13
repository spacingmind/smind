import { ChevronDown, ChevronRight } from "lucide-react";
import { useEffect, useState } from "react";

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
import type { Account, ProviderInfo, ProviderListResult, ProviderTestResult } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";

/**
 * Account-credential provider IDs (internal/accounts/refresh_providers.go),
 * the vocabulary internal/server/proxy.go actually matches accounts
 * against -- distinct from taskrunner.SupportedProviders()'s
 * claude-native/glm/kimi/codex-native task-execution IDs, which
 * provider.list serves and which this dialog used to (wrongly) source its
 * manual-add dropdown from. An account added under a task-execution
 * provider ID silently never matches proxy.go's routing lookup, so this
 * dialog must never offer those IDs here.
 *
 * provider.list is still consulted below, but only for its `kind: "cli"`
 * entries (see externalProviders state) -- a purely display-only
 * "managed externally" row, never fed into this dropdown.
 */
const MANUAL_PROVIDERS: { id: string; label: string }[] = [
  { id: "anthropic", label: "Anthropic (Claude)" },
  { id: "openai", label: "OpenAI (Codex)" },
  { id: "kimi", label: "Kimi" },
  { id: "xai", label: "xAI (Grok)" },
  { id: "antigravity", label: "Antigravity (Gemini)" },
];

/** Providers with a real browser-based OAuth login flow wired up (account.oauthStart) -- everyone else stays on the manual-paste form below. */
const OAUTH_PROVIDERS: { id: string; label: string }[] = [
  { id: "anthropic", label: "Anthropic (Claude)" },
  { id: "openai", label: "OpenAI (Codex)" },
];

/** Friendly display label for a provider id, falling back to the raw id for anything not in MANUAL_PROVIDERS. */
function providerLabel(id: string): string {
  return MANUAL_PROVIDERS.find((p) => p.id === id)?.label ?? id;
}

/** A small connection-status dot: green once provider.test reports ok, red once it reports not-ok, neutral (gray) until tested at all -- deepseek-harness's credential-configured-dot pattern. */
function StatusDot({ result }: { result: ProviderTestResult | undefined }) {
  const state = result === undefined ? "unknown" : result.ok ? "ok" : "failed";
  const color =
    state === "ok" ? "bg-emerald-500" : state === "failed" ? "bg-destructive" : "bg-muted-foreground/40";
  const label = state === "ok" ? "Connection ok" : state === "failed" ? "Connection failed" : "Not tested yet";
  return (
    <span
      className={`size-2 shrink-0 rounded-full ${color}`}
      title={label}
      aria-label={label}
      data-testid="accounts-status-dot"
      data-status={state}
    />
  );
}

/** Accounts settings dialog: list (account.list), a "Connect" OAuth login per known provider (account.oauthStart), and a manual-paste form (account.add) for everything else. No edit/remove this pass. */
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
  // Providers whose auth is managed entirely outside the account-credential
  // system (kind === "cli", e.g. GLM's spawned `npx` subprocess) -- shown as
  // a read-only "managed externally" row, never merged into MANUAL_PROVIDERS/
  // OAUTH_PROVIDERS since those are a different ID vocabulary (see the doc
  // comment above) that this dialog's add/connect flows actually route on.
  const [externalProviders, setExternalProviders] = useState<ProviderInfo[]>([]);
  const [provider, setProvider] = useState<string>(MANUAL_PROVIDERS[0].id);
  const [label, setLabel] = useState("");
  const [credential, setCredential] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  const [oauthLabel, setOauthLabel] = useState("");
  const [connecting, setConnecting] = useState<string | null>(null);
  const [authorizeUrl, setAuthorizeUrl] = useState<string | null>(null);
  const [oauthError, setOauthError] = useState<string | null>(null);

  // Manual paste is the secondary path (only path for kimi/xai/antigravity,
  // a fallback for anthropic/openai) -- collapsed by default so the primary
  // Connect flow isn't competing with a full form for attention.
  const [showManual, setShowManual] = useState(false);

  // provider.test results/pending-state per row, keyed by provider id --
  // account rows and "managed externally" rows both key on the same
  // provider id space provider.test accepts, so a single map covers both.
  // Undefined means "never tested" (neutral dot); present but pending means
  // a request is in flight for that row.
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

  // Separately fetch provider.list for the "managed externally" row(s) --
  // kept out of `refresh()` since it's a different RPC feeding a purely
  // display-only section, not the account list. Non-fatal on failure (falls
  // back to showing none) since it's not the primary content of this dialog.
  useEffect(() => {
    if (!open || !client) return;
    let cancelled = false;
    client
      .call<ProviderListResult>("provider.list")
      .then((result) => {
        if (cancelled) return;
        setExternalProviders(result.providers.filter((p) => p.kind === "cli"));
      })
      .catch((err) => console.error("provider.list failed, hiding externally-managed providers", err));
    return () => {
      cancelled = true;
    };
  }, [open, client]);

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
      });
      setLabel("");
      setCredential("");
      await refresh();
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
    try {
      await client!.callStream("account.oauthStart", { provider: providerId, label: oauthLabel.trim() }, (event, params) => {
        if (event !== "authorizeUrl") return;
        const url = (params as { url?: string } | undefined)?.url;
        if (url) setAuthorizeUrl(url);
      });
      setOauthLabel("");
      setAuthorizeUrl(null);
      await refresh();
    } catch (err) {
      setOauthError(err instanceof Error ? err.message : String(err));
    } finally {
      setConnecting(null);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Accounts</DialogTitle>
          <DialogDescription>
            Provider accounts used for routing. No edit or removal yet.
          </DialogDescription>
        </DialogHeader>
        {error && <p className="text-sm text-destructive">{error}</p>}
        <div className="max-h-40 overflow-y-auto">
          {accounts === null ? (
            <p className="text-sm text-muted-foreground">Loading…</p>
          ) : accounts.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              No accounts yet — connect one below.
            </p>
          ) : (
            <ul className="grid gap-1 text-sm">
              {accounts.map((a) => (
                <li key={a.id} className="flex flex-col gap-1 rounded-md px-2 py-1 hover:bg-accent" data-testid={`accounts-row-${a.provider}`}>
                  <div className="flex items-center justify-between gap-2">
                    <span className="flex min-w-0 items-center gap-1.5">
                      <StatusDot result={testResults[a.provider]} />
                      <span className="min-w-0 truncate font-medium">{a.label}</span>
                    </span>
                    <span className="flex shrink-0 items-center gap-2">
                      <span className="rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">
                        {providerLabel(a.provider)} · {a.credentialType}
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
                      className={`pl-3.5 text-xs ${testResults[a.provider].ok ? "text-emerald-600" : "text-destructive"}`}
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
            <p className="text-sm font-medium">Managed externally</p>
            <ul className="grid gap-1 text-sm">
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
                      <span className="rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">
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
                      className={`pl-3.5 text-xs ${testResults[p.id].ok ? "text-emerald-600" : "text-destructive"}`}
                      data-testid={`accounts-test-result-${p.id}`}
                    >
                      {testResults[p.id].detail}
                    </p>
                  )}
                </li>
              ))}
            </ul>
            <p className="text-xs text-muted-foreground">
              These run as a CLI subprocess and handle their own login — nothing to
              connect or paste here.
            </p>
          </div>
        )}

        <div className="grid gap-3 border-t pt-4">
          <p className="text-sm font-medium">Connect an account</p>
          <div className="grid gap-1">
            <label htmlFor="account-oauth-label" className="text-sm font-medium">
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
            {OAUTH_PROVIDERS.map((p) => (
              <Button
                key={p.id}
                variant="outline"
                disabled={connecting !== null}
                data-testid={`accounts-connect-${p.id}`}
                onClick={() => void connect(p.id)}
              >
                {connecting === p.id ? "Connecting…" : `Connect ${p.label}`}
              </Button>
            ))}
          </div>
          <p className="text-xs text-muted-foreground">
            Adding a second account for a provider you're already signed into in this
            browser may just reconnect the same one — sign out first, or use a private
            window, to pick a different account.
          </p>
          {oauthError && (
            <p className="rounded-md border border-destructive/30 bg-destructive/10 px-2.5 py-1.5 text-sm text-destructive">
              {oauthError}
            </p>
          )}
          {connecting && (
            <p className="rounded-md border bg-muted/50 px-2.5 py-1.5 text-sm text-muted-foreground">
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
          )}
        </div>

        <div className="border-t pt-4">
          <button
            type="button"
            aria-expanded={showManual}
            data-testid="accounts-manual-toggle"
            onClick={() => setShowManual((v) => !v)}
            className="flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
          >
            {showManual ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5" />}
            Paste a credential instead
          </button>
          <p className="mt-1 pl-4.5 text-xs text-muted-foreground">
            For Kimi, xAI, and Antigravity (no Connect flow yet), or to paste a
            credential obtained elsewhere.
          </p>

          {showManual && (
            <div className="mt-3 grid gap-3">
              <div className="grid gap-2">
                <div className="grid grid-cols-2 gap-2">
                  <div className="grid gap-1">
                    <label htmlFor="account-provider" className="text-sm font-medium">
                      Provider
                    </label>
                    <Select value={provider} onValueChange={setProvider}>
                      <SelectTrigger id="account-provider" className="w-full">
                        <SelectValue placeholder="Select provider" />
                      </SelectTrigger>
                      <SelectContent>
                        {MANUAL_PROVIDERS.map((p) => (
                          <SelectItem key={p.id} value={p.id}>
                            {p.label}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="grid gap-1">
                    <label htmlFor="account-label" className="text-sm font-medium">
                      Label
                    </label>
                    <Input
                      id="account-label"
                      value={label}
                      onChange={(e) => setLabel(e.target.value)}
                    />
                  </div>
                </div>
                <label htmlFor="account-credential" className="text-sm font-medium">
                  Credential
                </label>
                <textarea
                  id="account-credential"
                  className="h-24 w-full resize-none rounded-lg border border-input bg-transparent px-2.5 py-1 font-mono text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50"
                  placeholder="Paste the credential JSON (or API key) — same blob the CLI reads on stdin."
                  value={credential}
                  onChange={(e) => setCredential(e.target.value)}
                />
              </div>
              <div className="flex justify-end">
                <Button onClick={add} disabled={pending} data-testid="accounts-add-submit">
                  {pending ? "Adding…" : "Add account"}
                </Button>
              </div>
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
