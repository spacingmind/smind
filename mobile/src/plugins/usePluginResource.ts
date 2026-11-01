// usePluginResource: a plugin read (one of api.ts's four) kept fresh. Loads on mount, again after every reconnect
// (the connection epoch) and on every plugin.updated -- plus, through the optional watchExtra argument, any extra
// topic a caller wants treated the same way (a panel screen passes plugin.event). The last good value stays on
// screen during a refetch, so a list never blinks to a skeleton because a plugin changed on the computer.

import { useCallback, useEffect, useState } from 'react';
import { RelayConnection } from '../relay/RelayConnection';
import { useConnectionState } from '../relay/useConnectionState';
import { watchPluginUpdates } from './subscription';

export type PluginResource<T> = { kind: 'loading' } | { kind: 'ready'; value: T } | { kind: 'error'; message: string };

export type PluginExtraWatch = (conn: RelayConnection, reload: () => void) => () => void;

export function usePluginResource<T>(
  conn: RelayConnection | null,
  load: (conn: RelayConnection) => Promise<T>,
  watchExtra?: PluginExtraWatch,
): { state: PluginResource<T>; reload: () => void } {
  const [state, setState] = useState<PluginResource<T>>({ kind: 'loading' });
  const { epoch } = useConnectionState(conn);
  const [tick, setTick] = useState(0);
  const reload = useCallback(() => setTick((t) => t + 1), []);

  useEffect(() => {
    if (!conn) return;
    return watchPluginUpdates(conn, reload);
  }, [conn, reload]);

  useEffect(() => {
    if (!conn || !watchExtra) return;
    return watchExtra(conn, reload);
  }, [conn, reload, watchExtra]);

  useEffect(() => {
    if (!conn) return;
    let alive = true;
    load(conn)
      .then((value) => {
        if (alive) setState({ kind: 'ready', value });
      })
      .catch((e) => {
        if (alive) setState({ kind: 'error', message: e instanceof Error ? e.message : String(e) });
      });
    return () => {
      alive = false;
    };
  }, [conn, load, epoch, tick]);

  return { state, reload };
}
