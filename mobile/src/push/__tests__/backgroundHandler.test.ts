import { readFileSync } from 'node:fs';
import path from 'node:path';
import { describe, expect, it, vi } from 'vitest';
import { blobFromTaskPayload, handleBackgroundPush, type BackgroundDeps } from '../backgroundHandler';

const vector = JSON.parse(
  readFileSync(path.resolve(__dirname, '../../../../internal/push/testdata/push_seal_vector.json'), 'utf8'),
);

function hexToBytes(hex: string): Uint8Array {
  const out = new Uint8Array(hex.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(hex.slice(i * 2, i * 2 + 2), 16);
  return out;
}

const goodKeys = [{ deviceId: vector.device_id as string, key: hexToBytes(vector.key_hex) }];
const at: number = vector.event.at;

function deps(over: Partial<BackgroundDeps> = {}) {
  const present = vi.fn(async () => {});
  const d: BackgroundDeps = { keys: { all: async () => goodKeys }, nowSeconds: () => at + 5, present, ...over };
  return { d, present: d.present === present ? present : (d.present as ReturnType<typeof vi.fn>) };
}

const viaDataString = { data: { dataString: JSON.stringify(vector.blob) } };

describe('handleBackgroundPush', () => {
  it('shows the real text for a vector blob in dataString', async () => {
    const { d, present } = deps();
    expect(await handleBackgroundPush(viaDataString, d)).toBe('shown');
    expect(present).toHaveBeenCalledTimes(1);
    expect(present).toHaveBeenCalledWith({
      identifier: 'evt-0001',
      title: vector.event.title,
      body: vector.event.body,
      data: { eventId: 'evt-0001', taskId: 12, chatId: 3 },
    });
  });

  it('accepts the Expo-wrapped body shape', async () => {
    const { d, present } = deps();
    const payload = { data: { dataString: JSON.stringify({ body: JSON.stringify(vector.blob) }) } };
    expect(await handleBackgroundPush(payload, d)).toBe('shown');
    expect(present).toHaveBeenCalledTimes(1);
  });

  it('accepts data and top-level shapes', async () => {
    expect(blobFromTaskPayload({ data: vector.blob })).toEqual(vector.blob);
    expect(blobFromTaskPayload(vector.blob)).toEqual(vector.blob);
    const { d, present } = deps();
    expect(await handleBackgroundPush({ data: vector.blob }, d)).toBe('shown');
    expect(await handleBackgroundPush(vector.blob, d)).toBe('shown');
    expect(present).toHaveBeenCalledTimes(2);
  });

  it('shows the generic notification when no key opens the blob', async () => {
    const { d, present } = deps({ keys: { all: async () => [{ deviceId: 'other', key: new Uint8Array(32).fill(9) }] } });
    expect(await handleBackgroundPush(viaDataString, d)).toBe('generic');
    expect(present).toHaveBeenCalledTimes(1);
    const arg = present.mock.calls[0][0] as Record<string, unknown>;
    expect(arg).not.toHaveProperty('identifier');
    expect(arg.data).toEqual({});
  });

  it('ignores a payload with no blob', async () => {
    for (const p of [{}, null, { data: { dataString: 'not json' } }, { data: { dataString: '{"x":1}' } }]) {
      const { d, present } = deps();
      expect(await handleBackgroundPush(p, d)).toBe('ignored');
      expect(present).not.toHaveBeenCalled();
    }
  });

  it('drops a stale push', async () => {
    const { d, present } = deps({ nowSeconds: () => at + 86401 });
    expect(await handleBackgroundPush(viaDataString, d)).toBe('dropped');
    expect(present).not.toHaveBeenCalled();
  });

  it('a failing key store falls back to the generic notification', async () => {
    const { d, present } = deps({
      keys: {
        all: async () => {
          throw new Error('locked');
        },
      },
    });
    expect(await handleBackgroundPush(viaDataString, d)).toBe('generic');
    expect(present).toHaveBeenCalledTimes(1);
  });

  it('present errors propagate', async () => {
    const { d } = deps({
      present: async () => {
        throw new Error('boom');
      },
    });
    await expect(handleBackgroundPush(viaDataString, d)).rejects.toThrow('boom');
  });
});
