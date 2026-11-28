import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";

import { useFleetHosts } from "@/hooks/use-fleet-hosts";
import { desktop, isDesktop, type Connection } from "@/lib/platform";

/**
 * The hosts the "Add project" dialog's Where step shows (C1 D1): the current
 * connection (a chip when it is the only one), the desktop's other saved
 * connections (listed, never switched from inside the dialog -- ADR-0013
 * switches the whole app, so that stays in Settings -> Connections), and the
 * approved fleet hosts (rows with the reason, disabled: a peer cannot create,
 * ADR-0048). The web build has no connection object, so the page's own host
 * stands in for the current one.
 */

/** One saved desktop connection other than the current one. */
export interface SavedProjectHost {
  id: string;
  label: string;
  /** The connection's address (host:port), muted next to the name; empty when it has none to show. */
  address: string;
  current: boolean;
}

/** One fleet host the step lists disabled: visible, never selectable here. */
export interface FleetProjectHost {
  hostId: string;
  name: string;
  reason: string;
}

export interface ProjectHosts {
  /** The per-host recents key (C1 D4): the desktop connection id, or the page origin on the web. */
  hostKey: string;
  current: { name: string; address: string };
  saved: SavedProjectHost[];
  fleet: FleetProjectHost[];
  /** No other saved connection and no approved fleet host: the step collapses to the one chip. */
  single: boolean;
}

const connectionsKey = ["desktop.connections"] as const;

/** `host:port` of a connection's base URL; empty when it is not a plain URL (a relay link). */
function addressOf(baseUrl: string): string {
  try {
    return new URL(baseUrl).host;
  } catch {
    return "";
  }
}

interface ConnectionsData {
  current: Connection;
  saved: SavedProjectHost[];
}

export function useProjectHosts(): ProjectHosts {
  const fleetList = useFleetHosts();
  // The desktop's saved Connections are an app-shell read, not a daemon RPC:
  // one cache entry, fetched only in the desktop build (the web build's
  // `desktop` is the always-rejecting stub).
  const connections = useQuery({
    queryKey: connectionsKey,
    enabled: isDesktop,
    staleTime: Infinity,
    retry: false,
    queryFn: async (): Promise<ConnectionsData> => {
      const current = await desktop.getCurrentConnection();
      const saved = (await desktop.listConnections())
        .filter((c) => c.id !== current.id)
        .map((c) => ({ id: c.id, label: c.label, address: addressOf(c.baseUrl), current: false }));
      return { current, saved };
    },
  });

  const fleet = useMemo<FleetProjectHost[]>(() => {
    const list = fleetList.data;
    // Pending or off both read as no fleet hosts; pending and revoked are
    // never listed (a revoked host cannot serve the flow either).
    if (!list || list.off) return [];
    return list.hosts
      .filter((h) => h.state === "approved")
      .map((h) => ({ hostId: h.hostId, name: h.alias || h.label, reason: "Adding projects on fleet hosts isn't supported yet" }));
  }, [fleetList.data]);

  const data = isDesktop ? connections.data ?? null : null;
  const saved = data?.saved ?? [];
  return {
    hostKey: data ? data.current.id : window.location.origin,
    // A friendly name first ("This Mac", or the saved connection's name); the
    // address is secondary. The web build names the page's own server.
    current: data
      ? { name: data.current.label, address: addressOf(data.current.baseUrl) }
      : { name: "This server", address: window.location.host },
    saved,
    fleet,
    single: saved.length === 0 && fleet.length === 0,
  };
}
