// Mounted once in the root layout (ADR-0040 section 5). Renders nothing. Wires
// the foreground rule, notification taps (warm and cold start) and push-token
// rotation. Does nothing unless this build can receive push at all, so Expo Go,
// web and iOS never touch expo-notifications here. Never logs: tokens and
// notification content are secrets.

import { useRouter, usePathname } from 'expo-router';
import * as Notifications from 'expo-notifications';
import { useEffect, useRef, useState } from 'react';
import { AppState } from 'react-native';
import { useConnectionOrNull } from '../../connection/ConnectionProvider';
import { routeGate } from '../../navigation/gate';
import { pushAvailability } from '../availability';
import { foregroundBehavior } from '../foregroundBehavior';
import { openTaskIdFromPath } from '../path';
import { reregisterToken } from '../register';
import { PendingTarget, releaseTarget, resolveNotificationTarget, targetHref } from '../target';
import { onPushTokenChange, platformPushEnv } from './adapters';
import { buildRegisterDeps } from './registerDeps';

const AVAILABLE = pushAvailability(platformPushEnv()) === 'available';

export function PushController() {
  const ctx = useConnectionOrNull();
  const router = useRouter();
  const pathname = usePathname();
  const conn = ctx?.conn ?? null;
  const connected = ctx?.state.kind === 'connected';
  const gate = routeGate(conn !== null, ctx?.state ?? null);

  // Live inputs read by the notification handler at call time, never closed over.
  const live = useRef({ appActive: AppState.currentState === 'active', connected: false, openTaskId: null as number | null });
  live.current.connected = connected;
  live.current.openTaskId = openTaskIdFromPath(pathname);

  const pending = useRef(new PendingTarget());
  const [taps, setTaps] = useState(0);
  const connRef = useRef(conn);
  connRef.current = conn;

  useEffect(() => {
    if (!AVAILABLE) return;
    const sub = AppState.addEventListener('change', (s) => {
      live.current.appActive = s === 'active';
    });
    return () => sub.remove();
  }, []);

  useEffect(() => {
    if (!AVAILABLE) return;
    Notifications.setNotificationHandler({
      handleNotification: async (n) =>
        foregroundBehavior({
          ...live.current,
          eventTaskId: resolveNotificationTarget(n.request.content.data)?.taskId ?? null,
        }),
    });
    return () => Notifications.setNotificationHandler(null);
  }, []);

  useEffect(() => {
    if (!AVAILABLE) return;
    const take = (response: Notifications.NotificationResponse | null): boolean => {
      if (response === null || response.actionIdentifier !== Notifications.DEFAULT_ACTION_IDENTIFIER) return false;
      const target = resolveNotificationTarget(response.notification.request.content.data);
      if (target === null) return false;
      pending.current.set(target);
      setTaps((n) => n + 1);
      return true;
    };
    let alive = true;
    const sub = Notifications.addNotificationResponseReceivedListener((r) => {
      take(r);
    });
    // Cold start: the tap that launched the app arrives before any listener existed.
    void Notifications.getLastNotificationResponseAsync()
      .then((r) => {
        if (!alive) return;
        take(r);
        if (r !== null) void Notifications.clearLastNotificationResponseAsync().catch(() => {});
      })
      .catch(() => {});
    return () => {
      alive = false;
      sub.remove();
    };
  }, []);

  // Release a pending target exactly once, when the app is reachable.
  useEffect(() => {
    if (!AVAILABLE) return;
    const t = releaseTarget(gate, pending.current);
    if (t !== null) router.push(targetHref(t));
  }, [gate, taps, router]);

  useEffect(() => {
    if (!AVAILABLE) return;
    return onPushTokenChange(() => {
      const c = connRef.current;
      if (c === null) return;
      void buildRegisterDeps(c)
        .then((deps) => reregisterToken(deps))
        .catch(() => {});
    });
  }, []);

  return null;
}
