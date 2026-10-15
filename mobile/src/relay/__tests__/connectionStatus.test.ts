import { describe, expect, it } from 'vitest';
import { describeState } from '../connectionStatus';

describe('describeState', () => {
  it('names the daemon command to approve a pending device', () => {
    const v = describeState({ kind: 'pending', deviceId: 'ugarfmfxwqrf74yfe7qppt32dy', label: 'Pixel 9' })!;
    expect(v.tone).toBe('warning');
    expect(v.title).toBe('Waiting for approval');
    expect(v.command).toBe('smind relay devices approve ugarfmfxwqrf74yfe7qppt32dy');
    expect(v.detail).toContain('Pixel 9');
  });

  it('says a revoked device was revoked and how to get back', () => {
    const v = describeState({ kind: 'revoked', deviceId: 'x' })!;
    expect(v.tone).toBe('danger');
    expect(v.title).toBe('This device was revoked');
    expect(v.detail).toContain('Reset');
    expect(v.command).toBeUndefined();
  });

  it('shows failures and reconnects, and nothing for connecting/connected/closed', () => {
    expect(describeState({ kind: 'failed', message: 'boom' })).toMatchObject({ tone: 'danger', detail: 'boom' });
    expect(describeState({ kind: 'reconnecting', attempt: 2, reason: 'closed' })).toMatchObject({ tone: 'info', title: 'Reconnecting…' });
    expect(describeState({ kind: 'connecting' })).toBeNull();
    expect(describeState({ kind: 'connected' })).toBeNull();
    expect(describeState({ kind: 'closed' })).toBeNull();
  });
});
