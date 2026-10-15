import { useRef, useState, type FormEvent } from "react";

import { Button } from "@/components/ui/button";
import { InlineSpinner } from "@/components/ui/inline-spinner";
import { Input } from "@/components/ui/input";
import { desktop, type Connection, type SshTarget } from "@/lib/platform";

/** The smind daemon's default port, prefilled as the remote daemon port. */
export const DEFAULT_DAEMON_PORT = 4648;

export type SshValidation = { ok: true; target: SshTarget } | { ok: false; error: string };

function parsePort(raw: string): number | null {
  const text = raw.trim();
  if (!/^\d+$/.test(text)) return null;
  const n = Number(text);
  return n >= 1 && n <= 65535 ? n : null;
}

/**
 * Client-side checks that run BEFORE anything is sent to Rust (ADR-0031 P5 item 8; Rust
 * re-validates). Mirrors Paseo's ssh-transport validation: no empty or whitespace host,
 * no host starting with `-` (it would be read as an ssh option), ports are integers 1-65535.
 */
export function validateSshTarget(input: { host: string; sshPort: string; daemonPort: string }): SshValidation {
  const host = input.host.trim();
  if (!host) return { ok: false, error: "Enter the host to connect to, e.g. homelab or user@10.0.0.5." };
  if (/\s/.test(host)) return { ok: false, error: "The host cannot contain spaces." };
  if (host.startsWith("-")) return { ok: false, error: "The host cannot start with a dash." };
  let sshPort: number | null = null;
  if (input.sshPort.trim()) {
    sshPort = parsePort(input.sshPort);
    if (sshPort === null) return { ok: false, error: "The SSH port must be a whole number from 1 to 65535." };
  }
  const daemonPort = input.daemonPort.trim() ? parsePort(input.daemonPort) : DEFAULT_DAEMON_PORT;
  if (daemonPort === null) return { ok: false, error: "The daemon port must be a whole number from 1 to 65535." };
  return { ok: true, target: { host, sshPort, daemonPort } };
}

/**
 * ADR-0031 D7 (b2): connect to a daemon on another machine through the system `ssh`
 * (`desktop.addSshConnection`). Shared by first-run and Settings -> Connections. The
 * system ssh does the authentication (keys/agent; no password prompt, BatchMode), so
 * nothing secret passes through here; ssh's stderr is shown as Rust returns it.
 */
export function SshConnectForm({ onConnected }: { onConnected: (conn: Connection) => void | Promise<void> }) {
  const [host, setHost] = useState("");
  const [sshPort, setSshPort] = useState("");
  const [daemonPort, setDaemonPort] = useState(String(DEFAULT_DAEMON_PORT));
  const [label, setLabel] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const inFlight = useRef(false);

  async function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (inFlight.current) return;
    const result = validateSshTarget({ host, sshPort, daemonPort });
    if (!result.ok) {
      setError(result.error);
      return;
    }
    setError(null);
    inFlight.current = true;
    setBusy(true);
    try {
      const conn = await desktop.addSshConnection(label.trim() || result.target.host, result.target);
      await onConnected(conn);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  }

  return (
    <form className="flex flex-col gap-2" onSubmit={submit} data-testid="ssh-form" noValidate>
      <p className="text-ui-base text-muted-foreground">
        Tunnels to a smind daemon already running on another machine through your system <code className="font-mono text-ui-sm">ssh</code>.
        It uses your SSH keys and agent; nothing changes on that machine.
      </p>
      <Input
        aria-label="SSH host"
        data-testid="ssh-host"
        autoComplete="off"
        autoCorrect="off"
        autoCapitalize="off"
        spellCheck={false}
        placeholder="Host, e.g. homelab or user@10.0.0.5"
        value={host}
        disabled={busy}
        onChange={(e) => setHost(e.target.value)}
      />
      <div className="grid grid-cols-2 gap-2">
        <Input
          aria-label="SSH port"
          data-testid="ssh-port"
          inputMode="numeric"
          autoComplete="off"
          placeholder="SSH port (optional)"
          value={sshPort}
          disabled={busy}
          onChange={(e) => setSshPort(e.target.value)}
        />
        <Input
          aria-label="Daemon port"
          data-testid="ssh-daemon-port"
          inputMode="numeric"
          autoComplete="off"
          placeholder="Daemon port"
          value={daemonPort}
          disabled={busy}
          onChange={(e) => setDaemonPort(e.target.value)}
        />
      </div>
      <Input
        aria-label="SSH connection label"
        data-testid="ssh-label"
        autoComplete="off"
        placeholder="Label (optional)"
        value={label}
        disabled={busy}
        onChange={(e) => setLabel(e.target.value)}
      />
      {error && (
        <p role="alert" data-testid="ssh-error" className="whitespace-pre-wrap break-words text-ui-base text-destructive">
          {error}
        </p>
      )}
      <div className="flex items-center gap-2">
        <Button type="submit" size="sm" data-testid="ssh-submit" disabled={busy}>
          Connect
        </Button>
        {busy && <InlineSpinner label="Connecting over SSH…" />}
      </div>
    </form>
  );
}
