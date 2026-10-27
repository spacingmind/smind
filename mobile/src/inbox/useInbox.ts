// useInbox: the inbox count (always) and list (only while `withList`, i.e. the screen is open), kept fresh. Both load
// on mount, after every reconnect (the connection epoch) and on every inbox.changed; the last good value stays on
// screen during a refetch. There is no optimistic state: a setter is followed by a refetch.

import { useCallback, useEffect, useState } from 'react';
import { RelayConnection } from '../relay/RelayConnection';
import { useConnectionState } from '../relay/useConnectionState';
import { fetchInbox, fetchInboxCount } from './api';
import { watchInboxChanged } from './subscription';
import { InboxCount, InboxItem } from './types';

export type InboxList = { kind: 'loading' } | { kind: 'ready'; items: InboxItem[] } | { kind: 'error'; message: string };

export function useInbox(conn: RelayConnection | null, withList: boolean): { count: InboxCount | null; list: InboxList; reload: () => void } {
  const [count, setCount] = useState<InboxCount | null>(null);
  const [list, setList] = useState<InboxList>({ kind: 'loading' });
  const { epoch } = useConnectionState(conn);
  const [tick, setTick] = useState(0);
  const reload = useCallback(() => setTick((t) => t + 1), []);

  useEffect(() => {
    if (!conn) return;
    return watchInboxChanged(conn, reload);
  }, [conn, reload]);

  useEffect(() => {
    if (!conn) return;
    let alive = true;
    fetchInboxCount(conn)
      .then((c) => {
        if (alive) setCount(c);
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [conn, epoch, tick]);

  useEffect(() => {
    if (!conn || !withList) return;
    let alive = true;
    fetchInbox(conn)
      .then((items) => {
        if (alive) setList({ kind: 'ready', items });
      })
      .catch((e) => {
        if (alive) setList({ kind: 'error', message: e instanceof Error ? e.message : String(e) });
      });
    return () => {
      alive = false;
    };
  }, [conn, withList, epoch, tick]);

  return { count, list, reload };
}
