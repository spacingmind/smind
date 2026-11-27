import { useEffect, useState } from "react";

import { desktop, isDesktop } from "@/lib/platform";

/**
 * True when this desktop app is connected to its daemon through the relay (connection kind "relay"): the local-only
 * RPCs are refused there, so the UI shows read-only copy and makes no call. Always false on the web build (there is no
 * connection object). `ready` is false until the connection is known; callers make no call before it.
 */
export function useViaRelay(): { viaRelay: boolean; ready: boolean } {
  const [state, setState] = useState<{ viaRelay: boolean; ready: boolean }>({ viaRelay: false, ready: !isDesktop });
  useEffect(() => {
    if (!isDesktop) return;
    let cancelled = false;
    desktop
      .getCurrentConnection()
      .then((c) => {
        if (!cancelled) setState({ viaRelay: c.kind === "relay", ready: true });
      })
      .catch(() => {
        if (!cancelled) setState({ viaRelay: false, ready: true });
      });
    return () => {
      cancelled = true;
    };
  }, []);
  return state;
}
