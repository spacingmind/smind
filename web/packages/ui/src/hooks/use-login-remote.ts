import { useEffect, useState } from "react";

import { isRemoteLogin, type LoginLocationOverride } from "@/lib/login-remote";
import { desktop, isDesktop, type Connection } from "@/lib/platform";
import { useLoginLocationOverride, useLoginLocationStore } from "@/stores/login-location-store";

/**
 * The webview's current hostname, behind a mutable source so tests can pin
 * it per scenario (jsdom's location is otherwise read-only).
 */
export const hostnameSource: { get(): string } = { get: () => window.location.hostname };

export function useHostname(): string {
  return hostnameSource.get();
}

/**
 * Where an OAuth login's browser half runs (ADR-0033 D2–D5): on desktop,
 * derived from the selected connection's kind (`getCurrentConnection`,
 * read once on mount); on web, always remote unless the page itself is
 * served from the daemon's loopback origin. The user's per-connection
 * answer ("sign in on this device / from another device") overrides the
 * derived value and persists in the login-location store.
 *
 * Before the desktop connection resolves, `connection` is null and the
 * webview's hostname (loopback) applies -- a local first frame, corrected
 * as soon as the connection is known.
 */
export function useLoginRemote(): {
  remote: boolean;
  derivedRemote: boolean;
  override: LoginLocationOverride | null;
  /** True once the connection is known: at once on web, after `getCurrentConnection` settles on desktop. */
  resolved: boolean;
  setOverride(v: LoginLocationOverride | null): void;
} {
  const [connection, setConnection] = useState<Pick<Connection, "id" | "kind"> | null>(null);
  const [settled, setSettled] = useState(!isDesktop);

  useEffect(() => {
    if (!isDesktop) return;
    let cancelled = false;
    desktop
      .getCurrentConnection()
      .then((c) => {
        if (!cancelled) setConnection({ id: c.id, kind: c.kind });
      })
      .catch(() => {})
      .finally(() => {
        if (!cancelled) setSettled(true);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const connectionId = isDesktop ? (connection?.id ?? "") : "web";
  const hostname = useHostname();
  const override = useLoginLocationOverride(connectionId);
  const derivedRemote = isRemoteLogin({ connection, hostname, override: null });
  const remote = isRemoteLogin({ connection, hostname, override });

  return {
    remote,
    derivedRemote,
    override,
    resolved: settled,
    // Before the desktop connection resolves the id is unknown -- never
    // persist an override under "".
    setOverride: (v) => {
      if (connectionId === "") return;
      useLoginLocationStore.getState().setOverride(connectionId, v);
    },
  };
}
