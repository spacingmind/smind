// keepalive.test.ts pins ADR-0031 D9's numbers (probe every 30 s, a stream
// the relay has been silent on for 75 s is dead) and the nonce-echo detection
// to the mirror of internal/relay/client/keepalive.go's behaviour, on fake
// timers.

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { DEFAULT_KEEPALIVE, Keepalive, randomProbeNonce } from '../keepalive';
import { Frame } from '../proto';

function frame(over: Partial<Frame>): Frame {
  return { workspaceId: '', sessionId: new Uint8Array(0), deviceId: '', direction: 1, sequence: 0, payload: new Uint8Array(0), keepalive: false, ...over };
}

beforeEach(() => {
  vi.useFakeTimers();
});
afterEach(() => {
  vi.useRealTimers();
});

describe('keepalive defaults (ADR-0031 D9, flow 8)', () => {
  it('is 30 s / 75 s / 10 s', () => {
    expect(DEFAULT_KEEPALIVE).toEqual({ intervalMs: 30_000, deadAfterMs: 75_000, probeTimeoutMs: 10_000 });
  });

  it('probes at once and then every 30 s while the relay echoes, and never declares a talking stream dead', async () => {
    const probes: number[] = [];
    let dead = false;
    const ka = new Keepalive(DEFAULT_KEEPALIVE, (n) => probes.push(n), () => (dead = true), 4242);
    expect(probes).toEqual([4242]); // first probe immediately

    // The relay echoes each probe a moment later.
    for (let i = 0; i < 6; i++) {
      await vi.advanceTimersByTimeAsync(1_000);
      ka.noteFrame(frame({ keepalive: true, sequence: 4242 }));
      await vi.advanceTimersByTimeAsync(29_000);
    }
    expect(probes.length).toBe(7); // t=0 and one per 30 s
    expect(ka.supported).toBe(true);
    expect(dead).toBe(false);
    ka.stop();
  });

  it('declares the stream dead 75 s after the last frame once the relay has proven it echoes', async () => {
    let dead = 0;
    const ka = new Keepalive(DEFAULT_KEEPALIVE, () => {}, () => dead++, 7);
    await vi.advanceTimersByTimeAsync(500);
    ka.noteFrame(frame({ keepalive: true, sequence: 7 })); // echo at t=0.5 s => supported, lastRecv = 0.5 s
    await vi.advanceTimersByTimeAsync(74_000);
    expect(dead).toBe(0);
    await vi.advanceTimersByTimeAsync(1_000); // t = 75.5 s
    expect(dead).toBe(1);
    await vi.advanceTimersByTimeAsync(120_000);
    expect(dead).toBe(1); // reported once
  });

  it('any frame, not only an echo, counts as liveness', async () => {
    let dead = 0;
    const ka = new Keepalive(DEFAULT_KEEPALIVE, () => {}, () => dead++, 7);
    ka.noteFrame(frame({ keepalive: true, sequence: 7 }));
    for (let i = 0; i < 5; i++) {
      await vi.advanceTimersByTimeAsync(60_000);
      ka.noteFrame(frame({ payload: new Uint8Array([1]) })); // application data
    }
    expect(dead).toBe(0);
    ka.stop();
  });

  it('a keepalive frame with another nonce proves liveness but not support (a relay without keepalive forwarded the peer\'s probe)', async () => {
    let dead = 0;
    const ka = new Keepalive(DEFAULT_KEEPALIVE, () => {}, () => dead++, 7);
    ka.noteFrame(frame({ keepalive: true, sequence: 99 }));
    expect(ka.supported).toBe(false);
    await vi.advanceTimersByTimeAsync(10_000); // probe timeout: keepalive switches itself off
    await vi.advanceTimersByTimeAsync(300_000);
    expect(dead).toBe(0); // never killed on a relay that does not echo
  });

  it('stops probing after stop()', async () => {
    const probes: number[] = [];
    const ka = new Keepalive(DEFAULT_KEEPALIVE, (n) => probes.push(n), () => {}, 5);
    ka.stop();
    await vi.advanceTimersByTimeAsync(200_000);
    expect(probes).toEqual([5]);
  });
});

describe('randomProbeNonce', () => {
  it('is a non-zero integer below 2^52 and not constant', () => {
    const seen = new Set<number>();
    for (let i = 0; i < 50; i++) {
      const n = randomProbeNonce();
      expect(Number.isInteger(n)).toBe(true);
      expect(n).toBeGreaterThan(0);
      expect(n).toBeLessThan(2 ** 52);
      seen.add(n);
    }
    expect(seen.size).toBeGreaterThan(40);
  });
});
