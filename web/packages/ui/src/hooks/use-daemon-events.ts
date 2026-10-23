import { useEffect, useMemo, useRef } from "react";

import type { QueryClient } from "@tanstack/react-query";

import { applyDaemonEvent } from "@/lib/query/event-bridge";
import { queryKeys } from "@/lib/query-keys";
import type { DaemonNotification, WsClient } from "@/lib/ws-client";
import { LIFECYCLE_TOPICS } from "@/lib/workspace-tree";

/** ADR 0014's three agent-profile lifecycle topics -- not part of LIFECYCLE_TOPICS (workspace-tree.ts), since a profile isn't part of the workspace/space/task tree that constant describes. */
const PROFILE_TOPICS = ["profile.created", "profile.updated", "profile.deleted"];

/** ADR-0018's three MCP server registry lifecycle topics (Settings -> MCP servers reacts to these). */
const MCP_TOPICS = ["mcpServer.created", "mcpServer.updated", "mcpServer.deleted"];

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
/** ADR-0016's three chat lifecycle topics (P3: chat tabs, rename, archive all react to these). */
const CHAT_TOPICS = ["chat.created", "chat.updated", "chat.archived", "chat.queueUpdated"];

/** ADR-0015/0028's account topics: a created/renamed/re-credentialed account, a removal, and a fetched usage report. */
const ACCOUNT_TOPICS = ["account.updated", "account.usage", "account.removed"];

/**
 * ADR-0026's self-learning topics: a new proposal, any skill/proposal change, and a reflect/curate job's status;
 * ADR-0037's probe lifecycle (started, an arm changed, finished, failed).
 */
const LEARNING_TOPICS = [
  "learnedSkill.proposed",
  "learnedSkill.changed",
  "learning.job",
  "learnedSkill.probe.started",
  "learnedSkill.probe.arm",
  "learnedSkill.probe.finished",
  "learnedSkill.probe.failed",
];

/** ADR-0039's task git state: the daemon's recomputed `{taskId, git}` snapshot (the bridge patches the caches). */
const GIT_TOPICS = ["task.git", "forge.status"];

const TOPICS = [
  "task.status",
  "run.status",
  "permission.pending",
  ...LIFECYCLE_TOPICS,
  ...PROFILE_TOPICS,
  ...MCP_TOPICS,
  ...CHAT_TOPICS,
  ...ACCOUNT_TOPICS,
  ...LEARNING_TOPICS,
  ...GIT_TOPICS,
];

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
export function useDaemonEvents(client: WsClient | null, queryClient?: QueryClient): DaemonEvents | null {
  const listenersRef = useRef(new Map<string, Set<(payload: unknown) => void>>());
  const lastClient = useRef<WsClient | null>(null);

  useEffect(() => {
    if (!client) return;
    // A new client after an earlier one is a reconnect: the cache outlived the transport and missed
    // every event in the gap, so it is invalidated -- but only once the new subscription is in
    // place, or an event between the re-read and the subscribe would be lost for good.
    const reconnected = lastClient.current !== null && lastClient.current !== client;
    lastClient.current = client;
    // A failed subscribe just means no live events this connection; the invalidation still runs
    // (the re-read is the source of truth), and nothing here surfaces as an error state.
    client
      .call("events.subscribe", { topics: TOPICS })
      .catch(() => {})
      .then(() => {
        if (reconnected && queryClient) {
          // The daemon may have restarted with a fixed PATH or credential: its health answers are
          // re-tested (the footer's ensure() notices the gap); everything else is re-read.
          queryClient.removeQueries({ queryKey: queryKeys.providerTestAll() });
          void queryClient.invalidateQueries();
        }
      });

    const off = client.onNotification((n: DaemonNotification) => {
      // ADR-0032: this is the single event -> query-cache bridge; it runs before listeners so a
      // listener that reads the cache sees the event applied.
      if (queryClient) {
        try {
          applyDaemonEvent(queryClient, n.topic, n.payload);
        } catch (err) {
          console.error("useDaemonEvents: query bridge threw", err);
        }
      }
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
  }, [client, queryClient]);

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
