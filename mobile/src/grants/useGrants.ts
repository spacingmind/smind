// useGrants: the Saved grants screen's resource -- permission.grants.list on mount, after every reconnect (the
// connection epoch) and on reload; revoke() then reloads. No topic announces a grant, so nothing streams.

import { useCallback, useEffect, useState } from 'react';
import { RelayConnection } from '../relay/RelayConnection';
import { useConnectionState } from '../relay/useConnectionState';
import { fetchGrants, revokeGrant } from './api';
import { SavedGrant } from './types';

export type GrantsState = { kind: 'loading' } | { kind: 'ready'; grants: SavedGrant[] } | { kind: 'error'; message: string };

export function useGrants(conn: RelayConnection | null): { state: GrantsState; reload: () => void; revoke: (id: number) => Promise<void> } {
  const [state, setState] = useState<GrantsState>({ kind: 'loading' });
  const { epoch } = useConnectionState(conn);
  const [tick, setTick] = useState(0);
  const reload = useCallback(() => setTick((t) => t + 1), []);

  useEffect(() => {
    if (!conn) return;
    let alive = true;
    fetchGrants(conn)
      .then((grants) => {
        if (alive) setState({ kind: 'ready', grants });
      })
      .catch((e) => {
        if (alive) setState({ kind: 'error', message: e instanceof Error ? e.message : String(e) });
      });
    return () => {
      alive = false;
    };
  }, [conn, epoch, tick]);

  const revoke = useCallback(
    async (id: number) => {
      if (!conn) throw new Error('not connected');
      try {
        await revokeGrant(conn, id);
      } finally {
        reload();
      }
    },
    [conn, reload],
  );
  return { state, reload, revoke };
}
