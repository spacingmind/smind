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

  useEffect(() => {
    return () => {
      for (const t of timers.current) clearTimeout(t);
      timers.current.clear();
    };
  }, []);

  const fetch = useCallback(() => {
    if (!client || chatId === null) return;
    let cancelled = false;
    client
      .call<{ items: ChatQueueItem[] }>("chat.queueList", { chatId })
      .then((result) => {
        if (!cancelled) setAll(result.items ?? []);
      })
      .catch(() => {});
    // A chat switch (this effect re-running) must also drop this fetch's
    // cleanup -- guard via the closure, mirroring use-run-timeline's
    // session pattern without a second ref.
    return () => {
      cancelled = true;
    };
  }, [client, chatId]);

  useEffect(() => {
    setAll([]);
    autoCancelledAt.current.clear();
    if (!client || chatId === null) return;

    const offFetch = fetch();
    if (!events) return offFetch;
    const offQueue = events.subscribe("chat.queueUpdated", (payload) => {
      const p = payload as { chatId?: number; items?: ChatQueueItem[] };
      if (p.chatId !== chatId) return;
      setAll(p.items ?? []);
    });
    const offDropped = events.subscribe("event.dropped", () => {
      fetch();
    });
    return () => {
      offFetch?.();
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
