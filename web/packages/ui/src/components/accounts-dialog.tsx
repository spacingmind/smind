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
import type { Account } from "@/lib/types";
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
  const [provider, setProvider] = useState<string>(MANUAL_PROVIDERS[0].id);
  const [label, setLabel] = useState("");
  const [credential, setCredential] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  const [oauthLabel, setOauthLabel] = useState("");
  const [connecting, setConnecting] = useState<string | null>(null);
  const [authorizeUrl, setAuthorizeUrl] = useState<string | null>(null);
  const [oauthError, setOauthError] = useState<string | null>(null);

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
              No accounts yet — add one below.
            </p>
          ) : (
            <ul className="grid gap-1 text-sm">
              {accounts.map((a) => (
                <li key={a.id} className="flex items-center justify-between gap-2 rounded-md px-2 py-1 hover:bg-accent">
                  <span className="font-medium">{a.label}</span>
                  <span className="text-muted-foreground">
                    {a.provider} · {a.credentialType}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </div>

        <div className="grid gap-3 border-t pt-4">
          <div className="grid gap-1">
            <label htmlFor="account-oauth-label" className="text-sm font-medium">
              Label
            </label>
            <Input
              id="account-oauth-label"
              value={oauthLabel}
              onChange={(e) => setOauthLabel(e.target.value)}
              placeholder="e.g. work"
            />
          </div>
          {oauthError && <p className="text-sm text-destructive">{oauthError}</p>}
          <div className="flex flex-wrap gap-2">
            {OAUTH_PROVIDERS.map((p) => (
              <Button
                key={p.id}
                variant="outline"
                disabled={connecting !== null}
                onClick={() => void connect(p.id)}
              >
                {connecting === p.id ? "Connecting…" : `Connect ${p.label}`}
              </Button>
            ))}
          </div>
          {connecting && (
            <p className="text-sm text-muted-foreground">
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

        <div className="grid gap-3 border-t pt-4">
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
            <Button onClick={add} disabled={pending}>
              {pending ? "Adding…" : "Add account"}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
