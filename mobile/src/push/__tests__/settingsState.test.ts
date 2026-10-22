import { describe, expect, it } from 'vitest';
import { describeRow, stateFromDaemonError, stateFromEnableResult, type RowState } from '../settingsState';

describe('settingsState', () => {
  it('describeRow covers every state', () => {
    const none = { action: null, actionLabel: null };
    const table: [RowState, object][] = [
      [{ kind: 'checking' }, { value: '…', hint: null, ...none }],
      [{ kind: 'unavailable', reason: 'web' }, { value: 'Not available on web', hint: null, ...none }],
      [
        { kind: 'unavailable', reason: 'expo_go' },
        { value: 'Needs a development build', hint: 'Expo Go cannot receive push notifications', ...none },
      ],
      [
        { kind: 'unavailable', reason: 'ios_blocked' },
        { value: 'Not available on iOS yet', hint: 'Needs the Apple Developer Program', ...none },
      ],
      [{ kind: 'unavailable', reason: 'daemon_disabled' }, { value: 'Off on this daemon', hint: null, ...none }],
      [
        { kind: 'unavailable', reason: 'old_daemon' },
        { value: 'Update the daemon', hint: 'This daemon has no notification support', ...none },
      ],
      [{ kind: 'off' }, { value: 'Off', hint: null, action: 'enable', actionLabel: 'Turn on' }],
      [{ kind: 'working' }, { value: 'Working…', hint: null, ...none }],
      [{ kind: 'on' }, { value: 'On', hint: null, action: 'disable', actionLabel: 'Turn off' }],
      [
        { kind: 'denied' },
        { value: 'Blocked', hint: 'Allow notifications for smind in system settings', ...none },
      ],
      [{ kind: 'error', message: 'boom' }, { value: 'Could not turn on', hint: 'boom', action: 'enable', actionLabel: 'Try again' }],
      [{ kind: 'error' }, { value: 'Could not turn on', hint: null, action: 'enable', actionLabel: 'Try again' }],
    ];
    for (const [state, want] of table) expect(describeRow(state)).toEqual(want);
  });

  it('enable results map to row states', () => {
    expect(stateFromEnableResult({ ok: true, kinds: ['done'] })).toEqual({ kind: 'on' });
    expect(stateFromEnableResult({ ok: false, reason: 'permission_denied' })).toEqual({ kind: 'denied' });
    const noToken = stateFromEnableResult({ ok: false, reason: 'no_token' });
    expect(noToken).toEqual({
      kind: 'error',
      message: 'No push token. Is Google Play services available and is the build configured for push?',
    });
    expect(stateFromEnableResult({ ok: false, reason: 'rpc_failed', message: 'x' })).toEqual({ kind: 'error', message: 'x' });
    expect(stateFromEnableResult({ ok: false, reason: 'rpc_failed' })).toEqual({ kind: 'error' });
  });

  it('daemon errors map to row states', () => {
    const withCode = (message: string, code?: string) => Object.assign(new Error(message), { code });
    expect(stateFromDaemonError(withCode('x', 'push_disabled'))).toEqual({ kind: 'unavailable', reason: 'daemon_disabled' });
    expect(stateFromDaemonError(withCode('x', 'forbidden'))).toEqual({ kind: 'unavailable', reason: 'old_daemon' });
    expect(stateFromDaemonError(new Error('Method Not Found: push.status'))).toEqual({ kind: 'unavailable', reason: 'old_daemon' });
    expect(stateFromDaemonError(new Error('unknown method push.status'))).toEqual({ kind: 'unavailable', reason: 'old_daemon' });
    expect(stateFromDaemonError(new Error('boom'))).toEqual({ kind: 'error', message: 'boom' });
    expect(stateFromDaemonError('weird')).toEqual({ kind: 'error' });
  });
});
