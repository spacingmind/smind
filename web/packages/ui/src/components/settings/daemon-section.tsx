import { useCallback, useEffect, useState } from "react";

import { registerSettingsSection } from "@/components/settings/settings-registry";
import { Button } from "@/components/ui/button";
import { desktop, isDesktop, type DaemonProgress, type DaemonStatus } from "@/lib/platform";

/**
 * ADR-0013 part D2's Settings -> Daemon: status, version, managed/
 * unmanaged, and the one action that applies (install/update/restart/take
 * over), plus the log file location. Registered only in a desktop build,
 * same pattern as connections-section.tsx.
 */
function DaemonSection() {
  const [status, setStatus] = useState<DaemonStatus | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [stageMessage, setStageMessage] = useState<string | null>(null);
  const [confirmingTakeOver, setConfirmingTakeOver] = useState(false);

  const refresh = useCallback(() => {
    desktop
      .daemonStatus()
      .then(setStatus)
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)));
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  useEffect(() => {
    return desktop.onDaemonProgress((p: DaemonProgress) => setStageMessage(p.message));
  }, []);

  async function run(action: () => Promise<DaemonStatus>) {
    setError(null);
    setBusy(true);
    setStageMessage(null);
    try {
      const next = await action();
      setStatus(next);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
      setStageMessage(null);
      setConfirmingTakeOver(false);
    }
  }

  if (!status) {
    return (
      <div className="flex flex-col gap-2" data-testid="settings-section-daemon">
        {error && (
          <p role="alert" data-testid="daemon-error" className="text-ui-base text-destructive">
            {error}
          </p>
        )}
      </div>
    );
  }

  if (status.platform === "unsupported") {
    return (
      <div className="flex flex-col gap-2" data-testid="settings-section-daemon">
        <h3 className="text-ui-base font-medium text-foreground">Daemon</h3>
        <p data-testid="daemon-unsupported" className="text-ui-base text-muted-foreground">
          This platform isn&apos;t supported for app-managed daemon install. On native Windows, use{" "}
          <code>smind serve</code> directly, or run it inside WSL2.
        </p>
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-3" data-testid="settings-section-daemon">
      <h3 className="text-ui-base font-medium text-foreground">Daemon</h3>
      <dl className="grid grid-cols-2 gap-x-4 gap-y-1 text-ui-base">
        <dt className="text-muted-foreground">Status</dt>
        <dd data-testid="daemon-reachable">{status.reachable ? "Running" : "Not running"}</dd>
        <dt className="text-muted-foreground">Version</dt>
        <dd data-testid="daemon-version">{status.daemonVersion ?? "—"}</dd>
        <dt className="text-muted-foreground">Managed</dt>
        <dd data-testid="daemon-managed-state">
          {status.managedState === "managed" ? "Managed by this app" : status.managedState === "unmanaged" ? "Running, not managed by the app" : "Not running"}
        </dd>
        <dt className="text-muted-foreground">Log file</dt>
        <dd data-testid="daemon-log-path" className="truncate font-mono text-ui-sm">
          {status.logPath ?? "—"}
        </dd>
      </dl>

      {error && (
        <p role="alert" data-testid="daemon-error" className="text-ui-base text-destructive">
          {error}
        </p>
      )}
      {busy && stageMessage && (
        <p data-testid="daemon-progress" className="text-ui-base text-muted-foreground">
          {stageMessage}
        </p>
      )}

      <div className="flex gap-2">
        {status.managedState === "notRunning" && (
          <Button size="sm" data-testid="daemon-install" onClick={() => run(() => desktop.daemonInstall())} disabled={busy}>
            Install
          </Button>
        )}
        {status.managedState === "managed" && status.comparison === "older" && (
          <Button size="sm" data-testid="daemon-update" onClick={() => run(() => desktop.daemonUpdate())} disabled={busy}>
            Update & restart
          </Button>
        )}
        {status.managedState === "managed" && (
          <Button variant="outline" size="sm" data-testid="daemon-restart" onClick={() => run(() => desktop.daemonRestart())} disabled={busy}>
            Restart
          </Button>
        )}
        {status.managedState === "unmanaged" && !confirmingTakeOver && (
          <Button variant="outline" size="sm" data-testid="daemon-take-over" onClick={() => setConfirmingTakeOver(true)} disabled={busy}>
            Take over management
          </Button>
        )}
        {status.managedState === "unmanaged" && confirmingTakeOver && (
          <>
            <span className="self-center text-ui-base text-muted-foreground">Manage the daemon already running on this port?</span>
            <Button size="sm" data-testid="daemon-take-over-confirm" onClick={() => run(() => desktop.takeOverDaemon())} disabled={busy}>
              Confirm
            </Button>
            <Button variant="ghost" size="sm" data-testid="daemon-take-over-cancel" onClick={() => setConfirmingTakeOver(false)} disabled={busy}>
              Cancel
            </Button>
          </>
        )}
      </div>
    </div>
  );
}

if (isDesktop) {
  registerSettingsSection({
    id: "daemon",
    label: "Daemon",
    order: 160,
    render: () => <DaemonSection />,
  });
}
