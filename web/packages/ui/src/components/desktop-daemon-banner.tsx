import { useCallback, useEffect, useRef, useState } from "react";

import { Button } from "@/components/ui/button";
import { useCopyFeedback } from "@/components/timeline/message-actions";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { compareVersions, dismissNotice, isNoticeDismissed, managedByLabel, noticeToken, updateCommands, type UpdateCommand } from "@/lib/daemon-version";
import { Icons } from "@/lib/icons";
import { desktop, isDesktop, type Connection, type ConnectionVersionInfo, type DaemonProgress } from "@/lib/platform";
import type { WsClient } from "@/lib/ws-client";

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
 *
 * ADR-0031 P10: a daemon the app does not manage (a url/relay connection, a
 * service-managed or hand-started local daemon) gets a dismissible notice
 * with the command that updates it, copyable, instead of an action. A relay
 * connection has no `/healthz`, so its version is read over the connection
 * itself (`daemon.info` on `client`); a relay daemon too old to answer shows
 * "version unknown" and the installer commands, never "up to date".
 */
export function DesktopDaemonBanner({
  runsLoaded = false,
  runningRuns = 0,
  client = null,
}: {
  runsLoaded?: boolean;
  runningRuns?: number;
  client?: WsClient | null;
}) {
  const [current, setCurrent] = useState<Connection | null>(null);
  const [info, setInfo] = useState<ConnectionVersionInfo | null>(null);
  const [managed, setManaged] = useState(false);
  const [busy, setBusy] = useState(false);
  const [stageMessage, setStageMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [armed, setArmed] = useState(true);
  const [confirmOpen, setConfirmOpen] = useState(false);
  /** What `daemon.info` said, when `/healthz` could not tell us (relay); "unsupported" = the daemon predates the method. */
  const [reported, setReported] = useState<DaemonReport | "unsupported" | null>(null);
  const [dismissedToken, setDismissedToken] = useState<string | null>(null);

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

  // `/healthz` is out of reach for a relay connection (and can fail for a
  // tunnel); the connection itself can still answer `daemon.info`. Asked
  // only when the probe produced no version, and never for an older daemon
  // that lacks the method -- its absence is the answer.
  const needsReport = info !== null && info.daemonVersion === null;
  useEffect(() => {
    if (!needsReport || !client) return;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;
    // A daemon from before this method never answers an unknown method (it
    // dropped the request), so silence has to count as "unsupported" too.
    const silence = new Promise<"unsupported">((resolve) => {
      timer = setTimeout(() => resolve("unsupported"), DAEMON_INFO_TIMEOUT_MS);
    });
    Promise.race([client.call<DaemonReport>("daemon.info"), silence])
      .then((r) => !cancelled && setReported(r))
      .catch((err: unknown) => {
        // "method not found" is the pre-daemon.info daemon (newer daemons say
        // so); anything else (connection not ready, dropped) tells us
        // nothing -- stay quiet.
        if (!cancelled && err instanceof Error && /method not found/i.test(err.message)) setReported("unsupported");
      })
      .finally(() => clearTimeout(timer));
    return () => {
      cancelled = true;
      clearTimeout(timer);
    };
  }, [needsReport, client, current?.id]);

  const daemonVersion = info?.daemonVersion ?? (reported && reported !== "unsupported" ? reported.version : null);
  const managedBy = info?.managedBy ?? (reported && reported !== "unsupported" ? (reported.managedBy ?? null) : null);
  const comparison = info === null ? "unknown" : info.daemonVersion !== null ? info.comparison : daemonVersion !== null ? compareVersions(info.appVersion, daemonVersion) : "unknown";
  const versionUnknown = current?.kind === "relay" && info !== null && daemonVersion === null && reported === "unsupported";

  const actionable = current?.kind === "local" && managed;
  const older = comparison === "older";
  const token = info ? noticeToken(daemonVersion, info.appVersion) : "";
  const dismissed = !actionable && current !== null && (dismissedToken === token || isNoticeDismissed(current.id, token));

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

  if (!isDesktop || !current || !info || !(older || versionUnknown) || dismissed) {
    return null;
  }

  function handleManualUpdate() {
    // Until the run snapshot is loaded, "0 running" is unknown, not idle --
    // ask first rather than risk interrupting work we can't see yet.
    if (!runsLoaded || runningRuns > 0) setConfirmOpen(true);
    else void runUpdate();
  }

  const deferring = actionable && runningRuns > 0;
  const commands = actionable ? [] : updateCommands(daemonVersion);

  return (
    <div
      data-testid="desktop-daemon-banner"
      role="status"
      className="flex items-start justify-between gap-3 border-b border-warning/40 bg-warning/10 px-4 py-2 text-ui-base text-foreground"
    >
      <div className="flex min-w-0 flex-col gap-1.5">
        <span>
          {versionUnknown
            ? `Daemon version unknown — it may be older than this app (v${info.appVersion})`
            : `Daemon v${daemonVersion} is older than this app (v${info.appVersion})`}
          {!actionable && managedBy ? ` · managed by ${managedByLabel(managedBy)}` : null}
          {busy
            ? ` — Updating daemon…${stageMessage ? ` — ${stageMessage}` : ""}`
            : deferring && armed
              ? ` · ${runningRuns} ${runningRuns === 1 ? "agent" : "agents"} running — will update when ${runningRuns === 1 ? "it" : "they"} finish`
              : null}
        </span>
        {commands.length > 0 && (
          <div className="flex flex-col gap-1" data-testid="desktop-daemon-banner-commands">
            <span className="text-ui-sm text-muted-foreground">{commands.length === 1 ? "Update it by running:" : "Update it by running, on that machine:"}</span>
            {commands.map((c) => (
              <CommandRow key={c.command} command={c} />
            ))}
          </div>
        )}
      </div>
      <div className="flex shrink-0 items-center gap-2">
        {!actionable && (
          <Button
            size="icon-xs"
            variant="ghost"
            aria-label="Dismiss"
            title="Dismiss until the daemon or app version changes"
            data-testid="desktop-daemon-banner-dismiss"
            onClick={() => {
              dismissNotice(current.id, token);
              setDismissedToken(token);
            }}
          >
            <Icons.close aria-hidden />
          </Button>
        )}
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

/** How long a connected daemon gets to answer `daemon.info` before it counts as predating the method. */
const DAEMON_INFO_TIMEOUT_MS = 4000;

/** What `daemon.info` returns that the banner reads. */
interface DaemonReport {
  version: string;
  commit?: string;
  managedBy?: string;
}

/** One copyable command: the code, and a button that puts exactly that text on the clipboard. */
function CommandRow({ command }: { command: UpdateCommand }) {
  const { copied, copy } = useCopyFeedback();
  return (
    <div className="flex flex-wrap items-center gap-2">
      {command.label && <span className="w-36 shrink-0 text-ui-sm text-muted-foreground">{command.label}</span>}
      <code
        data-testid="desktop-daemon-banner-command"
        className="min-w-0 select-all break-all rounded-md border border-border bg-background px-2 py-0.5 font-mono text-ui-sm text-foreground"
      >
        {command.command}
      </code>
      <Button size="xs" variant="outline" data-testid="desktop-daemon-banner-copy" onClick={() => void copy(command.command)}>
        {copied ? <Icons.check aria-hidden /> : <Icons.copy aria-hidden />}
        <span aria-live="polite">{copied ? "Copied" : "Copy"}</span>
      </Button>
    </div>
  );
}
