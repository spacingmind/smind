// useConnectionState subscribes a component to a RelayConnection's state and
// epoch (how many times it has become connected). Screens put the epoch in the
// dependency list of their data loading, so what a drop interrupted is loaded
// again once the connection is back.

import { useEffect, useState } from 'react';
import { ConnectionState, RelayConnection } from './RelayConnection';

export function useConnectionState(conn: RelayConnection | null): { state: ConnectionState; epoch: number } {
  const [, force] = useState(0);
  useEffect(() => {
    if (!conn) return;
    const off = conn.onState(() => force((n) => n + 1));
    force((n) => n + 1); // a change between render and subscribe
    return off;
  }, [conn]);
  return { state: conn ? conn.state : { kind: 'closed' }, epoch: conn ? conn.epoch : 0 };
}
