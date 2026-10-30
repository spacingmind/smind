import { useCallback, useEffect, useState } from "react";


import { SetupSection } from "@/components/first-run/setup-section";
import { registerSettingsSection } from "@/components/settings/settings-registry";
import { SettingsGroup, SettingsRow } from "@/components/settings/settings-layout";
import { Alert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { managedByLabel } from "@/lib/daemon-version";
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
        {error && <Alert variant="error" testId="daemon-error" description={error} />}
      </div>
    );
  }

  if (status.platform === "unsupported") {
    return (
      <div className="flex flex-col gap-2" data-testid="settings-section-daemon">
        <SettingsGroup title="Daemon">
          <p data-testid="daemon-unsupported" className="px-4 py-3 text-ui-base text-muted-foreground">
            This platform isn&apos;t supported for app-managed daemon install. The desktop app manages a
            daemon on Windows and macOS only.
          </p>
        </SettingsGroup>
      </div>
    );
  }

  const manageOff = settings?.manageLocalDaemon === false;
  const locked = busy || manageOff;

  return (
    <div className="flex flex-col gap-3" data-testid="settings-section-daemon">
      <SettingsGroup title="Daemon">
        <SettingsRow
          label="Status"
          control={<span data-testid="daemon-reachable">{status.reachable ? "Running" : "Not running"}</span>}
        />
        <SettingsRow label="Version" control={<span data-testid="daemon-version">{status.daemonVersion ?? "—"}</span>} />
        <SettingsRow
          label="Managed"
          control={
            <span data-testid="daemon-managed-state">
              {status.managedBy
                ? `Managed by ${managedByLabel(status.managedBy)}`
                : status.managedState === "managed"
                  ? "Managed by this app"
                  : status.managedState === "unmanaged"
                    ? "Running, not managed by the app"
                    : "Not running"}
            </span>
          }
        />
        <SettingsRow
          label="Log file"
          control={
            <span data-testid="daemon-log-path" className="block truncate font-mono text-ui-sm">
              {status.logPath ?? "—"}
            </span>
          }
        />

        {error && (
          <div className="px-4 py-3">
            <Alert variant="error" testId="daemon-error" description={error} />
          </div>
        )}
        {status.prerequisiteMessage && (
          <p role="alert" data-testid="daemon-prerequisite" className="px-4 py-3 text-ui-base text-muted-foreground">
            {status.prerequisiteMessage}
          </p>
        )}
        {status.managedBy === "docker" && (
          <p data-testid="daemon-service-notice" className="px-4 py-3 text-ui-base text-muted-foreground">
            This daemon runs in a Docker container (managed by Docker), so this app leaves it alone. Restart or update it with{" "}
            <code className="font-mono text-ui-sm">docker</code> (pull the new image and recreate the container).
          </p>
        )}
        {status.managedBy && status.managedBy !== "docker" && (
          <p data-testid="daemon-service-notice" className="px-4 py-3 text-ui-base text-muted-foreground">
            An OS service ({managedByLabel(status.managedBy)}) runs this daemon, so this app leaves it alone: restarting or
            stopping it from here would only make the service start it again. Manage it with{" "}
            <code className="font-mono text-ui-sm">smind service</code>, and update it with{" "}
            <code className="font-mono text-ui-sm">smind update</code>.
          </p>
        )}
        {status.wslDaemonSuspected && (
          <p data-testid="daemon-wsl-notice" className="px-4 py-3 text-ui-base text-muted-foreground">
            A daemon that this app did not start is answering on this port. If you run sMind inside
            WSL, stop it there, then install the Windows daemon here. Data from a WSL install is not
            migrated.
          </p>
        )}
        {busy && stageMessage && (
          <p data-testid="daemon-progress" className="px-4 py-3 text-ui-base text-muted-foreground">
            {stageMessage}
          </p>
        )}

        {manageOff && (
          <SettingsRow
            testId="daemon-manage-off"
            label="App-managed daemon"
            description="This app is not managing a daemon on this computer, so installing, updating, restarting and taking over are turned off. The status above is read-only."
            control={
              <Button size="sm" data-testid="daemon-enable-managed" disabled={busy} onClick={() => void enableManaged()}>
                Enable app-managed daemon
              </Button>
            }
          />
        )}

        <div className="flex flex-wrap items-center gap-2 px-4 py-3">
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
        {status.managedState === "unmanaged" && !status.wslDaemonSuspected && !status.managedBy && !confirmingTakeOver && (
          <Button variant="outline" size="sm" data-testid="daemon-take-over" onClick={() => setConfirmingTakeOver(true)} disabled={locked}>
            Take over management
          </Button>
        )}
        {status.managedState === "unmanaged" && !status.wslDaemonSuspected && !status.managedBy && confirmingTakeOver && (
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
      </SettingsGroup>
      <SetupSection />
    </div>
  );
}

if (isDesktop) {
  registerSettingsSection({
    id: "daemon",
    label: "Daemon",
    groupLabel: "Connection",
    description: "Local daemon status, version and app management.",
    order: 160,
    icon: <Icons.host aria-hidden className="size-3.5" />,
    render: () => <DaemonSection />,
  });
}
