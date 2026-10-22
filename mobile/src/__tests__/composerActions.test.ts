import { describe, expect, it } from 'vitest';
import { composerActions } from '../composerActions';

describe('composerActions', () => {
  it('idle, no text: only a disabled Send', () => {
    const a = composerActions({ liveRunId: null, canSend: false, stopping: false });
    expect(a.sendNow.visible).toBe(false);
    expect(a.stop.visible).toBe(false);
    expect(a.send.disabled).toBe(true);
  });
  it('idle, text: Send enabled, nothing else', () => {
    const a = composerActions({ liveRunId: null, canSend: true, stopping: false });
    expect(a.sendNow.visible).toBe(false);
    expect(a.stop.visible).toBe(false);
    expect(a.send.disabled).toBe(false);
  });
  it('live, no text: Stop visible, Send disabled, no Send now', () => {
    const a = composerActions({ liveRunId: 'r1', canSend: false, stopping: false });
    expect(a.stop).toEqual({ visible: true, label: 'Stop', disabled: false });
    expect(a.sendNow.visible).toBe(false);
    expect(a.send.disabled).toBe(true);
  });
  it('live, text: Stop, Send now and Send all available', () => {
    const a = composerActions({ liveRunId: 'r1', canSend: true, stopping: false });
    expect(a.stop).toEqual({ visible: true, label: 'Stop', disabled: false });
    expect(a.sendNow.visible).toBe(true);
    expect(a.send.disabled).toBe(false);
  });
  it('stopping: Stopping… disabled, Send now still follows canSend', () => {
    expect(composerActions({ liveRunId: 'r1', canSend: true, stopping: true }).stop).toEqual({ visible: true, label: 'Stopping…', disabled: true });
    expect(composerActions({ liveRunId: 'r1', canSend: true, stopping: true }).sendNow.visible).toBe(true);
    expect(composerActions({ liveRunId: 'r1', canSend: false, stopping: true }).sendNow.visible).toBe(false);
  });
  it('never shows Send now when not live', () => {
    for (const canSend of [true, false]) for (const stopping of [true, false]) expect(composerActions({ liveRunId: null, canSend, stopping }).sendNow.visible).toBe(false);
  });
});
