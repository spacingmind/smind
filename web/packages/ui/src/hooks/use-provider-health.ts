import { useCallback, useEffect, useRef, useState } from "react";

import type { ProviderHealth } from "@/lib/model-browser";
import { accountHealthTestKey } from "@/lib/provider-health";
import type { ProviderInfo, ProviderTestResult } from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";

/**
 * Lazy per-provider health for the combined model picker (Paseo disables an
 * unavailable provider with its reason). `provider.test` is a local PATH /
 * stored-credential check -- no network -- but it is still only run when the
 * picker is first opened, never on render or on an interval: `ensure()` is
 * single-flight per provider id, so opening the popover twice (or while the
 * first round is still in flight) tests each provider exactly once per
 * client. A provider list that grows after the first open (provider.list
 * answering after the fallback list) is tested on the next open for just the
 * new ids. A failed RPC leaves that provider untested, i.e. enabled -- never
 * disabled on a guess.
 */
export function useProviderHealth(
  client: WsClientLike | null,
  providers: ProviderInfo[],
): { health: Record<string, ProviderHealth | undefined>; ensure: () => void } {
  const [health, setHealth] = useState<Record<string, ProviderHealth | undefined>>({});
  const requested = useRef<Set<string>>(new Set());
  const providersRef = useRef(providers);
  providersRef.current = providers;

  // A new client is a new daemon: forget what was tested against the last one.
  useEffect(() => {
    requested.current = new Set();
    setHealth({});
  }, [client]);

  const ensure = useCallback(() => {
    if (!client) return;
    const all = providersRef.current;
    for (const info of all) {
      if (requested.current.has(info.id)) continue;
      requested.current.add(info.id);
      client
        .call<ProviderTestResult>("provider.test", { provider: accountHealthTestKey(info.id, all) })
        .then((r) => setHealth((prev) => ({ ...prev, [info.id]: { ok: r.ok, detail: r.detail } })))
        .catch(() => {
          // Untested, not unhealthy: allow a later open to try again.
          requested.current.delete(info.id);
        });
    }
  }, [client]);

  return { health, ensure };
}
