import type { DeviceIdentity } from '@smind/relay-client/src/deviceIdentity';
import { generateWebIdentityKey, identityFromWebKey } from './identity';
import type { RelayStore, StoredPairing } from './store';

export class NotPairedError extends Error {
  constructor() {
    super('this browser is not paired with a daemon');
    this.name = 'NotPairedError';
  }
}

export class RepairRequiredError extends Error {
  reason: 'identity_missing' = 'identity_missing';
  constructor() {
    super("this browser's device key is gone (cleared site data or browser eviction): pair again");
    this.name = 'RepairRequiredError';
  }
}

/** Never creates a key: a missing identity under a pairing means re-pair. */
export async function loadIdentityForConnect(
  store: RelayStore,
  subtle: SubtleCrypto = crypto.subtle,
): Promise<{ identity: DeviceIdentity; pairing: StoredPairing }> {
  const pairing = await store.loadPairing();
  if (!pairing) throw new NotPairedError();
  const rec = await store.loadIdentity();
  if (!rec) throw new RepairRequiredError();
  return { identity: identityFromWebKey(rec, subtle), pairing };
}

/** The only function that generates a device key. */
export async function createIdentityForPairing(
  store: RelayStore,
  subtle: SubtleCrypto = crypto.subtle,
): Promise<DeviceIdentity> {
  const key = await generateWebIdentityKey(subtle);
  await store.saveIdentity({ ...key, createdAt: Date.now() });
  return identityFromWebKey(key, subtle);
}
