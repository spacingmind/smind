import { useRef, useState, type FormEvent } from "react";

import { Button } from "@/components/ui/button";
import { InlineSpinner } from "@/components/ui/inline-spinner";
import { Input } from "@/components/ui/input";
import { Icons } from "@/lib/icons";
import { desktop, isRelayPairingLink, type Connection, type ProbeResult } from "@/lib/platform";

/** Where the "not reachable" explanation can send the user. */
export type ConnectMethodId = "url" | "ssh" | "relay";

function parseDaemonUrl(raw: string): URL | null {
  try {
    const url = new URL(raw.trim());
    return url.protocol === "http:" || url.protocol === "https:" ? url : null;
  } catch {
    return null;
  }
}

/**
 * ADR-0031 D7 (b): connect to a daemon by URL. Probes the endpoint BEFORE saving
 * (`desktop.probeConnection`) and shows what answered; refuses anything that is not a
 * smind daemon; asks for the daemon password only when the probe says it is required.
 * The password lives in an uncontrolled input, is read once at submit and cleared right
 * after -- it is never held in React state. Shared by first-run and Settings -> Connections.
 */
export function UrlConnectForm({
  onConnected,
  onRelayLink,
  onPickMethod,
}: {
  onConnected: (conn: Connection) => void | Promise<void>;
  /** A relay pairing link was pasted here: hand it to the relay flow instead of probing it as a URL. */
  onRelayLink: (link: string) => void;
  /** The "can't reach it" explanation offers the other ways in. */
  onPickMethod?: (method: ConnectMethodId) => void;
}) {
  const [url, setUrl] = useState("");
  const [label, setLabel] = useState("");
  const [probe, setProbe] = useState<{ url: string; result: ProbeResult } | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const passwordRef = useRef<HTMLInputElement>(null);
  const inFlight = useRef(false);

  function changeUrl(next: string) {
    setUrl(next);
    // What was probed no longer describes what is typed.
    setProbe(null);
    setError(null);
  }

  async function check() {
    if (isRelayPairingLink(url)) {
      const link = url.trim();
      setUrl("");
      onRelayLink(link);
      return;
    }
    const raw = url.trim();
    if (!parseDaemonUrl(raw)) {
      setError("Enter an http:// or https:// address, e.g. http://host:4648.");
      return;
    }
    setError(null);
    setProbe(null);
    inFlight.current = true;
    setBusy(true);
    try {
      const result = await desktop.probeConnection(raw);
      setProbe({ url: raw, result });
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      inFlight.current = false;
      setBusy(false);
    }
  }

  async function save() {
    if (!probe) return;
    const password = probe.result.authRequired ? (passwordRef.current?.value ?? "") : undefined;
    if (probe.result.authRequired && !password) {
      setError("Enter the daemon password.");
      return;
    }
    setError(null);
    inFlight.current = true;
    setBusy(true);
    try {
      const conn = await desktop.addConnection(label.trim(), probe.url, password || undefined);
      if (passwordRef.current) passwordRef.current.value = "";
      await onConnected(conn);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      if (passwordRef.current) passwordRef.current.value = "";
      inFlight.current = false;
      setBusy(false);
    }
  }

  function submit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    if (inFlight.current) return;
    void (probe && saveable(probe.result) ? save() : check());
  }

  const result = probe?.result ?? null;
  const notSmind = result?.reachable && result.service !== "smind";
  const unreachable = result !== null && !result.reachable;
  const host = probe ? new URL(probe.url).host : "";

  return (
    <form className="flex flex-col gap-2" onSubmit={submit} data-testid="url-form" autoComplete="off" noValidate>
      <p className="text-ui-base text-muted-foreground">Use an http:// or https:// address with the daemon's port (4648 by default).</p>
      <Input
        aria-label="Connection URL"
        data-testid="connection-add-url"
        autoComplete="off"
        autoCorrect="off"
        autoCapitalize="off"
        spellCheck={false}
        placeholder="http://host:4648"
        value={url}
        disabled={busy}
        onChange={(e) => changeUrl(e.target.value)}
      />
      <Input
        aria-label="Connection label"
        data-testid="connection-add-label"
        autoComplete="off"
        placeholder="Label (optional)"
        value={label}
        disabled={busy}
        onChange={(e) => setLabel(e.target.value)}
      />
      {result && saveable(result) && (
        <p data-testid="url-probe-ok" className="flex items-center gap-1.5 text-ui-base text-success">
          <Icons.success aria-hidden className="size-3.5 shrink-0" />
          Reachable{result.version ? `, v${result.version.replace(/^v/, "")}` : ""}
          {result.authRequired ? " · password required" : ""}
        </p>
      )}
      {result?.reachable && result.authRequired && (
        <div className="flex flex-col gap-1">
          <label htmlFor="url-password" className="text-ui-sm font-medium text-muted-foreground">
            Daemon password
          </label>
          <Input
            id="url-password"
            ref={passwordRef}
            data-testid="url-password"
            type="password"
            autoComplete="off"
            data-1p-ignore
            data-lpignore="true"
            placeholder="Enter the daemon password"
            disabled={busy}
          />
        </div>
      )}
      {notSmind && (
        <p role="alert" data-testid="url-not-smind" className="text-ui-base text-destructive">
          That address answers, but it is not a sMind daemon ({host}). Nothing was saved.
        </p>
      )}
      {unreachable && (
        <div role="alert" data-testid="url-unreachable" className="flex flex-col gap-1.5 text-ui-base text-muted-foreground">
          <p className="text-destructive">
            Couldn&apos;t reach {host}
            {result.error ? `: ${result.error}` : "."}
          </p>
          {result.loopback ? (
            <p>
              Nothing answers on this computer at that address. Start the daemon with{" "}
              <code className="font-mono text-ui-sm">smind serve</code>, or check the port.
            </p>
          ) : (
            <>
              <p data-testid="url-loopback-note">
                sMind daemons listen on loopback (127.0.0.1) unless a password is set, so a daemon on another machine usually can&apos;t be
                reached directly. Connect over SSH, or pair through a relay link instead.
              </p>
              {onPickMethod && (
                <div className="flex gap-2">
                  <Button type="button" variant="outline" size="sm" data-testid="url-pick-ssh" onClick={() => onPickMethod("ssh")}>
                    Connect over SSH
                  </Button>
                  <Button type="button" variant="outline" size="sm" data-testid="url-pick-relay" onClick={() => onPickMethod("relay")}>
                    Pair via relay link
                  </Button>
                </div>
              )}
            </>
          )}
        </div>
      )}
      {error && (
        <p role="alert" data-testid="connection-error" className="whitespace-pre-wrap break-words text-ui-base text-destructive">
          {error}
        </p>
      )}
      <div className="flex items-center gap-2">
        <Button type="submit" size="sm" data-testid="connection-add-submit" disabled={busy || !url.trim()}>
          {probe && saveable(probe.result) ? "Add" : "Check"}
        </Button>
        {busy && <InlineSpinner label={probe ? "Adding…" : "Checking…"} />}
      </div>
    </form>
  );
}

/** A probe result the user may save: reachable and positively a sMind daemon. */
function saveable(result: ProbeResult): boolean {
  return result.reachable && result.service === "smind";
}
