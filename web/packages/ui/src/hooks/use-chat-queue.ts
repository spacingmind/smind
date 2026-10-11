import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import type { DaemonEvents } from "@/hooks/use-daemon-events";
import type { ChatQueueItem } from "@/lib/types";
import type { WsClientLike } from "@/lib/ws-client";

/** How long an auto-cancelled item keeps showing its reason before dropping (ADR-0021 §9). */
const AUTO_CANCELLED_TTL_MS = 10_000;

/**
 * The daemon's per-chat prompt queue, one source of truth for the
 * composer (ADR-0021 §9): full-snapshot state from chat.queueList on
 * mount and every chat.queueUpdated afterwards, refetched on a new
 * client (reconnect) and on event.dropped. Only this chat's items are
 * ever stored -- a snapshot or late response for another chat is
 * ignored, so a chat switch can't flash the old queue.
 *
 * Cancel reason strings ("cancelled by user", the "delivery failed:"
 * family) are server-defined (internal/runs/queue.go) -- the coupling is
 * deliberate and unavoidable: the UI decides *visibility* (a user cancel
 * vanishes at once; an auto-cancel shows its reason briefly) from that
 * one field.
 */
export function useChatQueue(client: WsClientLike | null, events: DaemonEvents | null, chatId: number | null): {
  items: ChatQueueItem[];
  cancel: (itemId: number) => Promise<void>;
  refresh: () => void;
} {
  // The full latest snapshot (all statuses) -- visibility filtering is a
  // derived view below, so a delivered item's disappearance and an
  // auto-cancelled item's 10s grace period are both pure functions of
  // this plus time.
  const [all, setAll] = useState<ChatQueueItem[]>([]);
  const [tick, setTick] = useState(0);
  // First-seen timestamps for the auto-cancelled items currently in
  // their 10s grace window, plus the pending timers that end them.
  const autoCancelledAt = useRef(new Map<number, number>());
  const timers = useRef(new Set<ReturnType<typeof setTimeout>>());
  // Stale-response guards: the chat this hook currently serves, the
  // client it served it with, and how many live snapshots have arrived --
  // a queueList answer that predates a switch, a reconnect, or a newer
  // snapshot must never clobber the newer state.
  const chatIdRef = useRef<number | null>(chatId);
  const clientRef = useRef<WsClientLike | null>(client);
  const snapshotSeq = useRef(0);

  useEffect(() => {
    return () => {
      for (const t of timers.current) clearTimeout(t);
      timers.current.clear();
    };
  }, []);

  const fetch = useCallback(() => {
    if (!client || chatId === null) return;
    // Capture the world as it was when the request left: a late answer
    // from a superseded chat/client, or one a newer snapshot has already
    // overtaken, is dropped rather than applied.
    const reqChatId = chatId;
    const reqClient = client;
    const reqSeq = snapshotSeq.current;
    client
      .call<{ items: ChatQueueItem[] }>("chat.queueList", { chatId })
      .then((result) => {
        if (chatIdRef.current !== reqChatId || clientRef.current !== reqClient || snapshotSeq.current !== reqSeq) return;
        setAll(result.items ?? []);
      })
      .catch(() => {});
  }, [client, chatId]);

  useEffect(() => {
    chatIdRef.current = chatId;
    clientRef.current = client;
    setAll([]);
    autoCancelledAt.current.clear();
    if (!client || chatId === null) return;
    // An in-flight queueList from the previous selection is stale by
    // definition; advancing the sequence drops its response too.
    snapshotSeq.current += 1;

    fetch();
    if (!events) return;
    const offQueue = events.subscribe("chat.queueUpdated", (payload) => {
      const p = payload as { chatId?: number; items?: ChatQueueItem[] };
      if (p.chatId !== chatId) return;
      snapshotSeq.current += 1;
      setAll(p.items ?? []);
    });
    const offDropped = events.subscribe("event.dropped", () => {
      fetch();
    });
    return () => {
      offQueue();
      offDropped();
    };
  }, [client, events, chatId, fetch]);

  // Track the grace window: record an auto-cancelled item's first sight
  // and arm its one-shot timer; forget items no longer in the snapshot.
  useEffect(() => {
    const now = Date.now();
    const live = new Set<number>();
    for (const item of all) {
      if (item.Status !== "cancelled") continue;
      if (!item.CancelReason || item.CancelReason === "cancelled by user") continue;
      live.add(item.ID);
      if (!autoCancelledAt.current.has(item.ID)) {
        autoCancelledAt.current.set(item.ID, now);
        const t = setTimeout(() => {
          timers.current.delete(t);
          setTick((n) => n + 1);
        }, AUTO_CANCELLED_TTL_MS);
        timers.current.add(t);
      }
    }
    for (const id of autoCancelledAt.current.keys()) {
      if (!live.has(id)) autoCancelledAt.current.delete(id);
    }
  }, [all]);

  const items = useMemo(() => {
    void tick; // re-derive when a grace window's timer fires
    const queued = all
      .filter((item) => item.Status === "queued")
      .sort((a, b) => b.Priority - a.Priority || a.ID - b.ID);
    const cancelledVisible = all.filter((item) => {
      if (item.Status !== "cancelled") return false;
      if (!item.CancelReason || item.CancelReason === "cancelled by user") return false;
      // An item not yet in the map was first seen on this very render
      // (the effect below records it right after commit) -- visible.
      const seen = autoCancelledAt.current.get(item.ID);
      return seen === undefined || Date.now() - seen < AUTO_CANCELLED_TTL_MS;
    });
    return [...queued, ...cancelledVisible];
  }, [all, tick]);

  const cancel = useCallback(
    async (itemId: number) => {
      if (!client) return;
      try {
        await client.call("chat.queueCancel", { itemId });
      } catch {
        // Typically "not queued" -- the item was delivered a moment
        // before the click. The list is the truth: refetch and swallow.
        fetch();
      }
    },
    [client, fetch],
  );

  return { items, cancel, refresh: fetch };
}
