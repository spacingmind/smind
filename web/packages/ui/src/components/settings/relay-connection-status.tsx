import { useEffect, useState, type FormEvent } from "react";

import { Button } from "@/components/ui/button";
import { InlineSpinner } from "@/components/ui/inline-spinner";
import { Input } from "@/components/ui/input";
import { desktop, type RelayStatus } from "@/lib/platform";

/** How often the live state of the selected relay connection is re-read while Settings → Connections is open. */
export const RELAY_STATUS_POLL_MS = 2000;

/**
 * Polls the desktop shell for the selected relay connection's live state. Idle (`null`) unless
 * `active`; a failed read keeps the last known state rather than flashing an error, since the
 * next tick retries.
 */
export function useRelayStatus(active: boolean, connectionId?: string): RelayStatus | null {
  const [status, setStatus] = useState<RelayStatus | null>(null);
  useEffect(() => {
    // A new connection id means a different relay: drop the previous one's state at once rather
    // than showing it (and its password prompt) for a poll interval.
    setStatus(null);
    if (!active) return;
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
  }, [active, connectionId]);
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
export function RelayConnectionStatus({ status, connectionId }: { status: RelayStatus; connectionId?: string }) {
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
    case "passwordRequired":
    case "incorrectPassword":
    case "passwordLocked":
      return <DaemonPasswordPrompt status={status} connectionId={connectionId} />;
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

/** The longest daemon password the app will send (the daemon refuses more); kept equal to the Rust-side limit. */
export const MAX_DAEMON_PASSWORD_BYTES = 128;

/**
 * ADR-0031 P13c: the daemon has a password and this connection has none (or the one it had was
 * rejected, or too many wrong ones were tried). The password is typed here, handed to Rust once and
 * dropped from this component immediately; Rust holds it in memory only and never sends it back, so
 * nothing in this tree can read it again. Nothing is written to any web storage.
 */
function DaemonPasswordPrompt({ status, connectionId }: { status: RelayStatus; connectionId?: string }) {
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const locked = status.state === "passwordLocked";
  const waitSecs = useLockCountdown(locked ? status.retryAfterSecs : undefined);
  const quiet = "text-ui-sm text-muted-foreground";

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    const entered = password;
    if (!entered || busy || locked) return;
    if (new TextEncoder().encode(entered).length > MAX_DAEMON_PASSWORD_BYTES) {
      setError("That is too long to be the daemon's password.");
      return;
    }
    setBusy(true);
    setError(null);
    setPassword(""); // gone from this component before the call even returns
    try {
      await desktop.relaySetPassword(connectionId ?? "", entered);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Could not hand the password to the app.");
    } finally {
      setBusy(false);
    }
  }

  const headline =
    status.state === "incorrectPassword"
      ? "That password was not accepted."
      : locked
        ? `Too many wrong passwords. Try again in ${waitSecs ?? status.retryAfterSecs ?? 0} s.`
        : "This daemon needs its password.";
  return (
    <form
      onSubmit={(e) => void submit(e)}
      role="status"
      className="mt-1 flex flex-col gap-1.5"
      data-testid="connection-relay-status"
      data-state={status.state}
    >
      <span className={status.state === "passwordRequired" ? "text-ui-sm font-medium text-foreground" : "text-ui-sm font-medium text-destructive"}>
        {headline}
      </span>
      <span className={quiet}>
        Set with <code className="rounded bg-muted px-1 font-mono text-foreground">smind set-password</code> on the daemon&apos;s machine. The app keeps it in memory only
        and asks again after a restart.
      </span>
      <div className="flex items-center gap-2">
        <Input
          aria-label="Daemon password"
          data-testid="relay-password-input"
          type="password"
          autoComplete="off"
          autoCorrect="off"
          autoCapitalize="off"
          spellCheck={false}
          data-1p-ignore
          data-lpignore="true"
          placeholder="Daemon password"
          className="max-w-64"
          disabled={locked || busy}
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
        <Button type="submit" size="sm" data-testid="relay-password-submit" disabled={locked || busy || !password}>
          Connect
        </Button>
        {busy && <InlineSpinner />}
      </div>
      {error && (
        <span role="alert" className="text-ui-sm text-destructive" data-testid="relay-password-error">
          {error}
        </span>
      )}
    </form>
  );
}

/** Counts a lockout down locally from the seconds Rust reported when the state began (re-armed by a new lockout). */
function useLockCountdown(secs: number | undefined): number | undefined {
  const [left, setLeft] = useState(secs);
  useEffect(() => {
    setLeft(secs);
    if (secs === undefined) return;
    const started = Date.now();
    const timer = setInterval(() => setLeft(Math.max(0, secs - Math.floor((Date.now() - started) / 1000))), 1000);
    return () => clearInterval(timer);
  }, [secs]);
  return left;
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
