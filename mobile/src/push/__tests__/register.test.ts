import { describe, expect, it, vi } from 'vitest';
import { base64Encode } from '../../relay/base64';
import { MemoryPushKeyStore, PushKeyStore } from '../keys';
import { ALL_KINDS, disablePush, enablePush, reregisterToken, RegisterDeps } from '../register';

interface Harness {
  deps: RegisterDeps;
  order: string[];
  calls: { method: string; params: unknown }[];
  keys: PushKeyStore;
}

function harness(opts: {
  permission?: boolean;
  token?: string | null;
  rpc?: (method: string, params: unknown) => Promise<unknown>;
} = {}): Harness {
  const order: string[] = [];
  const calls: { method: string; params: unknown }[] = [];
  const mem = new MemoryPushKeyStore();
  const keys: PushKeyStore = {
    put: async (d, k) => {
      order.push('key-put');
      await mem.put(d, k);
    },
    get: (d) => mem.get(d),
    delete: async (d) => {
      order.push('key-delete');
      await mem.delete(d);
    },
    all: () => mem.all(),
  };
  let counter = 0;
  const deps: RegisterDeps = {
    platform: 'android',
    deviceId: 'dev-1',
    ensureChannel: async () => {
      order.push('channel');
    },
    requestPermission: async () => {
      order.push('permission');
      return opts.permission ?? true;
    },
    getToken: async () => {
      order.push('token');
      return opts.token === undefined ? 'ExponentPushToken[abc]' : opts.token;
    },
    call: async (method, params) => {
      order.push(`rpc:${method}`);
      calls.push({ method, params });
      if (opts.rpc) return opts.rpc(method, params);
      return {};
    },
    keys,
    randomBytes: (n) => new Uint8Array(n).fill(++counter),
  };
  return { deps, order, calls, keys };
}

describe('enablePush', () => {
  it('enablePush runs channel, permission, token, then registers', async () => {
    const h = harness();
    const r = await enablePush(h.deps);
    expect(r).toEqual({ ok: true, kinds: [...ALL_KINDS] });
    expect(h.order).toEqual(['channel', 'permission', 'token', 'key-put', 'rpc:push.register']);
  });

  it('the RPC params are exactly v, provider, token, platform, key (std base64 of the stored 32 bytes) and kinds', async () => {
    const h = harness();
    await enablePush(h.deps, ['permission', 'done']);
    const stored = (await h.keys.get('dev-1'))!;
    expect(stored.length).toBe(32);
    expect(h.calls).toEqual([
      {
        method: 'push.register',
        params: {
          v: 1,
          provider: 'expo',
          token: 'ExponentPushToken[abc]',
          platform: 'android',
          key: base64Encode(stored),
          kinds: ['permission', 'done'],
        },
      },
    ]);
    expect(Object.keys(h.calls[0].params as object).sort()).toEqual(['key', 'kinds', 'platform', 'provider', 'token', 'v']);
  });

  it('uses the kinds the daemon echoes back', async () => {
    const h = harness({ rpc: async () => ({ kinds: ['done'] }) });
    expect(await enablePush(h.deps)).toEqual({ ok: true, kinds: ['done'] });
  });

  it('permission denied stops before the token and the daemon', async () => {
    const h = harness({ permission: false });
    expect(await enablePush(h.deps)).toEqual({ ok: false, reason: 'permission_denied' });
    expect(h.order).toEqual(['channel', 'permission']);
    expect(await h.keys.all()).toEqual([]);
  });

  it('no token stops before the daemon and stores no key', async () => {
    for (const token of [null, '']) {
      const h = harness({ token });
      expect(await enablePush(h.deps)).toEqual({ ok: false, reason: 'no_token' });
      expect(h.order).toEqual(['channel', 'permission', 'token']);
      expect(await h.keys.all()).toEqual([]);
    }
    const h = harness();
    h.deps.getToken = async () => {
      throw new Error('boom');
    };
    expect(await enablePush(h.deps)).toEqual({ ok: false, reason: 'no_token' });
    expect(h.calls).toEqual([]);
  });

  it('an RPC failure deletes the new key and reports rpc_failed', async () => {
    const h = harness({
      rpc: async () => {
        throw new Error('daemon said no');
      },
    });
    expect(await enablePush(h.deps)).toEqual({ ok: false, reason: 'rpc_failed', message: 'daemon said no' });
    expect(h.order.slice(-2)).toEqual(['rpc:push.register', 'key-delete']);
    expect(await h.keys.get('dev-1')).toBeNull();
  });

  it('every enable generates a fresh key', async () => {
    const h = harness();
    await enablePush(h.deps);
    const first = (await h.keys.get('dev-1'))!;
    await enablePush(h.deps);
    const second = (await h.keys.get('dev-1'))!;
    expect(second).not.toEqual(first);
    const [p1, p2] = h.calls.map((c) => (c.params as { key: string }).key);
    expect(p1).not.toBe(p2);
  });
});

describe('reregisterToken', () => {
  it('reregisterToken reuses the stored key and does not prompt', async () => {
    const h = harness();
    await enablePush(h.deps);
    const stored = (await h.keys.get('dev-1'))!;
    h.order.length = 0;
    h.calls.length = 0;
    h.deps.getToken = async () => {
      h.order.push('token');
      return 'ExponentPushToken[new]';
    };
    const r = await reregisterToken(h.deps);
    expect(r).toEqual({ ok: true, kinds: [...ALL_KINDS] });
    expect(h.order).toEqual(['token', 'rpc:push.register']);
    expect(h.calls[0].params).toMatchObject({ token: 'ExponentPushToken[new]', key: base64Encode(stored) });
    expect(await h.keys.get('dev-1')).toEqual(stored);
  });

  it('reregisterToken returns null when push was never enabled', async () => {
    const h = harness();
    expect(await reregisterToken(h.deps)).toBeNull();
    expect(h.order).toEqual([]);
  });
});

describe('disablePush', () => {
  it('disablePush unregisters then deletes the key even when the RPC fails', async () => {
    const h = harness();
    await enablePush(h.deps);
    h.order.length = 0;
    h.deps.call = async (method) => {
      h.order.push(`rpc:${method}`);
      throw new Error('offline');
    };
    await expect(disablePush(h.deps)).resolves.toBeUndefined();
    expect(h.order).toEqual(['rpc:push.unregister', 'key-delete']);
    expect(await h.keys.get('dev-1')).toBeNull();
  });
});

describe('logging', () => {
  it('no console output', async () => {
    const spies = (['log', 'warn', 'error'] as const).map((m) => vi.spyOn(console, m).mockImplementation(() => {}));
    try {
      const h = harness();
      await enablePush(h.deps);
      await reregisterToken(h.deps);
      await disablePush(h.deps);
      const failing = harness({
        rpc: async () => {
          throw new Error('x');
        },
      });
      await enablePush(failing.deps);
      await disablePush(failing.deps);
      for (const s of spies) expect(s).not.toHaveBeenCalled();
    } finally {
      for (const s of spies) s.mockRestore();
    }
  });
});

describe('the kinds a phone subscribes to', () => {
  it('include stalled, so the daemon sends it stall alerts (the daemon filters by registered kind)', () => {
    expect(ALL_KINDS).toContain('stalled');
  });
});
