// useReview keeps one steward review (steward.get) fresh. Loads on mount, again after every reconnect
// (the connection epoch) and on every inbox.changed -- a steward update pokes the inbox, so that event
// is the review's change signal. The last good value stays on screen during a refetch.

import { useCallback, useEffect, useState } from 'react';
import { RelayConnection } from '../relay/RelayConnection';
import { useConnectionState } from '../relay/useConnectionState';
import { watchInboxChanged } from '../inbox/subscription';
import { fetchReview } from './api';
import { StewardReview } from './types';

export type ReviewState = { kind: 'loading' } | { kind: 'ready'; review: StewardReview } | { kind: 'error'; message: string };

export function useReview(conn: RelayConnection | null, reviewId: number): { state: ReviewState; reload: () => void } {
  const [state, setState] = useState<ReviewState>({ kind: 'loading' });
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
    fetchReview(conn, reviewId)
      .then((review) => {
        if (alive) setState({ kind: 'ready', review });
      })
      .catch((e) => {
        if (alive) setState({ kind: 'error', message: e instanceof Error ? e.message : String(e) });
      });
    return () => {
      alive = false;
    };
  }, [conn, reviewId, epoch, tick]);

  return { state, reload };
}
