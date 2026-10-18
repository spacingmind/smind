import { useEffect, useState } from "react";

import {
  useRemoveAccount,
  useRenameAccount,
  useUpdateAccountCredential,
} from "@/hooks/use-accounts";
import type { SettingsSectionContext } from "@/components/settings/settings-registry";
import { removeWarning } from "@/components/settings/providers-section";
import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import type { Account, ProviderInfo, RunSummary } from "@/lib/types";

export function authTypeLabel(credentialType: string): string {
  return credentialType === "oauth" ? "OAuth" : "API key";
}

export type RowAction = { kind: "rename" | "credential" | "remove"; account: Account };

export function TestButton({
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

/** Inline label edit (sketch §4's "edit opens inline in the row"): one input + Save/Cancel, saving via account.rename. */
export function RenameForm({
  account,
  onSaved,
  onCancel,
}: {
  account: Account;
  onSaved: () => void;
  onCancel: () => void;
}) {
  // The edit still targets the real account.label; the usage report's
  // displayName wins for display (AC58) but is not what this form changes.
  const rename = useRenameAccount();
  const [label, setLabel] = useState(account.label);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function save() {
    if (!label.trim()) return;
    setPending(true);
    setError(null);
    try {
      await rename.mutateAsync({ id: account.id, label: label.trim() });
      onSaved();
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
export function UpdateCredentialForm({
  account,
  onSaved,
  onCancel,
}: {
  account: Account;
  onSaved: () => void;
  onCancel: () => void;
}) {
  const updateCredential = useUpdateAccountCredential();
  const [credential, setCredential] = useState("");
  const [baseUrl, setBaseUrl] = useState("");
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function save() {
    if (!credential.trim()) return;
    setPending(true);
    setError(null);
    try {
      await updateCredential.mutateAsync({
        id: account.id,
        credential: credential.trim(),
        ...(baseUrl.trim() ? { baseUrl: baseUrl.trim() } : {}),
      });
      onSaved();
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
export function RemoveAccountDialog({
  client,
  account,
  providers,
  accounts,
  onRemoved,
  onOpenChange,
}: {
  client: SettingsSectionContext["client"];
  // eslint-disable-next-line
  account: Account;
  providers: ProviderInfo[];
  accounts: Account[];
  onRemoved: () => void;
  onOpenChange: (open: boolean) => void;
}) {
  const [runs, setRuns] = useState<RunSummary[] | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const remove = useRemoveAccount();

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
              setPending(true);
              setError(null);
              try {
                await remove.mutateAsync(account.id);
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

export function FormError({ message }: { message: string | null }) {
  if (!message) return null;
  return <p className="text-ui-base text-destructive">{message}</p>;
}
