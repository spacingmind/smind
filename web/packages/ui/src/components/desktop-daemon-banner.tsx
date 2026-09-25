import { useCallback, useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { desktop, isDesktop, type Connection, type ConnectionVersionInfo, type DaemonProgress } from "@/lib/platform";

/**
 * ADR-0013 part D2's "daemon vX is older than this app (vY)" banner. Only
 * the local, app-managed daemon gets an actionable "Update & restart"
 * button -- a local-but-unmanaged daemon (the user's own `smind serve`)
 * and every remote/url/relay connection get the same text as a plain,
 * non-actionable notice instead (AC3's "never touch a daemon we don't
 * manage" rule applies here too, not just on the Rust side).
 *
 * `connectionVersion` (not `daemonStatus`) is what drives *this*
 * component even for the local connection, since it's the one primitive
 * that works for every connection kind (the loopback proxy doesn't
 * forward `/healthz`, so a non-local connection's version can only be
 * read this way); `daemonStatus` is asked separately, only when the
 * current connection is local, purely to learn whether it's managed.
 */
export function DesktopDaemonBanner() {
  const [current, setCurrent] = useState<Connection | null>(null);
  const [info, setInfo] = useState<ConnectionVersionInfo | null>(null);
  const [managed, setManaged] = useState(false);
  const [busy, setBusy] = useState(false);
  const [stageMessage, setStageMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(() => {
    if (!isDesktop) return;
    desktop
      .getCurrentConnection()
      .then(async (c) => {
        setCurrent(c);
        const versionInfo = await desktop.connectionVersion(c.id);
        setInfo(versionInfo);
        if (c.kind === "local") {
          const status = await desktop.daemonStatus();
          setManaged(status.managedState === "managed");
        } else {
          setManaged(false);
        }
      })
      .catch(() => {
        // Best-effort: a version-skew notice is cosmetic, never load-bearing.
      });
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  useEffect(() => {
    if (!isDesktop) return;
    return desktop.onDaemonProgress((p: DaemonProgress) => setStageMessage(p.message));
  }, []);

  async function handleUpdate() {
    setError(null);
    setBusy(true);
    setStageMessage(null);
    try {
      await desktop.daemonUpdate();
      refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
      setStageMessage(null);
    }
  }

  if (!isDesktop || !current || !info || info.comparison !== "older") {
    return null;
  }

  const actionable = current.kind === "local" && managed;

  return (
    <div
      data-testid="desktop-daemon-banner"
      role="status"
      className="flex items-center justify-between gap-3 border-b border-warning/40 bg-warning/10 px-4 py-2 text-ui-base text-foreground"
    >
      <span>
        Daemon v{info.daemonVersion} is older than this app (v{info.appVersion})
        {busy && stageMessage ? ` — ${stageMessage}` : null}
      </span>
      <div className="flex items-center gap-2">
        {error && (
          <span role="alert" data-testid="desktop-daemon-banner-error" className="text-destructive">
            {error}
          </span>
        )}
        {actionable && (
          <Button size="sm" data-testid="desktop-daemon-banner-update" onClick={handleUpdate} disabled={busy}>
            {busy ? "Updating…" : "Update & restart"}
          </Button>
        )}
      </div>
    </div>
  );
}
