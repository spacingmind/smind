import { chacha20poly1305 } from '@noble/ciphers/chacha.js';
import { readFileSync } from 'node:fs';
import path from 'node:path';
import { describe, expect, it } from 'vitest';
import { base64UrlEncode } from '../../relay/base64';
import { GENERIC_NOTIFICATION, notificationFor, openBlob, openWithAnyKey } from '../open';

const vector = JSON.parse(
  readFileSync(path.resolve(__dirname, '../../../../internal/push/testdata/push_seal_vector.json'), 'utf8'),
);

function hexToBytes(hex: string): Uint8Array {
  const out = new Uint8Array(hex.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(hex.slice(i * 2, i * 2 + 2), 16);
  return out;
}

const key = hexToBytes(vector.key_hex);
const deviceId: string = vector.device_id;
const wrong1 = new Uint8Array(32).fill(1);
const wrong2 = new Uint8Array(32).fill(2);

const ERRORS: Record<string, string> = { ErrAuth: 'auth', ErrVersion: 'version', ErrMalformed: 'malformed' };

function sealRaw(plain: Uint8Array) {
  const nonce = new Uint8Array(12).fill(7);
  const aad = new TextEncoder().encode('smind-push-v1' + deviceId);
  const ct = chacha20poly1305(key, nonce, aad).encrypt(plain);
  return { v: 1, n: base64UrlEncode(nonce), c: base64UrlEncode(ct) };
}

function framed(json: Uint8Array, mutate?: (b: Uint8Array) => void): Uint8Array {
  const b = new Uint8Array(256);
  b[0] = (json.length >> 8) & 0xff;
  b[1] = json.length & 0xff;
  b.set(json, 2);
  mutate?.(b);
  return b;
}

describe('openBlob', () => {
  it('opens the Go vector', () => {
    const r = openBlob(key, deviceId, vector.blob);
    expect(r.ok).toBe(true);
    if (r.ok) expect(r.event).toEqual(vector.event);
  });

  it('rejects every negative in the vector with the named error', () => {
    expect(vector.negative.length).toBeGreaterThan(0);
    for (const entry of vector.negative) {
      const r = openBlob(key, entry.device_id ?? deviceId, entry.blob);
      expect(r, entry.name).toEqual({ ok: false, error: ERRORS[entry.error] });
    }
  });

  it('rejects a wrong key and a short key', () => {
    expect(openBlob(wrong1, deviceId, vector.blob)).toEqual({ ok: false, error: 'auth' });
    expect(openBlob(new Uint8Array(31), deviceId, vector.blob)).toEqual({ ok: false, error: 'key' });
  });

  it('rejects non-object and ill-typed blobs without throwing', () => {
    for (const bad of [null, 'x', 42, { v: 1 }, { v: 1, n: 5, c: 'x' }, { v: 1, n: '!!!', c: '???' }]) {
      expect(openBlob(key, deviceId, bad)).toEqual({ ok: false, error: 'malformed' });
    }
    expect(openBlob(key, deviceId, { ...vector.blob, v: 2 })).toEqual({ ok: false, error: 'version' });
  });

  it('rejects framing violations', () => {
    const enc = new TextEncoder();
    const json = enc.encode(JSON.stringify(vector.event));
    const cases: Uint8Array[] = [
      framed(json, (b) => {
        b[0] = 0xff;
        b[1] = 0xff;
      }),
      framed(json, (b) => {
        b[255] = 1;
      }),
      framed(new Uint8Array([0x7b, 0x22, 0xff, 0xfe, 0x22, 0x7d])),
      framed(enc.encode(JSON.stringify({ ...vector.event, taskId: '12' }))),
    ];
    for (const plain of cases) {
      expect(openBlob(key, deviceId, sealRaw(plain))).toEqual({ ok: false, error: 'malformed' });
    }
  });
});

describe('openWithAnyKey', () => {
  it('trial decryption picks the matching key', () => {
    const keys = [
      { deviceId: 'a', key: wrong1 },
      { deviceId, key },
      { deviceId: 'b', key: wrong2 },
    ];
    const r = openWithAnyKey(keys, vector.blob);
    expect(r.ok).toBe(true);
    if (r.ok) expect(r.deviceId).toBe(vector.device_id);
    expect(openWithAnyKey([], vector.blob)).toEqual({ ok: false, error: 'key' });
    expect(openWithAnyKey([{ deviceId: 'a', key: wrong1 }, { deviceId: 'b', key: wrong2 }], vector.blob).ok).toBe(false);
  });
});

describe('notificationFor', () => {
  it('returns the real text, generic text, or null', () => {
    const result = openWithAnyKey([{ deviceId, key }], vector.blob);
    const fresh = notificationFor(result, vector.event.at + 10);
    expect(fresh).toEqual({
      title: vector.event.title,
      body: vector.event.body,
      data: { eventId: 'evt-0001', taskId: 12, chatId: 3 },
    });
    expect(notificationFor(result, vector.event.at + 86401)).toBeNull();
    const failed = notificationFor({ ok: false, error: 'auth' }, vector.event.at);
    expect(failed).toEqual({ title: GENERIC_NOTIFICATION.title, body: GENERIC_NOTIFICATION.body, data: null });
  });
});

describe('automation pause alerts (no task to open)', () => {
  const pause = { id: 'ev-pause-1', kind: 'error', taskId: 0, automationId: 3, title: 'Automation paused: nightly', body: 'Automation paused: the template is no longer valid', at: 1_000_000 };
  const seal = (event: object) => sealRaw(framed(new TextEncoder().encode(JSON.stringify(event))));

  it('opens with taskId 0 and carries the automation id into the notification data', () => {
    const n = notificationFor(openWithAnyKey([{ deviceId, key }], seal(pause)), 1_000_100);
    expect(n).toEqual({
      title: 'Automation paused: nightly',
      body: 'Automation paused: the template is no longer valid',
      data: { eventId: 'ev-pause-1', taskId: 0, automationId: 3 },
    });
  });

  it('rejects a negative or non-integer automation id', () => {
    for (const bad of [-1, 1.5, '3']) {
      expect(openBlob(key, deviceId, seal({ ...pause, automationId: bad })), String(bad)).toEqual({ ok: false, error: 'malformed' });
    }
  });
});

describe('stalled pushes (daemon kind "stalled")', () => {
  const stalled = {
    id: 'ev-stall-1',
    kind: 'stalled',
    taskId: 4,
    title: 'Lead task',
    body: 'Tree stalled: Lead task has been idle for 15 min',
    at: 1_000_000,
  };
  const seal = (event: object) => sealRaw(framed(new TextEncoder().encode(JSON.stringify(event))));

  it('opens as itself, not as a malformed blob', () => {
    const r = openBlob(key, deviceId, seal(stalled));
    expect(r.ok).toBe(true);
    if (r.ok) expect(r.event.kind).toBe('stalled');
  });

  it('shows the daemon-built title and body instead of the generic text', () => {
    const n = notificationFor(openWithAnyKey([{ deviceId, key }], seal(stalled)), 1_000_100);
    expect(n).toEqual({
      title: 'Lead task',
      body: 'Tree stalled: Lead task has been idle for 15 min',
      data: { eventId: 'ev-stall-1', taskId: 4 },
    });
  });

  it('still rejects an unknown kind', () => {
    expect(openBlob(key, deviceId, seal({ ...stalled, kind: 'bogus' }))).toEqual({ ok: false, error: 'malformed' });
  });
});
