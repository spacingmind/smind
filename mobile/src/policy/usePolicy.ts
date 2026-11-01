// usePolicy: the policy screen's READ-ONLY resource -- policy.get and policy.audit (limit 50) loaded together
// on mount, again after every reconnect (the connection epoch) and on reload. The last good value stays on
// screen during a refetch (the usePluginResource discipline), minus any subscription: the policy has no
// update events, so nothing streams and only these two reads ever happen.

import { useCallback, useEffect, useState } from 'react';
import { RelayConnection } from '../relay/RelayConnection';
import { useConnectionState } from '../relay/useConnectionState';
import { fetchPolicy, fetchPolicyAudit } from './api';
import { PolicyAuditRow, PolicyStatus } from './types';

export type PolicyState =
  | { kind: 'loading' }
  | { kind: 'ready'; status: PolicyStatus; rows: PolicyAuditRow[] }
  | { kind: 'error'; message: string };

export function usePolicy(conn: RelayConnection | null): { state: PolicyState; reload: () => void } {
  const [state, setState] = useState<PolicyState>({ kind: 'loading' });
  const { epoch } = useConnectionState(conn);
  const [tick, setTick] = useState(0);
  const reload = useCallback(() => setTick((t) => t + 1), []);

  useEffect(() => {
    if (!conn) return;
    let alive = true;
    Promise.all([fetchPolicy(conn), fetchPolicyAudit(conn, 50)])
      .then(([status, rows]) => {
        if (alive) setState({ kind: 'ready', status, rows });
      })
      .catch((e) => {
        if (alive) setState({ kind: 'error', message: e instanceof Error ? e.message : String(e) });
      });
    return () => {
      alive = false;
    };
  }, [conn, epoch, tick]);

  return { state, reload };
}
