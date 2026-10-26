// useAutomations: an automation read kept fresh. Loads on mount, again after every reconnect (the connection epoch)
// and on every automation.* event; the last good value stays on screen during a refetch (the usePluginResource shape).

import { useCallback, useEffect, useState } from 'react';
import { RelayConnection } from '../relay/RelayConnection';
import { useConnectionState } from '../relay/useConnectionState';
import { fetchAutomations } from './api';
import { watchAutomationEvents } from './subscription';
import { AutomationListing } from './types';

export type AutomationResource<T> = { kind: 'loading' } | { kind: 'ready'; value: T } | { kind: 'error'; message: string };

export function useAutomationResource<T>(conn: RelayConnection | null, load: (conn: RelayConnection) => Promise<T>): { state: AutomationResource<T>; reload: () => void } {
  const [state, setState] = useState<AutomationResource<T>>({ kind: 'loading' });
  const { epoch } = useConnectionState(conn);
  const [tick, setTick] = useState(0);
  const reload = useCallback(() => setTick((t) => t + 1), []);

  useEffect(() => {
    if (!conn) return;
    return watchAutomationEvents(conn, reload);
  }, [conn, reload]);

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

/** The list screen's read: automation.list, refetched on automation.*. */
export function useAutomations(conn: RelayConnection | null) {
  return useAutomationResource<AutomationListing>(conn, fetchAutomations);
}
