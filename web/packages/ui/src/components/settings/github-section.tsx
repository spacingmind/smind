import { useEffect, useId, useState, type FormEvent } from "react";

import { describePrError, type PrErrorView } from "@/components/git/pr-error";
import { SettingsSectionTitle } from "@/components/settings/settings-title";
import { registerSettingsSection } from "@/components/settings/settings-registry";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { ConfirmDialog } from "@/components/ui/confirm-dialog";
import { Input } from "@/components/ui/input";
import { LoadingRows } from "@/components/ui/loading-rows";
import { useConfirmAction } from "@/hooks/use-confirm-action";
import { useForgeLogout, useForgeSetToken, useForgeStatus } from "@/hooks/use-forge-status";
import { FORGE_SETTINGS_SECTION_ID } from "@/components/git/forge-settings-link";
import { Icons } from "@/lib/icons";
import type { ForgeHostStatus } from "@/lib/types";

const INSECURE_SENTENCE =
  "This connection isn't encrypted, so smind won't send a token over it. Use the relay, an ssh tunnel or TLS.";

/** Mirrors the daemon's hostname rule so an obvious typo never leaves the form. */
const HOST_PATTERN = /^[A-Za-z0-9]([A-Za-z0-9.-]*[A-Za-z0-9])?(:\d{1,5})?$/;

const SOURCE_WORD: Record<ForgeHostStatus["source"], string> = {
  keyring: "system keychain",
  file: "gh config file",
  env: "environment variable",
  none: "not stored",
};

/** Whether the app talks to a daemon on this machine, where `gh auth login` in a terminal is an option. */
function isLocalDaemon(): boolean {
  const host = window.location.hostname;
  return host === "localhost" || host === "127.0.0.1" || host === "[::1]" || host === "::1";
}

function HostRow({ host, onLogout }: { host: ForgeHostStatus; onLogout: (host: string) => void }) {
  return (
    <li className="flex items-center gap-3 px-3 py-2" data-testid={`github-host-${host.host}`}>
      <Icons.success aria-hidden className="size-4 shrink-0 text-success" />
      <div className="min-w-0 flex-1">
        <p className="truncate text-ui-base text-foreground">
          Signed in to <span className="font-medium">{host.host}</span> as <span className="font-medium">{host.login}</span>
        </p>
        <p className="truncate text-ui-sm text-muted-foreground">
          {host.scopes.length > 0 ? `Scopes: ${host.scopes.join(", ")}` : "Scopes: not reported"} · {host.protocol || "https"} ·{" "}
          {SOURCE_WORD[host.source]}
        </p>
      </div>
      {/* A token from the environment can't be removed by gh auth logout, so there is nothing to confirm. */}
      {host.source !== "env" && (
        <Button type="button" variant="outline" size="sm" data-testid={`github-logout-${host.host}`} onClick={() => onLogout(host.host)}>
          Log out
        </Button>
      )}
    </li>
  );
}

/**
 * Settings -> GitHub (ADR-0039 §8): the daemon host's `gh` sign-in. The pasted token is held by this component's
 * input state only: never in a store, the query cache, a URL or a log, cleared on submit (whatever the outcome)
 * and on unmount (it is local state, so unmounting drops it).
 */
