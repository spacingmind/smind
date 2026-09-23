import { useCallback, useEffect, useState } from "react";

import type { ConfigOptionParams, RunConfigOptionsResult } from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";

/**
 * GLM/Kimi's live-session-scoped config options (thinking level, and
 * whatever else a given ACP agent advertises) -- unlike Claude's
 * thinking-level selector, this option list only exists once a live ACP
 * session has actually been created (see internal/taskrunner's
 * Runner.ConfigOptions doc comment), so it can't be a pre-run composer
 * control the way Claude's is; it's scoped to whichever run is currently
 * live in the chat view instead.
 *
 * There's no dedicated push notification for "config options just became
 * available" -- they're decided once, at session creation, before any
 * session/update event streams -- so refetchKey is meant to be something
 * that changes whenever the run receives a new event (its items.length is
 * what the caller passes). Re-fetching on every new event is what catches
 * the option list going from empty to populated shortly after the run
 * starts, without a dedicated poll loop of its own.
 */
export function useRunConfigOptions(
  client: WsClientLike | null,
  runId: string | null,
  refetchKey: number,
): {
  options: ConfigOptionParams[];
  error: string | null;
  setOption: (configId: string, value: string) => Promise<void>;
} {
  const [options, setOptions] = useState<ConfigOptionParams[]>([]);
  const [error, setError] = useState<string | null>(null);

  // Cleared on client/runId change only -- not on every refetchKey tick,
  // which fires once per streamed event and would otherwise flicker the
  // control empty on every chunk while a run is live.
  useEffect(() => {
    setOptions([]);
    setError(null);
  }, [client, runId]);

  useEffect(() => {
    if (!client || !runId) return;
    let cancelled = false;
    client
      .call<RunConfigOptionsResult>("run.listConfigOptions", { runId })
      .then((result) => {
        if (!cancelled) setOptions(result.options);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err));
      });
    return () => {
      cancelled = true;
    };
  }, [client, runId, refetchKey]);

  const setOption = useCallback(
    async (configId: string, value: string) => {
      if (!client || !runId) return;
      try {
        const result = await client.call<RunConfigOptionsResult>("run.setConfigOption", {
          runId,
          configId,
          value,
        });
        setOptions(result.options);
        setError(null);
      } catch (err) {
        setError(err instanceof Error ? err.message : String(err));
        throw err;
      }
    },
    [client, runId],
  );

  return { options, error, setOption };
}
