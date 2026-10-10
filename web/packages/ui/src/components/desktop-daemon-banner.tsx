import { useCallback, useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { desktop, isDesktop, type Connection, type ConnectionVersionInfo, type DaemonProgress } from "@/lib/platform";

/**
 * ADR-0013 part D2's "daemon vX is older than this app (vY)" banner. Only
 * the local, app-managed daemon gets an actionable "Update & restart"
 * button -- a local-but-unmanaged daemon (the user's own `smind serve`)
 * and every remote/url/relay connection get the same text as a plain,
 * non-actionable notice instead (AC3's "never touch a daemon we don't
 * manage" rule applies here too, not just on the Rust side).
 *
 * desktop-macos-app M5: for the managed local daemon the update decision
 * lives here, not in Rust, because only the UI knows whether agent runs
 * are in flight (a restart marks them interrupted). Idle -> update
 * automatically, at most once per (connection id, daemon version) per
 * app session; busy -> defer while armed, or ask via a confirm dialog.
 *
 * `connectionVersion` (not `daemonStatus`) is what drives *this*
 * component even for the local connection, since it's the one primitive
 * that works for every connection kind (the loopback proxy doesn't
 * forward `/healthz`, so a non-local connection's version can only be
 * read this way); `daemonStatus` is asked separately, only when the
 * current connection is local, purely to learn whether it's managed.
 */
export function DesktopDaemonBanner({ runsLoaded = false, runningRuns = 0 }: { runsLoaded?: boolean; runningRuns?: number }) {
  const [current, setCurrent] = useState<Connection | null>(null);
  const [info, setInfo] = useState<ConnectionVersionInfo | null>(null);
  const [managed, setManaged] = useState(false);
  const [busy, setBusy] = useState(false);
  const [stageMessage, setStageMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [armed, setArmed] = useState(true);
  const [confirmOpen, setConfirmOpen] = useState(false);

  /** (connection id, daemon version) pairs already auto-updated this app session, so a failure never loops. */
  const autoFiredRef = useRef<Set<string>>(new Set());

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

  const runUpdate = useCallback(async () => {
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
  }, [refresh]);

  const actionable = current?.kind === "local" && managed;
  const older = info?.comparison === "older";

  // Idle and armed -> update without a click. Not before runsLoaded: an
  // empty run map before the first run.list means "not known yet", not
  // "0 running".
  useEffect(() => {
    if (!actionable || !older || !runsLoaded || runningRuns > 0 || !armed || busy) return;
    const key = `${current!.id}:${info!.daemonVersion}`;
    if (autoFiredRef.current.has(key)) return;
    autoFiredRef.current.add(key);
    runUpdate();
  }, [actionable, older, runsLoaded, runningRuns, armed, busy, current, info, runUpdate]);

  if (!isDesktop || !current || !info || !older) {
    return null;
  }

  function handleManualUpdate() {
    // Until the run snapshot is loaded, "0 running" is unknown, not idle --
    // ask first rather than risk interrupting work we can't see yet.
    if (!runsLoaded || runningRuns > 0) setConfirmOpen(true);
    else void runUpdate();
  }

  const deferring = actionable && runningRuns > 0;

  return (
    <div
      data-testid="desktop-daemon-banner"
      role="status"
      className="flex items-center justify-between gap-3 border-b border-warning/40 bg-warning/10 px-4 py-2 text-ui-base text-foreground"
    >
      <span>
        Daemon v{info.daemonVersion} is older than this app (v{info.appVersion})
        {busy
          ? ` — Updating daemon…${stageMessage ? ` — ${stageMessage}` : ""}`
          : deferring && armed
            ? ` · ${runningRuns} ${runningRuns === 1 ? "agent" : "agents"} running — will update when ${runningRuns === 1 ? "it" : "they"} finish`
            : null}
      </span>
      <div className="flex items-center gap-2">
        {error && (
          <>
            <span role="alert" data-testid="desktop-daemon-banner-error" className="text-destructive">
              {error}
            </span>
            <Button size="sm" variant="outline" data-testid="desktop-daemon-banner-retry" onClick={() => void runUpdate()} disabled={busy}>
              Retry
            </Button>
          </>
        )}
        {actionable && deferring && armed && (
          <Button size="sm" variant="outline" data-testid="desktop-daemon-banner-update-now" onClick={() => setConfirmOpen(true)} disabled={busy}>
            Update now
          </Button>
        )}
        {actionable && deferring && armed && !busy && !error && (
          <Button size="sm" variant="ghost" data-testid="desktop-daemon-banner-not-now" onClick={() => setArmed(false)}>
            Not now
          </Button>
        )}
        {actionable && (!deferring || !armed) && (
          <Button size="sm" data-testid="desktop-daemon-banner-update" onClick={handleManualUpdate} disabled={busy}>
            {busy ? "Updating…" : "Update & restart"}
          </Button>
        )}
      </div>
      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <DialogContent data-testid="desktop-daemon-banner-confirm">
          <DialogHeader>
            <DialogTitle>Update daemon now?</DialogTitle>
            <DialogDescription>
              {runsLoaded
                ? `Running agent work will be interrupted — ${runningRuns} ${runningRuns === 1 ? "agent is" : "agents are"} currently running.`
                : "Running agent work will be interrupted. Agent status is still loading, so some agents may be running."}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setConfirmOpen(false)}>
              Cancel
            </Button>
            <Button
              variant="destructive"
              data-testid="desktop-daemon-banner-confirm-update"
              onClick={() => {
                setConfirmOpen(false);
                void runUpdate();
              }}
            >
              Update & restart
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
