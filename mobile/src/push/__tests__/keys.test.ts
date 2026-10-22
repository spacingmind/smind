import { describe, expect, it } from 'vitest';
import type { SecureStoreLike } from '../../relay/deviceKeyStore';
import { MemoryPushKeyStore, PushKeyStore, secureStorePushKeyStore } from '../keys';

type Opts = { keychainAccessible?: number } | undefined;

function fakeStore(accessible?: number) {
  const data = new Map<string, string>();
  const seen: Opts[] = [];
  const store: SecureStoreLike = {
    keychainAccessible: accessible,
    async getItemAsync(k, o) {
      seen.push(o);
      return data.get(k) ?? null;
    },
    async setItemAsync(k, v, o) {
      seen.push(o);
      data.set(k, v);
    },
    async deleteItemAsync(k, o) {
      seen.push(o);
      data.delete(k);
    },
  };
  return { store, data, seen };
}

const keyA = new Uint8Array(32).map((_, i) => i);
const keyB = new Uint8Array(32).fill(9);

describe('secureStorePushKeyStore', () => {
  it('round-trips a key and lists it', async () => {
    const { store, data } = fakeStore();
    const s = secureStorePushKeyStore(store);
    await s.put('dev-a', keyA);
    expect(await s.get('dev-a')).toEqual(keyA);
    expect(data.get('smind.push.key.v1.dev-a')).toMatch(/^[0-9a-f]{64}$/);
    expect(JSON.parse(data.get('smind.push.keys.v1')!)).toEqual(['dev-a']);
    await s.put('dev-a', keyB);
    await s.put('dev-b', keyA);
    expect(await s.all()).toEqual([
      { deviceId: 'dev-a', key: keyB },
      { deviceId: 'dev-b', key: keyA },
    ]);
    expect(JSON.parse(data.get('smind.push.keys.v1')!)).toEqual(['dev-a', 'dev-b']);
  });

  it('put rejects a key that is not 32 bytes', async () => {
    const { store, data } = fakeStore();
    const s = secureStorePushKeyStore(store);
    await expect(s.put('dev-a', new Uint8Array(31))).rejects.toBeInstanceOf(RangeError);
    expect(data.size).toBe(0);
  });

  it('a corrupt stored value reads as missing', async () => {
    const { store, data } = fakeStore();
    const s = secureStorePushKeyStore(store);
    data.set('smind.push.key.v1.dev-a', 'zz');
    expect(await s.get('dev-a')).toBeNull();
    data.set('smind.push.key.v1.dev-a', 'A'.repeat(64));
    expect(await s.get('dev-a')).toBeNull();
  });

  it('all() skips corrupt and missing entries and survives a corrupt index', async () => {
    const { store, data } = fakeStore();
    const s = secureStorePushKeyStore(store);
    await s.put('good', keyA);
    data.set('smind.push.keys.v1', JSON.stringify(['good', 'corrupt', 'missing', 7, null, 'BAD ID']));
    data.set('smind.push.key.v1.corrupt', 'nothex');
    expect(await s.all()).toEqual([{ deviceId: 'good', key: keyA }]);
    data.set('smind.push.keys.v1', '{not json');
    expect(await s.all()).toEqual([]);
    data.set('smind.push.keys.v1', '{"a":1}');
    expect(await s.all()).toEqual([]);
  });

  it('delete removes the key and its index entry', async () => {
    const { store, data } = fakeStore();
    const s = secureStorePushKeyStore(store);
    await s.put('dev-a', keyA);
    await s.put('dev-b', keyB);
    await s.delete('dev-a');
    expect(await s.get('dev-a')).toBeNull();
    expect(data.has('smind.push.key.v1.dev-a')).toBe(false);
    expect(JSON.parse(data.get('smind.push.keys.v1')!)).toEqual(['dev-b']);
    await s.delete('dev-a');
    expect((await s.all()).map((e) => e.deviceId)).toEqual(['dev-b']);
  });

  it('rejects a device id that is not a valid secure-store name', async () => {
    const { store } = fakeStore();
    const s = secureStorePushKeyStore(store);
    for (const bad of ['', 'A', 'a b', 'a/b', 'x'.repeat(65)]) {
      await expect(s.put(bad, keyA)).rejects.toBeInstanceOf(RangeError);
      await expect(s.get(bad)).rejects.toBeInstanceOf(RangeError);
      await expect(s.delete(bad)).rejects.toBeInstanceOf(RangeError);
    }
  });

  it('keychainAccessible is passed on every call', async () => {
    const { store, seen } = fakeStore(3);
    const s = secureStorePushKeyStore(store);
    await s.put('dev-a', keyA);
    await s.get('dev-a');
    await s.all();
    await s.delete('dev-a');
    expect(seen.length).toBeGreaterThan(4);
    for (const o of seen) expect(o).toEqual({ keychainAccessible: 3 });
  });
});

describe('MemoryPushKeyStore', () => {
  it('the memory store behaves the same', async () => {
    const s: PushKeyStore = new MemoryPushKeyStore();
    await s.put('dev-a', keyA);
    await s.put('dev-b', keyB);
    expect(await s.get('dev-a')).toEqual(keyA);
    expect(await s.get('nope')).toBeNull();
    expect(await s.all()).toHaveLength(2);
    await s.delete('dev-a');
    expect(await s.get('dev-a')).toBeNull();
    expect((await s.all()).map((e) => e.deviceId)).toEqual(['dev-b']);
    await expect(s.put('dev-c', new Uint8Array(5))).rejects.toBeInstanceOf(RangeError);
    await expect(s.put('Bad Id', keyA)).rejects.toBeInstanceOf(RangeError);
  });
});
