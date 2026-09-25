import { useEffect, useMemo, useRef } from "react";

import type { DaemonNotification, WsClient } from "@/lib/ws-client";
import { LIFECYCLE_TOPICS } from "@/lib/workspace-tree";

/** ADR 0014's three agent-profile lifecycle topics -- not part of LIFECYCLE_TOPICS (workspace-tree.ts), since a profile isn't part of the workspace/space/task tree that constant describes. */
const PROFILE_TOPICS = ["profile.created", "profile.updated", "profile.deleted"];

/**
 * The topics the UI subscribes to -- ADR 0005's initial three, ADR 0009's
 * eight workspace/space/task lifecycle topics, and ADR 0014's three
 * profile.* topics -- in one events.subscribe per connection, covering
 * every consumer.
 *
 * `event.dropped` is deliberately not here: the daemon's per-connection
 * pump synthesises it on queue overflow rather than publishing it, and
 * `knownTopics` (internal/wsapi/events.go) would reject it as unknown.
 * It still arrives as an ordinary notification, so it dispatches to
 * `subscribe("event.dropped", ...)` listeners like any other topic.
 */
const TOPICS = ["task.status", "run.status", "permission.pending", ...LIFECYCLE_TOPICS, ...PROFILE_TOPICS];

/** Registration surface returned by useDaemonEvents -- topic-filtered, with per-listener try/catch isolation. */
export interface DaemonEvents {
  subscribe(topic: string, listener: (payload: unknown) => void): () => void;
}

/**
 * Owns the app's single events.subscribe per connection (ADR 0005): on
 * every client change (initial connect and each reconnect -- delivery is
 * live-only, so a fresh connection must re-subscribe) it issues ONE
 * events.subscribe for all three topics, whatever the number of
 * consumers. Consumers register topic listeners on the returned stable
 * surface instead of touching the client's onNotification directly, so
 * two components listening to run.status never cause a second daemon
 * subscription, and unregistering on unmount is purely local.
 *
 * Returned from App.tsx once and passed down as a prop -- the stream
 * follows the client's lifecycle by construction (a reconnect swaps the
 * client, this effect re-runs, listeners re-register via their own
 * client-keyed effects upstream).
 */
export function useDaemonEvents(client: WsClient | null): DaemonEvents | null {
  const listenersRef = useRef(new Map<string, Set<(payload: unknown) => void>>());

  useEffect(() => {
    if (!client) return;
    // Fire-and-forget: a failed subscribe just means no live events this
    // connection -- consumers' own refetch-on-client-change remains the
    // source of truth, so this must never surface as an error state.
    client.call("events.subscribe", { topics: TOPICS }).catch(() => {});

    const off = client.onNotification((n: DaemonNotification) => {
      const set = listenersRef.current.get(n.topic);
      if (!set) return;
      for (const fn of set) {
        try {
          fn(n.payload);
        } catch (err) {
          console.error("useDaemonEvents: listener threw", err);
        }
      }
    });

    return () => {
      off();
    };
  }, [client]);

  return useMemo(
    () =>
      client
        ? {
            subscribe(topic: string, listener: (payload: unknown) => void): () => void {
              let set = listenersRef.current.get(topic);
              if (!set) {
                set = new Set();
                listenersRef.current.set(topic, set);
              }
              set.add(listener);
              return () => {
                set!.delete(listener);
              };
            },
          }
        : null,
    [client],
  );
}
