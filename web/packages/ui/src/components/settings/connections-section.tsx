import { useCallback, useEffect, useState, type FormEvent } from "react";

import { registerSettingsSection } from "@/components/settings/settings-registry";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { desktop, isDesktop, type Connection } from "@/lib/platform";

/**
 * ADR-0013 AC6's host picker, as Settings -> Connections: list every
 * saved connection, add an arbitrary `http(s)://host:port` one, remove a
 * non-local one, and switch which one is current. Registered only in a
 * desktop build (`isDesktop`, checked once at module load below) --
 * `settings-screen.tsx` imports this file unconditionally for its
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
function ConnectionsSection() {
  const [connections, setConnections] = useState<Connection[]>([]);
  const [current, setCurrent] = useState<Connection | null>(null);
  const [label, setLabel] = useState("");
  const [url, setUrl] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const refresh = useCallback(() => {
    Promise.all([desktop.listConnections(), desktop.getCurrentConnection()])
      .then(([list, cur]) => {
        setConnections(list);
        setCurrent(cur);
      })
      .catch((err: unknown) => setError(err instanceof Error ? err.message : String(err)));
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  async function handleAdd(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    setError(null);
    setBusy(true);
    try {
      await desktop.addConnection(label, url);
      setLabel("");
      setUrl("");
      refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  async function handleSelect(id: string) {
    setError(null);
    try {
      await desktop.selectConnection(id);
      refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  async function handleRemove(id: string) {
    setError(null);
    try {
      await desktop.removeConnection(id);
      refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  }

  return (
    <div className="flex flex-col gap-6" data-testid="settings-section-connections">
      <section className="flex flex-col gap-2">
        <h3 className="text-ui-base font-medium text-foreground">Daemon connections</h3>
        <ul className="flex flex-col gap-1">
          {connections.map((c) => (
            <li
              key={c.id}
              data-testid={`connection-row-${c.id}`}
              className="flex items-center justify-between gap-2 rounded-md border px-2 py-1.5"
            >
              <div className="flex min-w-0 flex-col">
                <span className="text-ui-base text-foreground">
                  {c.label}
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
      </section>
      <section className="flex flex-col gap-2">
        <h3 className="text-ui-base font-medium text-foreground">Add a connection</h3>
        <form className="flex flex-col gap-2" onSubmit={handleAdd}>
          <Input
            aria-label="Connection label"
            data-testid="connection-add-label"
            placeholder="Label (optional)"
            value={label}
            onChange={(e) => setLabel(e.target.value)}
          />
          <Input
            aria-label="Connection URL"
            data-testid="connection-add-url"
            placeholder="http://host:port"
            value={url}
            onChange={(e) => setUrl(e.target.value)}
          />
          <Button type="submit" size="sm" data-testid="connection-add-submit" disabled={busy || !url.trim()}>
            Add
          </Button>
        </form>
        {error && (
          <p role="alert" data-testid="connection-error" className="text-ui-base text-destructive">
            {error}
          </p>
        )}
      </section>
    </div>
  );
}

if (isDesktop) {
  registerSettingsSection({
    id: "connections",
    label: "Connections",
    order: 150,
    render: () => <ConnectionsSection />,
  });
}
