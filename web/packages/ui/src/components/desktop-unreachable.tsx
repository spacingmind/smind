import { useEffect, useState } from "react";

import { Button } from "@/components/ui/button";
import { desktop, type Connection } from "@/lib/platform";

/**
 * AC6's "can't reach <label> (<url>)" state: the desktop build's
 * replacement for `offline.html`'s job once the bundled UI itself is
 * what's on screen (ADR-0013's Consequences) -- shown only when the
 * initial connect to the *currently selected* connection fails outright
 * (App.tsx's `connectionStatus === "disconnected"`, which today only
 * happens pre-first-successful-connect; a later drop goes through the
 * existing automatic reconnect loop and its own status text instead, so
 * this never interrupts a connection that has already been up).
 *
 * `getCurrentConnection` is asked fresh on mount rather than trusting a
 * value passed down from a wider scope, since this is the one place in
 * the UI that specifically needs to show *which* connection failed.
 */
export function DesktopUnreachable({
  onRetry = () => window.location.reload(),
  onSwitchConnection,
}: {
  /** Overridable for tests; defaults to a full reload, same as offline.html's own "Retry now". */
  onRetry?: () => void;
  onSwitchConnection: () => void;
}) {
  const [current, setCurrent] = useState<Connection | null>(null);

  useEffect(() => {
    let cancelled = false;
    desktop
      .getCurrentConnection()
      .then((c) => {
        if (!cancelled) setCurrent(c);
      })
      .catch(() => {
        // Best-effort: the label/url are cosmetic here, and the command
        // itself might fail for the same reason the connection is
        // unreachable in the first place (e.g. a corrupt connections
        // file) -- the retry/switch actions below still work either way.
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <div
      data-testid="desktop-unreachable"
      className="flex h-full flex-col items-center justify-center gap-4 p-8 text-center"
    >
      <div className="flex flex-col gap-1">
        <h2 className="text-sm font-medium text-foreground">Can&apos;t reach {current?.label ?? "the daemon"}</h2>
        {current && <p className="font-mono text-xs text-muted-foreground">{current.baseUrl}</p>}
      </div>
      <p className="max-w-sm text-sm text-muted-foreground">
        Start the daemon (<code>smind serve</code>), or switch to a different connection.
      </p>
      <div className="flex gap-2">
        <Button variant="outline" size="sm" data-testid="desktop-unreachable-retry" onClick={onRetry}>
          Retry
        </Button>
        <Button size="sm" data-testid="desktop-unreachable-switch" onClick={onSwitchConnection}>
          Switch connection
        </Button>
      </div>
    </div>
  );
}
