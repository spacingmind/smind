// State of the Notifications row. No permission prompt happens until enable().
// Never logs.

import { useCallback, useEffect, useRef, useState } from 'react';
import { pushAvailability } from '../availability';
import { disablePush, enablePush, reregisterToken, type RegisterDeps } from '../register';
import {
  describeRow,
  stateFromDaemonError,
  stateFromEnableResult,
  type RowState,
  type RowView,
} from '../settingsState';
import type { RelayConnection } from '../../relay/RelayConnection';
import { platformPushEnv } from './adapters';
import { buildRegisterDeps } from './registerDeps';

export function usePushSettings(conn: RelayConnection | null): {
  view: RowView;
  state: RowState;
  enable(): Promise<void>;
  disable(): Promise<void>;
} {
  const [state, setState] = useState<RowState>({ kind: 'checking' });
  const depsRef = useRef<RegisterDeps | null>(null);
  // Bumped on every conn change and unmount; an older effect or action must not write state.
  const epoch = useRef(0);

  useEffect(() => {
    const mine = ++epoch.current;
    const live = () => epoch.current === mine;
    depsRef.current = null;

    const availability = pushAvailability(platformPushEnv());
    if (availability !== 'available') {
      setState({ kind: 'unavailable', reason: availability });
      return () => {
        epoch.current++;
      };
    }
    if (conn === null) {
      setState({ kind: 'checking' });
      return () => {
        epoch.current++;
      };
    }
    setState({ kind: 'checking' });
    void (async () => {
      let next: RowState;
      try {
        const deps = await buildRegisterDeps(conn);
        if (!live()) return;
        depsRef.current = deps;
        if ((await deps.keys.get(deps.deviceId)) === null) {
          next = { kind: 'off' };
        } else {
          const status = (await conn.call('push.status', {})) as { registered?: unknown } | null;
          if (status?.registered === true) {
            next = { kind: 'on' };
          } else {
            const r = await reregisterToken(deps);
            next = r === null ? { kind: 'off' } : stateFromEnableResult(r);
          }
        }
      } catch (e) {
        next = stateFromDaemonError(e);
      }
      if (live()) setState(next);
    })();
    return () => {
      epoch.current++;
    };
  }, [conn]);

  const enable = useCallback(async () => {
    const mine = epoch.current;
    const deps = depsRef.current;
    if (deps === null) return;
    setState({ kind: 'working' });
    let next: RowState;
    try {
      next = stateFromEnableResult(await enablePush(deps));
    } catch (e) {
      next = stateFromDaemonError(e);
    }
    if (epoch.current === mine) setState(next);
  }, []);

  const disable = useCallback(async () => {
    const mine = epoch.current;
    const deps = depsRef.current;
    if (deps === null) return;
    setState({ kind: 'working' });
    let next: RowState = { kind: 'off' };
    try {
      await disablePush(deps);
    } catch (e) {
      next = stateFromDaemonError(e);
    }
    if (epoch.current === mine) setState(next);
  }, []);

  return { view: describeRow(state), state, enable, disable };
}
