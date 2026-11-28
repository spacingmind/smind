import { describe, expect, it, vi } from 'vitest';
import { IDBFactory } from 'fake-indexeddb';
import { ed25519 } from '@noble/curves/ed25519.js';
import { openDatabase, openRelayStore, type StoredPairing } from '../store';
import {
  NotPairedError,
  RepairRequiredError,
  newIdentityForPairing,
  loadIdentityForConnect,
} from '../identity-manager';

const pairing: StoredPairing = {
  offerUrl: 'smind://pair?x=1',
  daemonId: 'd1',
  relay: 'relay.example:443',
  relayFingerprint: 'ab',
  label: 'laptop',
  pairedAt: 1,
};

describe('relay store', () => {
  it('identity_key_survives_reload', async () => {
    const factory = new IDBFactory();
    const a = await openRelayStore(factory);
    const { record, identity: created } = await newIdentityForPairing();
    await a.savePaired(record, pairing);
    a.close();

    const b = await openRelayStore(factory);
    const rec = await b.loadIdentity();
    expect(rec).not.toBeNull();
    expect(rec!.privateKey.extractable).toBe(false);
    const msg = new TextEncoder().encode('reload');
    const sig = new Uint8Array(
      await crypto.subtle.sign({ name: 'Ed25519' }, rec!.privateKey, msg),
    );
    expect(ed25519.verify(sig, msg, rec!.publicKey)).toBe(true);

    const { identity } = await loadIdentityForConnect(b);
    expect(identity.deviceId).toBe(created.deviceId);
    b.close();
  });

  it('identity_key_missing_means_repair_not_new_identity', async () => {
    const factory = new IDBFactory();
    const s = await openRelayStore(factory);
    await expect(loadIdentityForConnect(s)).rejects.toBeInstanceOf(NotPairedError);
    await s.savePairing(pairing);
    const err = await loadIdentityForConnect(s).catch((e) => e);
    expect(err).toBeInstanceOf(RepairRequiredError);
    expect(err.reason).toBe('identity_missing');
    expect(await s.loadIdentity()).toBeNull();
    s.close();
  });

  it('savepaired_is_atomic', async () => {
    const factory = new IDBFactory();
    const s = await openRelayStore(factory);
    const a = await newIdentityForPairing();
    await s.savePaired(a.record, pairing);

    const b = await newIdentityForPairing();
    // The identity put succeeds, then the pairing put throws (a function cannot be cloned): nothing may change.
    const bad = { ...pairing, label: 'B', offerUrl: (() => 1) as unknown as string };
    await expect(s.savePaired(b.record, bad)).rejects.toBeDefined();
    const id = await s.loadIdentity();
    expect(Array.from(id!.publicKey)).toEqual(Array.from(a.record.publicKey));
    expect((await s.loadPairing())!.label).toBe('laptop');
    s.close();
  });

  it('a_late_open_after_blocked_is_closed', async () => {
    vi.useFakeTimers();
    try {
      const closed: string[] = [];
      const req: Record<string, unknown> = {};
      const factory = { open: () => req } as unknown as IDBFactory;
      const p = openDatabase(factory, () => undefined, 5_000);
      const settled = p.then(
        () => 'resolved',
        (e: Error) => e.message,
      );
      (req.onblocked as () => void)();
      await vi.advanceTimersByTimeAsync(4_999);
      // still waiting: not rejected yet
      let early = false;
      void settled.then(() => (early = true));
      await Promise.resolve();
      expect(early).toBe(false);
      await vi.advanceTimersByTimeAsync(2);
      expect(await settled).toMatch(/blocked by another tab/);
      // the database opens only now: it must be closed straight away
      req.result = { close: () => closed.push('closed') };
      (req.onsuccess as () => void)();
      expect(closed).toEqual(['closed']);
    } finally {
      vi.useRealTimers();
    }
  });

  it('a_closed_store_rejects_with_a_clear_error', async () => {
    const s = await openRelayStore(new IDBFactory());
    s.close();
    await expect(s.loadIdentity()).rejects.toThrow('the relay store is closed');
    await expect(s.savePairing(pairing)).rejects.toThrow('the relay store is closed');
  });
});
