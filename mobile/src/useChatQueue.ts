// useChatQueue binds one ChatQueue (chatQueue.ts, the pure store) to a
// component's lifecycle for the current chat: start/stop on mount and
// chat change, refetch after every reconnect (the connection-state epoch
// in the effect deps), and re-render through a bump counter since the
// store's onChange is the only mutation signal.

import { useCallback, useEffect, useRef, useState } from 'react';
import { ChatQueueItem } from './api';
import { ChatQueue } from './chatQueue';
import { RelayConnection } from './relay/RelayConnection';
import { useConnectionState } from './relay/useConnectionState';

export function useChatQueue(
  conn: RelayConnection,
  chatId: number | null,
): {
  items: ChatQueueItem[];
  cancel: (itemId: number) => Promise<void>;
  refresh: () => void;
  /** The prompt of the delivered queue item that became this run (the snapshot keeps delivered items), if known. */
  promptForRun: (runId: string) => string | undefined;
} {
  const [, bump] = useState(0);
  const ref = useRef<ChatQueue | null>(null);
  const { epoch } = useConnectionState(conn);
  useEffect(() => {
    if (chatId === null) {
      ref.current = null;
      bump((n) => n + 1);
      return;
    }
    const q = new ChatQueue(conn, chatId, () => bump((n) => n + 1));
    ref.current = q;
    q.start();
    bump((n) => n + 1);
    return () => {
      q.stop();
      if (ref.current === q) ref.current = null;
    };
  }, [conn, chatId, epoch]);
  const cancel = useCallback(
    (itemId: number): Promise<void> => ref.current?.cancel(itemId) ?? Promise.resolve(),
    [],
  );
  const refresh = useCallback((): void => {
    void ref.current?.refresh();
  }, []);
  const promptForRun = useCallback((runId: string): string | undefined => ref.current?.promptForRun(runId), []);
  return { items: ref.current?.items ?? [], cancel, refresh, promptForRun };
}
