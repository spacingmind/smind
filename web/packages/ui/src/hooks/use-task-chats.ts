import { useCallback, useEffect, useRef, useState } from "react";

import type { DaemonEvents } from "@/hooks/use-daemon-events";
import type { WsClientLike } from "@/lib/ws-client";
import type { Chat, ChatArchivedEventPayload, ChatCreatedEventPayload, ChatUpdatedEventPayload } from "@/lib/types";

export interface UseTaskChatsResult {
  /** taskId's active (non-archived) chats, oldest -- the default chat -- first. null while the initial chat.list fetch is in flight. */
  chats: Chat[] | null;
  /** Re-fetches chat.list -- best-effort recovery after an event.dropped, mirroring use-task-attention.ts's own resync-on-drop pattern. */
  refresh: () => void;
}

/**
 * Loads taskId's active chats and keeps them live via ADR-0016's
 * chat.created/updated/archived events -- the source of both the "+" menu's
 * reopen-existing-chat section and the tab-title-sync path (App.tsx's
 * chat.updated subscriber calls tabs.updateChatTabTitle with what this hook
 * already decoded). Switching taskId (or a client change) refetches from
 * scratch, guarded by a per-selection cancelled flag so a stale fetch from
 * a superseded taskId can never clobber the current selection's list --
 * same pattern useRunTimeline.ts's Session already establishes.
 *
 * onChatUpdated fires for every chat.updated this task's list observes
 * (including ones already archived-and-filtered-out below) -- App.tsx uses
 * it to keep an open chat tab's title in sync with a rename that happened
 * elsewhere, which needs to happen regardless of archived state.
 */
export function useTaskChats(
  client: WsClientLike | null,
  taskId: number | null,
  events: DaemonEvents | null,
  onChatUpdated?: (chat: Chat) => void,
): UseTaskChatsResult {
  const [chats, setChats] = useState<Chat[] | null>(null);
  const onChatUpdatedRef = useRef(onChatUpdated);
  onChatUpdatedRef.current = onChatUpdated;
  // The current (client, taskId) selection's "am I still current" flag --
  // a fresh object per selection (mirroring use-run-timeline.ts's Session),
  // not a single shared ref: a shared ref reset to false by a *new*
  // selection's effect run would also (wrongly) un-cancel an older,
  // still-in-flight fetch that happens to resolve afterwards.
  const selectionRef = useRef<{ cancelled: boolean } | null>(null);

  const fetchChats = useCallback(() => {
    if (!client || taskId === null) {
      selectionRef.current = null;
      setChats(null);
      return;
    }
    const selection = { cancelled: false };
    selectionRef.current = selection;
    client
      .call<Chat[]>("chat.list", { taskId })
      .then((result) => {
        if (selection.cancelled) return;
        setChats((result ?? []).slice().sort((a, b) => a.ID - b.ID));
      })
      .catch(() => {
        if (!selection.cancelled) setChats([]);
      });
  }, [client, taskId]);

  useEffect(() => {
    // Reset synchronously on every (client, taskId) change, before the new
    // fetch resolves -- serving the *previous* taskId's chats while a new
    // one is selected is actively wrong, not just stale: App.tsx's
    // ensureTaskChats effect below fires as soon as `chats` looks non-null
    // and would seed the newly selected task's default-chat tab with the
    // previous task's chat (its id, right into a same-render race), a real
    // cross-task chat-identity mixup, not merely a flicker.
    setChats(null);
    fetchChats();
    return () => {
      if (selectionRef.current) selectionRef.current.cancelled = true;
    };
  }, [fetchChats]);

  useEffect(() => {
    if (!events || taskId === null) return;

    const offCreated = events.subscribe("chat.created", (payload) => {
      const p = payload as ChatCreatedEventPayload;
      if (!p?.chat || p.chat.TaskID !== taskId) return;
      setChats((prev) => {
        const list = prev ?? [];
        if (list.some((c) => c.ID === p.chat.ID)) return list;
        return [...list, p.chat].sort((a, b) => a.ID - b.ID);
      });
    });

    const offUpdated = events.subscribe("chat.updated", (payload) => {
      const p = payload as ChatUpdatedEventPayload;
      if (!p?.chat || p.chat.TaskID !== taskId) return;
      onChatUpdatedRef.current?.(p.chat);
      setChats((prev) => (prev ? prev.map((c) => (c.ID === p.chat.ID ? p.chat : c)) : prev));
    });

    const offArchived = events.subscribe("chat.archived", (payload) => {
      const p = payload as ChatArchivedEventPayload;
      if (!p?.chat || p.chat.TaskID !== taskId) return;
      setChats((prev) => (prev ? prev.filter((c) => c.ID !== p.chat.ID) : prev));
    });

    const offDropped = events.subscribe("event.dropped", fetchChats);

    return () => {
      offCreated();
      offUpdated();
      offArchived();
      offDropped();
    };
  }, [events, taskId, fetchChats]);

  return { chats, refresh: fetchChats };
}