function GithubSection() {
  const status = useForgeStatus();
  const setToken = useForgeSetToken();
  const logout = useForgeLogout();
  const [token, setTokenText] = useState("");
  const [host, setHost] = useState("");
  const [error, setError] = useState<PrErrorView | null>(null);
  const [signedIn, setSignedIn] = useState(false);
  const [loggingOut, setLoggingOut] = useState<string | null>(null);
  const logoutAction = useConfirmAction(loggingOut !== null);
  const tokenId = useId();
  const hostId = useId();
  const noteId = useId();

  // Belt and braces: an unmount empties the field even if a parent keeps this component's state alive.
  useEffect(() => () => setTokenText(""), []);

  const data = status.data;
  const installed = data?.cli.installed ?? false;
  const secure = data?.canSetToken ?? false;
  const fieldDisabled = !data || !installed || !secure || setToken.isPending;
  const hostInvalid = host.trim() !== "" && !HOST_PATTERN.test(host.trim());
  const signedInHosts = data?.hosts.filter((h) => h.authenticated) ?? [];

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    const value = token.trim();
    if (fieldDisabled || value === "" || hostInvalid) return;
    // Out of the field before the request is even sent; the call holds the only copy.
    setTokenText("");
    setError(null);
    setSignedIn(false);
    try {
      await setToken.submit(value, host.trim() || undefined);
      setSignedIn(true);
      setHost("");
    } catch (err) {
      setError(describePrError(err, "token"));
    }
  }

  const target = loggingOut;
  return (
    <div className="flex flex-col gap-4" data-testid="settings-section-github">
      <SettingsSectionTitle
        title="GitHub"
        description="Pull requests, issues and pushes use the GitHub CLI (gh) on the machine that runs the daemon."
      />

      {status.isPending ? (
        <LoadingRows rows={2} />
      ) : status.isError || !data ? (
        <Alert variant="error" testId="github-status-error" description="Couldn't read the GitHub status from the daemon. It may be too old to support GitHub sign-in." />
      ) : (
        <>
          {!installed ? (
            <Alert
              variant="warning"
              testId="github-cli-missing"
              description="The GitHub CLI (`gh`) isn't installed on the daemon host. Install it there, then reopen this page."
            />
          ) : signedInHosts.length > 0 ? (
            <ul className="divide-y divide-border rounded-xl border bg-card" data-testid="github-hosts">
              {signedInHosts.map((h) => (
                <HostRow key={h.host} host={h} onLogout={setLoggingOut} />
              ))}
            </ul>
          ) : (
            <div className="rounded-xl border bg-card px-3 py-2" data-testid="github-signed-out">
              <p className="text-ui-base text-foreground">Not signed in</p>
              <p className="text-ui-sm text-muted-foreground">Paste a GitHub token below to sign the daemon host in.</p>
            </div>
          )}
          {installed && data.cli.version && (
            <p className="text-ui-sm text-muted-foreground" data-testid="github-cli-version">
              gh {data.cli.version} on the daemon host
            </p>
          )}

          {installed && !secure && (
            <Alert variant="warning" testId="github-insecure" description={INSECURE_SENTENCE} />
          )}

          <form className="flex flex-col gap-3" onSubmit={(e) => void handleSubmit(e)} data-testid="github-token-form" autoComplete="off">
            <div className="grid gap-1">
              <label htmlFor={tokenId} className="text-ui-sm text-foreground-muted">
                Personal access token
              </label>
              <Input
                id={tokenId}
                type="password"
                name="smind-forge-credential"
                autoComplete="off"
                autoCapitalize="off"
                autoCorrect="off"
                spellCheck={false}
                data-1p-ignore
                data-lpignore="true"
                maxLength={255}
                value={token}
                disabled={fieldDisabled}
                aria-describedby={noteId}
                placeholder={secure ? "Paste a token" : "Unavailable on this connection"}
                data-testid="github-token-input"
                onChange={(e) => setTokenText(e.target.value)}
              />
              <p id={noteId} className="text-ui-sm text-muted-foreground">
                smind sends it once, only on a secure connection, to <code className="font-mono">gh auth login</code>; it isn&apos;t stored by the app.
                A classic token needs the <code className="font-mono">repo</code> and <code className="font-mono">read:org</code> scopes.
              </p>
            </div>
            <div className="grid gap-1">
              <label htmlFor={hostId} className="text-ui-sm text-foreground-muted">
                Host (optional)
              </label>
              <Input
                id={hostId}
                value={host}
                autoComplete="off"
                spellCheck={false}
                disabled={fieldDisabled}
                aria-invalid={hostInvalid || undefined}
                placeholder="the workspace's origin host, e.g. github.com"
                data-testid="github-host-input"
                onChange={(e) => setHost(e.target.value)}
              />
            </div>
            <div className="flex items-center gap-3">
              <Button type="submit" size="sm" disabled={fieldDisabled || token.trim() === "" || hostInvalid} data-testid="github-token-submit">
                {setToken.isPending ? "Signing in…" : "Sign in"}
              </Button>
              {signedIn && !error && (
                <p role="status" className="flex items-center gap-1 text-ui-sm text-success" data-testid="github-signed-in-note">
                  <Icons.check aria-hidden className="size-3.5" />
                  Signed in to GitHub.
                </p>
              )}
            </div>
            {error && (
              <p role="alert" className="text-ui-base text-destructive" data-testid="github-token-error">
                {error.message}
              </p>
            )}
          </form>

          {isLocalDaemon() && installed && (
            <p className="text-ui-sm text-muted-foreground" data-testid="github-local-hint">
              Or run <code className="font-mono">gh auth login</code> on this machine.
            </p>
          )}
        </>
      )}

      <ConfirmDialog
        open={loggingOut !== null}
        onOpenChange={(open) => {
          if (open) return;
          setLoggingOut(null);
          logoutAction.reset();
        }}
        title="Log out of GitHub?"
        description={
          <>
            Remove the daemon host&apos;s GitHub sign-in for <span className="font-medium text-foreground">{target}</span>. Pull requests and pushes over https stop working until you sign in again.
          </>
        }
        confirmLabel="Log out"
        pendingLabel="Logging out…"
        pending={logoutAction.pending}
        error={logoutAction.error}
        testId="github-logout-dialog"
        onConfirm={() => {
          if (target === null) return;
          void logoutAction.run(() => logout.mutateAsync({ host: target })).then((ok) => {
            if (ok) {
              setLoggingOut(null);
              setSignedIn(false);
            }
          });
        }}
      />
    </div>
  );
}

// A top-level entry right after Shortcuts (120) and before the Connection group (150).
registerSettingsSection({
  id: FORGE_SETTINGS_SECTION_ID,
  label: "GitHub",
  order: 130,
  icon: <Icons.forge aria-hidden className="size-3.5" />,
  render: () => <GithubSection />,
});
