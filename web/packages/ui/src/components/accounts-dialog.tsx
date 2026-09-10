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
import type { Account, ProviderListResult } from "@/lib/types";
import type { WsClient } from "@/lib/ws-client";

/** Accounts settings dialog: list (account.list) plus a minimal add form (account.add). No edit/remove this pass. */
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
  const [providers, setProviders] = useState<ProviderListResult["providers"]>([]);
  const [provider, setProvider] = useState<string>("");
  const [label, setLabel] = useState("");
  const [credential, setCredential] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);

  async function refresh() {
    if (!client) return;
    try {
      const [list, provs] = await Promise.all([
        client.call<Account[]>("account.list").then((r) => r ?? []),
        client.call<ProviderListResult>("provider.list"),
      ]);
      setAccounts(list);
      setProviders(provs.providers);
      // Default the select to the first provider rather than forcing a
      // pick when there's nothing meaningful to choose between yet.
      setProvider((prev) => prev || provs.providers[0]?.id || "");
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
                    {providers.map((p) => (
                      <SelectItem key={p.id} value={p.id}>
                        {p.label ?? p.id}
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
