import { useEffect, useState } from "react";

import { desktop, type RelayStatus } from "@/lib/platform";

/** How often the live state of the selected relay connection is re-read while Settings → Connections is open. */
export const RELAY_STATUS_POLL_MS = 2000;

/**
 * Polls the desktop shell for the selected relay connection's live state. Idle (`null`) unless
 * `active`; a failed read keeps the last known state rather than flashing an error, since the
 * next tick retries.
 */
export function useRelayStatus(active: boolean): RelayStatus | null {
  const [status, setStatus] = useState<RelayStatus | null>(null);
  useEffect(() => {
    if (!active) {
      setStatus(null);
      return;
    }
    let cancelled = false;
    const read = () => {
      desktop
        .relayStatus()
        .then((s) => {
          if (!cancelled) setStatus(s);
        })
        .catch(() => {});
    };
    read();
    const timer = setInterval(read, RELAY_STATUS_POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(timer);
    };
  }, [active]);
  return status;
}

/** The copy-pasteable command that approves this device on the daemon's machine. */
function approveCommand(deviceId: string): string {
  return `smind relay devices approve ${deviceId}`;
}

/** The command, on a line of its own so it is easy to read and copy (and never split mid-word). */
function Command({ deviceId }: { deviceId: string | undefined }) {
  return (
    <code
      className="block w-fit max-w-full rounded-md bg-muted px-2 py-1 font-mono text-ui-sm break-words text-foreground select-all"
      data-testid="connection-relay-command"
    >
      {approveCommand(deviceId ?? "<device id>")}
    </code>
  );
}

/**
 * What the relay session is doing, under the current relay connection. The two states a person
 * must act on — waiting for approval, and revoked — spell out the command to run on the machine
 * that runs the daemon.
 */
export function RelayConnectionStatus({ status }: { status: RelayStatus }) {
  const quiet = "text-ui-sm text-muted-foreground";
  switch (status.state) {
    case "connected":
      return (
        <span className={quiet} data-testid="connection-relay-status" data-state="connected">
          Connected through the relay
        </span>
      );
    case "connecting":
    case "reconnecting":
      return (
        <span className={quiet} data-testid="connection-relay-status" data-state={status.state}>
          {status.state === "connecting" ? "Connecting through the relay…" : "Reconnecting through the relay…"}
        </span>
      );
    case "awaitingApproval":
      return (
        <div role="status" className="mt-1 flex flex-col gap-1.5" data-testid="connection-relay-status" data-state="awaitingApproval">
          <span className="text-ui-sm font-medium text-foreground">Waiting for approval on the daemon.</span>
          <span className={quiet}>On the machine that runs it, run:</span>
          <Command deviceId={status.deviceId} />
          <span className={quiet}>This connection retries by itself, so it connects within seconds of the approval.</span>
        </div>
      );
    case "revoked":
      return (
        <div role="status" className="mt-1 flex flex-col gap-1.5" data-testid="connection-relay-status" data-state="revoked">
          <span className="text-ui-sm font-medium text-destructive">This device was revoked on the daemon, so it cannot connect.</span>
          <span className={quiet}>To allow it again, run this on the daemon&apos;s machine:</span>
          <Command deviceId={status.deviceId} />
          <span className={quiet}>Or remove this connection.</span>
        </div>
      );
    case "refused":
      return (
        <div role="status" className="mt-1 flex flex-col gap-1" data-testid="connection-relay-status" data-state="refused">
          <span className="text-ui-sm font-medium text-destructive">{refusalHeadline(status.code)}</span>
          <span className={quiet}>
            {status.message ?? "No reason given."}
            {status.code ? ` (${status.code})` : ""}
          </span>
        </div>
      );
  }
}

/** Codes the desktop itself raises (not the daemon's refusal of a hello) get their own headline. */
function refusalHeadline(code: string | undefined): string {
  switch (code) {
    case "device_key_unavailable":
      return "This device's key cannot be read.";
    case "daemon_outdated":
    case "hello_version_unsupported":
      return "This daemon and this app are on different versions.";
    case "device_hello_unsupported":
    case "device_hello_required":
    case "daemon_not_answering":
      return "Could not agree with the daemon on how to connect.";
    default:
      return "The daemon refused this device.";
  }
}
