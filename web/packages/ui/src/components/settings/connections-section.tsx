import { useCallback, useEffect, useRef, useState } from "react";

import { ConnectMethodPanel, CONNECT_METHOD_LABELS, useConnectMethod, type ConnectMethodId } from "@/components/first-run/connect-methods";
import { requestFirstRun } from "@/components/first-run/first-run-events";
import { registerSettingsSection } from "@/components/settings/settings-registry";
import { Button } from "@/components/ui/button";
import { desktop, isDesktop, type AppSettings, type Connection } from "@/lib/platform";
import { Icons } from "@/lib/icons";
import { cn } from "@/lib/utils";

/**
 * ADR-0013 AC6's host picker, as Settings -> Connections: list every
 * saved connection, add one through the single "Add connection" menu
 * (URL / SSH / relay link -- the same forms first-run shows, ADR-0031 D7),
 * remove a non-local one, and switch which one is current; plus the
 * "Manage this computer's daemon" toggle and "Run setup again". Registered
 * only in a desktop build (`isDesktop`, checked once at module load below)
 * -- `settings-screen.tsx` imports this file unconditionally for its
 * registration side effect, same as every other built-in section, but
 * the daemon-embedded build never runs with the flag set, so this
 * section simply never registers itself there.
 *
 * Reachability is shown only for the *current* connection, driven by
 * the same connection status the rest of the app already tracks
 * (App.tsx's `connectionStatus`) -- a saved-but-not-selected connection
 * can't be probed from here without a direct cross-origin fetch to its
 * real daemon URL, which is exactly what the loopback proxy exists to
 * avoid (ADR-0013's hard constraint: no daemon changes, no CORS).
 */
function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

const ADD_METHODS: ConnectMethodId[] = ["url", "ssh", "relay"];

