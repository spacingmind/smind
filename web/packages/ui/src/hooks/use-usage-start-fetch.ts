import { QueryClient, QueryClientContext, useMutation } from "@tanstack/react-query";
import { useContext, useEffect } from "react";

import {
  claimTouch,
  DETACHED_QUERY_CLIENT,
  heldData,
  lastTouchAt,
  usageTouchMutationOptions,
  windowIsActive,
} from "@/hooks/use-usage-reports";
import { DEFAULT_TTL_SECONDS, usageMutationKeys } from "@/lib/account-usage/query";
import { useDaemonConnection } from "@/lib/query/daemon-query-provider";
import { ConnectionClosedError } from "@/lib/ws-client";

/**
 * The one `account.usage` request of an app start (ADR-0028 section 3, amendment 2026-11-27): a single
 * `touch` for every account, so the shared usage cache is filled before any usage surface opens and
 * the ambient quota notice has something to read.
 *
 * It is one per QueryClient (a page load; a reconnect to the same daemon keeps the client and so does
 * not repeat it), single-flight, skipped when the cache is already warm or a touch was claimed inside
 * the TTL, and it waits for a visible and focused window like every touch. No interval, no re-send
 * after the answer, never `refresh` or `cached`; the only retry is a touch that never left the machine
 * (`ConnectionClosedError`), which the next connection of the same app start may send. The daemon's
 * per-account TTL, floor and single-flight stay the real guarantee.
 */
const started = new WeakSet<QueryClient>();

export function useUsageStartFetch(): void {
  const { client } = useDaemonConnection();
  const queryClient = useContext(QueryClientContext) ?? DETACHED_QUERY_CLIENT;
  const touch = useMutation(usageTouchMutationOptions(client, queryClient), queryClient);
  const { mutate } = touch;

  useEffect(() => {
    if (!client) return;
    if (started.has(queryClient)) return;

    const held = heldData(queryClient);
    const ttlMs = (held?.ttlSeconds ?? DEFAULT_TTL_SECONDS) * 1000;
    const last = lastTouchAt(queryClient);
    if (last !== null && Date.now() - last < ttlMs) {
      // A touch was sent or claimed inside the TTL (a usage surface that mounted with us, a warm
      // cache), whether or not the cache holds data yet: this app start already has its request.
      started.add(queryClient);
      return;
    }
    // A touch is already in flight: leave `started` unset so a later mount may still decide.
    if (queryClient.isMutating({ mutationKey: usageMutationKeys.touch }) > 0) return;

    const send = () => {
      if (started.has(queryClient)) return;
      // A surface's touch may have claimed the TTL since this effect decided to wait for an active
      // window: the app start has its one request, from that sender.
      const lastNow = lastTouchAt(queryClient);
      if (lastNow !== null && Date.now() - lastNow < ttlMs) {
        started.add(queryClient);
        return;
      }
      started.add(queryClient);
      // Claim the touch before the request leaves, so a surface mounting in the same tick (the
      // sidebar summary) sees it through lastTouchAt and sends nothing.
      claimTouch(queryClient);
      // A request that never left (the socket was already closed) lets the next connection of the
      // same app start send the one request; any other failure does not retry.
      mutate(undefined, {
        onError: (err) => {
          if (err instanceof ConnectionClosedError) started.delete(queryClient);
        },
      });
    };
    if (windowIsActive()) {
      send();
      return;
    }
    const onChange = () => {
      if (!windowIsActive()) return;
      document.removeEventListener("visibilitychange", onChange);
      window.removeEventListener("focus", onChange);
      send();
    };
    document.addEventListener("visibilitychange", onChange);
    window.addEventListener("focus", onChange);
    return () => {
      document.removeEventListener("visibilitychange", onChange);
      window.removeEventListener("focus", onChange);
    };
    // `mutate` is stable per observer; the effect is keyed on the connection only.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [client, queryClient]);
}
