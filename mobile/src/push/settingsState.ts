// The Notifications row's state machine and texts (ADR-0040 section 6). Pure.

import type { EnableResult } from './register';

export type RowState =
  | { kind: 'checking' }
  | { kind: 'unavailable'; reason: 'web' | 'expo_go' | 'ios_blocked' | 'daemon_disabled' | 'old_daemon' }
  | { kind: 'off' }
  | { kind: 'working' }
  | { kind: 'on' }
  | { kind: 'denied' }
  | { kind: 'error'; message?: string };

export interface RowView {
  value: string;
  action: 'enable' | 'disable' | null;
  actionLabel: string | null;
  hint: string | null;
}

const NO_ACTION = { action: null, actionLabel: null } as const;

export function describeRow(s: RowState): RowView {
  switch (s.kind) {
    case 'checking':
      return { value: '…', ...NO_ACTION, hint: null };
    case 'unavailable':
      switch (s.reason) {
        case 'web':
          return { value: 'Not available on web', ...NO_ACTION, hint: null };
        case 'expo_go':
          return { value: 'Needs a development build', ...NO_ACTION, hint: 'Expo Go cannot receive push notifications' };
        case 'ios_blocked':
          return { value: 'Not available on iOS yet', ...NO_ACTION, hint: 'Needs the Apple Developer Program' };
        case 'daemon_disabled':
          return { value: 'Off on this daemon', ...NO_ACTION, hint: null };
        case 'old_daemon':
          return { value: 'Update the daemon', ...NO_ACTION, hint: 'This daemon has no notification support' };
      }
      return { value: '', ...NO_ACTION, hint: null };
    case 'off':
      return { value: 'Off', action: 'enable', actionLabel: 'Turn on', hint: null };
    case 'working':
      return { value: 'Working…', ...NO_ACTION, hint: null };
    case 'on':
      return { value: 'On', action: 'disable', actionLabel: 'Turn off', hint: null };
    case 'denied':
      return { value: 'Blocked', ...NO_ACTION, hint: 'Allow notifications for smind in system settings' };
    case 'error':
      return { value: 'Could not turn on', action: 'enable', actionLabel: 'Try again', hint: s.message ?? null };
  }
}

export function stateFromEnableResult(r: EnableResult): RowState {
  if (r.ok) return { kind: 'on' };
  switch (r.reason) {
    case 'permission_denied':
      return { kind: 'denied' };
    case 'no_token':
      return {
        kind: 'error',
        message: 'No push token. Is Google Play services available and is the build configured for push?',
      };
    case 'rpc_failed':
      return r.message === undefined ? { kind: 'error' } : { kind: 'error', message: r.message };
  }
}

export function stateFromDaemonError(e: unknown): RowState {
  const code = (e as { code?: unknown } | null | undefined)?.code;
  const message = e instanceof Error ? e.message : undefined;
  if (code === 'push_disabled') return { kind: 'unavailable', reason: 'daemon_disabled' };
  const lower = message?.toLowerCase() ?? '';
  if (code === 'forbidden' || lower.includes('method not found') || lower.includes('unknown method')) {
    return { kind: 'unavailable', reason: 'old_daemon' };
  }
  return message === undefined ? { kind: 'error' } : { kind: 'error', message };
}