function ConnectionsSection() {
  const [connections, setConnections] = useState<Connection[]>([]);
  const [current, setCurrent] = useState<Connection | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [menuOpen, setMenuOpen] = useState(false);
  const connect = useConnectMethod();
  const [panelOpen, setPanelOpen] = useState(false);

  const refresh = useCallback(() => {
    Promise.all([desktop.listConnections(), desktop.getCurrentConnection()])
      .then(([list, cur]) => {
        setConnections(list);
        setCurrent(cur);
      })
      .catch((err: unknown) => setError(errorMessage(err)));
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  // A relay link pasted into the URL form moves `connect.method` to "relay": make sure the panel is showing.
  useEffect(() => {
    if (connect.method) setPanelOpen(true);
  }, [connect.method]);

  async function handleSelect(id: string) {
    setError(null);
    try {
      await desktop.selectConnection(id);
      refresh();
    } catch (err) {
      setError(errorMessage(err));
    }
  }

  async function handleRemove(id: string) {
    setError(null);
    try {
      await desktop.removeConnection(id);
      refresh();
    } catch (err) {
      setError(errorMessage(err));
    }
  }

  function openMethod(method: ConnectMethodId) {
    setMenuOpen(false);
    connect.setMethod(method);
    setPanelOpen(true);
  }

  function closePanel() {
    setPanelOpen(false);
    connect.setMethod(null);
    connect.clearNotice();
  }

  const method = panelOpen ? connect.method : null;

  return (
    <div className="flex flex-col gap-6" data-testid="settings-section-connections">
      <section className="flex flex-col gap-2">
        <div className="flex items-center justify-between gap-2">
          <h3 className="text-ui-base font-medium text-foreground">Daemon connections</h3>
          <div className="relative">
            <Button
              variant="outline"
              size="sm"
              aria-haspopup="menu"
              aria-expanded={menuOpen}
              data-testid="connection-add-menu"
              onClick={() => setMenuOpen((o) => !o)}
            >
              <Icons.add aria-hidden className="size-3.5" />
              Add connection
            </Button>
            {menuOpen && (
              <div
                role="menu"
                aria-label="Add connection"
                data-testid="connection-add-menu-items"
                className="absolute right-0 top-full z-10 mt-1 flex min-w-52 flex-col rounded-xl border bg-popover p-1 text-popover-foreground shadow-md"
              >
                {ADD_METHODS.map((m) => (
                  <button
                    key={m}
                    type="button"
                    role="menuitem"
                    data-testid={`connection-add-menu-${m}`}
                    className="rounded-lg px-2 py-1.5 text-left text-ui-base outline-none hover:bg-hover focus-visible:bg-hover"
                    onClick={() => openMethod(m)}
                  >
                    {CONNECT_METHOD_LABELS[m]}
                  </button>
                ))}
              </div>
            )}
          </div>
        </div>
        <ul className="flex flex-col gap-1 rounded-xl border bg-card p-3">
          {connections.map((c) => (
            <li
              key={c.id}
              data-testid={`connection-row-${c.id}`}
              className="flex items-center justify-between gap-2 rounded-lg px-2 py-1.5 hover:bg-hover"
            >
              <div className="flex min-w-0 flex-col">
                <span className="text-ui-base text-foreground">
                  {c.label}
                  {c.kind === "ssh" && (
                    <span className="ml-2 rounded border px-1 text-ui-sm text-muted-foreground" data-testid={`connection-kind-${c.id}`}>
                      SSH
                    </span>
                  )}
                  {current?.id === c.id && (
                    <span className="ml-2 text-ui-sm text-muted-foreground" data-testid={`connection-current-${c.id}`}>
                      (current)
                    </span>
                  )}
                </span>
                <span className="truncate font-mono text-ui-sm text-muted-foreground">{c.baseUrl}</span>
              </div>
              <div className="flex shrink-0 gap-1">
                {current?.id !== c.id && (
                  <Button
                    variant="outline"
                    size="sm"
                    data-testid={`connection-select-${c.id}`}
                    onClick={() => handleSelect(c.id)}
                  >
                    Switch
                  </Button>
                )}
                {c.kind !== "local" && (
                  <Button variant="ghost" size="sm" data-testid={`connection-remove-${c.id}`} onClick={() => handleRemove(c.id)}>
                    Remove
                  </Button>
                )}
              </div>
            </li>
          ))}
        </ul>
        {error && (
          <p role="alert" data-testid="connection-list-error" className="text-ui-base text-destructive">
            {error}
          </p>
        )}
      </section>
      {method && (
        <section className="flex flex-col gap-2 rounded-xl border bg-card p-3" data-testid="connection-add-panel">
          <div className="flex items-center justify-between gap-2">
            <h3 className="text-ui-base font-medium text-foreground">{CONNECT_METHOD_LABELS[method]}</h3>
            <Button variant="ghost" size="sm" data-testid="connection-add-close" onClick={closePanel}>
              Close
            </Button>
          </div>
          <ConnectMethodPanel
            method={method}
            state={connect}
            onConnected={(conn) => {
              refresh();
              // The relay flow resets itself and stays mounted so a "switching failed" note survives.
              if (conn.kind !== "relay") closePanel();
            }}
          />
        </section>
      )}
      <ManageLocalDaemon />
      <SetupSection />
    </div>
  );
}

/** The `manageLocalDaemon` toggle (ADR-0031 D7; Paseo's `manageBuiltInDaemon`): the setting is persisted first and nothing is ever stopped from here. */
function ManageLocalDaemon() {
  const [settings, setSettings] = useState<AppSettings | null>(null);
  const [supported, setSupported] = useState(true);
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const inFlight = useRef(false);

  useEffect(() => {
    let cancelled = false;
    desktop
      .appSettingsGet()
      .then((s) => !cancelled && setSettings(s))
      .catch(() => {});
    desktop
      .daemonStatus()
      .then((st) => !cancelled && setSupported(st.platform !== "unsupported"))
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, []);

  async function persist(next: boolean) {
    if (inFlight.current) return;
    inFlight.current = true;
    setBusy(true);
    setError(null);
    try {
      setSettings(await desktop.appSettingsSet({ manageLocalDaemon: next }));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      inFlight.current = false;
      setBusy(false);
      setConfirming(false);
    }
  }

  async function toggle() {
    if (!settings || inFlight.current) return;
    if (!settings.manageLocalDaemon) return persist(true);
    // Turning it off while the app owns a running daemon asks first (the daemon keeps running either way).
    const status = await desktop.daemonStatus().catch(() => null);
    if (status && status.managedState === "managed" && status.reachable) {
      setConfirming(true);
      return;
    }
    return persist(false);
  }

  if (!settings || !supported) return null;
  const on = settings.manageLocalDaemon;

  return (
    <section className="flex flex-col gap-2" data-testid="connection-manage-section">
      <div className="flex items-center justify-between gap-4">
        <div className="flex flex-col gap-0.5">
          <h3 className="text-ui-base font-medium text-foreground">Manage this computer&apos;s daemon</h3>
          <p className="max-w-sm text-ui-base text-muted-foreground">
            {on
              ? "The app installs, starts and updates the local daemon."
              : "The app never installs, starts, updates or takes over a daemon here. Switching to This computer still works if one is running."}
          </p>
        </div>
        <button
          type="button"
          role="switch"
          aria-checked={on}
          aria-label="Manage this computer's daemon"
          data-testid="connection-manage-toggle"
          disabled={busy}
          onClick={() => void toggle()}
          className={cn(
            "h-5 w-9 shrink-0 rounded-full border border-input-border transition-colors disabled:opacity-50",
            on ? "bg-primary" : "bg-transparent",
          )}
        >
          <span className={cn("block size-3.5 rounded-full bg-background shadow transition-transform", on ? "translate-x-4" : "translate-x-0.5")} />
        </button>
      </div>
      {confirming && (
        <div role="alertdialog" aria-label="Stop managing the daemon" data-testid="connection-manage-confirm" className="flex flex-col gap-2 rounded-xl border bg-card p-3">
          <p className="text-ui-base text-muted-foreground">
            The app manages a running daemon on this computer. Turning this off stops the app from updating, restarting or taking it over. The
            daemon itself keeps running.
          </p>
          <div className="flex gap-2">
            <Button size="sm" data-testid="connection-manage-confirm-yes" disabled={busy} onClick={() => void persist(false)}>
              Stop managing
            </Button>
            <Button variant="ghost" size="sm" data-testid="connection-manage-confirm-cancel" disabled={busy} onClick={() => setConfirming(false)}>
              Cancel
            </Button>
          </div>
        </div>
      )}
      {error && (
        <p role="alert" data-testid="connection-manage-error" className="text-ui-base text-destructive">
          {error}
        </p>
      )}
    </section>
  );
}

/** "Run setup again" / "Finish setup": reopens first-run without touching any saved connection or setting. */
function SetupSection() {
  const [pending, setPending] = useState(false);

  useEffect(() => {
    let cancelled = false;
    desktop
      .appSettingsGet()
      .then((s) => !cancelled && setPending(!s.onboardingCompleted))
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <section className="flex items-center justify-between gap-4" data-testid="connection-setup-section">
      <div className="flex flex-col gap-0.5">
        <h3 className="text-ui-base font-medium text-foreground">Setup</h3>
        <p className="max-w-sm text-ui-base text-muted-foreground">
          {pending
            ? "You skipped the first-run setup. Finish it to choose how this app gets a daemon."
            : "Reopen the first-run choices. Saved connections stay as they are until you pick something."}
        </p>
      </div>
      <Button variant="outline" size="sm" data-testid="connection-run-setup" onClick={requestFirstRun}>
        {pending ? "Finish setup" : "Run setup again"}
      </Button>
    </section>
  );
}

if (isDesktop) {
  registerSettingsSection({
    id: "connections",
    label: "Daemon server",
    groupLabel: "Connection",
    order: 150,
    icon: <Icons.connections aria-hidden className="size-3.5" />,
    render: () => <ConnectionsSection />,
  });
}
