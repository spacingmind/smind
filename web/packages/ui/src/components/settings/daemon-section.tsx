import { useCallback, useEffect, useState } from "react";


import { SetupSection } from "@/components/first-run/setup-section";
import { SettingsSectionTitle } from "@/components/settings/settings-title";
import { registerSettingsSection } from "@/components/settings/settings-registry";
import { Button } from "@/components/ui/button";
import { desktop, isDesktop, type AppSettings, type DaemonProgress, type DaemonStatus } from "@/lib/platform";
import { Icons } from "@/lib/icons";

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
  // ADR-0031 D7: with `manageLocalDaemon` off the app never installs/starts/updates/takes over a daemon; status stays read-only.
  const [settings, setSettings] = useState<AppSettings | null>(null);

  const refresh = useCallback(() => {
    desktop
      .daemonStatus()
      .then(setStatus)
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)));
  }, []);

  useEffect(() => {
    refresh();
    // Unknown (a failed read) is treated as "managing", the pre-first-run behaviour.
    Promise.resolve()
      .then(() => desktop.appSettingsGet())
      .then(setSettings)
      .catch(() => {});
  }, [refresh]);

  async function enableManaged() {
    setError(null);
    setBusy(true);
    try {
      setSettings(await desktop.appSettingsSet({ manageLocalDaemon: true }));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

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
      // A failed action may have stopped the daemon: re-read the live
      // status instead of leaving the pre-action "Running" on screen.
      refresh();
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
        <SettingsSectionTitle title="Daemon" />
        <p data-testid="daemon-unsupported" className="text-ui-base text-muted-foreground">
          This platform isn&apos;t supported for app-managed daemon install. The desktop app manages a
          daemon on Windows and macOS only.
        </p>
      </div>
    );
  }

  const manageOff = settings?.manageLocalDaemon === false;
  const locked = busy || manageOff;

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
      {status.prerequisiteMessage && (
        <p role="alert" data-testid="daemon-prerequisite" className="text-ui-base text-muted-foreground">
          {status.prerequisiteMessage}
        </p>
      )}
      {status.wslDaemonSuspected && (
        <p data-testid="daemon-wsl-notice" className="text-ui-base text-muted-foreground">
          A daemon that this app did not start is answering on this port. If you run sMind inside
          WSL, stop it there, then install the Windows daemon here. Data from a WSL install is not
          migrated.
        </p>
      )}
      {busy && stageMessage && (
        <p data-testid="daemon-progress" className="text-ui-base text-muted-foreground">
          {stageMessage}
        </p>
      )}

      {manageOff && (
        <div data-testid="daemon-manage-off" className="flex flex-col gap-2 rounded-xl border bg-card p-3">
          <p className="text-ui-base text-muted-foreground">
            This app is not managing a daemon on this computer, so installing, updating, restarting and taking over are turned off. The
            status above is read-only.
          </p>
          <div>
            <Button size="sm" data-testid="daemon-enable-managed" disabled={busy} onClick={() => void enableManaged()}>
              Enable app-managed daemon
            </Button>
          </div>
        </div>
      )}

      <div className="flex gap-2">
        {status.managedState === "notRunning" && (
          <Button size="sm" data-testid="daemon-install" onClick={() => run(() => desktop.daemonInstall())} disabled={locked}>
            Install
          </Button>
        )}
        {status.managedState === "managed" && status.comparison === "older" && (
          <Button size="sm" data-testid="daemon-update" onClick={() => run(() => desktop.daemonUpdate())} disabled={locked}>
            Update & restart
          </Button>
        )}
        {status.managedState === "managed" && (
          <Button
            variant="outline"
            size="sm"
            data-testid="daemon-restart"
            onClick={() => run(() => desktop.daemonRestart())}
            disabled={locked || !status.binaryInstalled}
            title={status.binaryInstalled ? undefined : "No app-managed daemon binary is installed yet -- use Update/Install first"}
          >
            Restart
          </Button>
        )}
        {status.managedState === "unmanaged" && !status.wslDaemonSuspected && !confirmingTakeOver && (
          <Button variant="outline" size="sm" data-testid="daemon-take-over" onClick={() => setConfirmingTakeOver(true)} disabled={locked}>
            Take over management
          </Button>
        )}
        {status.managedState === "unmanaged" && !status.wslDaemonSuspected && confirmingTakeOver && (
          <>
            <span className="self-center text-ui-base text-muted-foreground">Manage the daemon already running on this port?</span>
            <Button size="sm" data-testid="daemon-take-over-confirm" onClick={() => run(() => desktop.takeOverDaemon())} disabled={locked}>
              Confirm
            </Button>
            <Button variant="ghost" size="sm" data-testid="daemon-take-over-cancel" onClick={() => setConfirmingTakeOver(false)} disabled={busy}>
              Cancel
            </Button>
          </>
        )}
      </div>
      <SetupSection />
    </div>
  );
}

if (isDesktop) {
  registerSettingsSection({
    id: "daemon",
    label: "Daemon",
    groupLabel: "Connection",
    order: 160,
    icon: <Icons.host aria-hidden className="size-3.5" />,
    render: () => <DaemonSection />,
  });
}
