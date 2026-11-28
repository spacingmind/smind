import type { DeviceIdentity } from '@smind/relay-client/src/deviceIdentity';
import { generateWebIdentityKey, identityFromWebKey } from './identity';
import type { RelayStore, StoredIdentity, StoredPairing } from './store';

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

/**
 * The only function that generates a device key. It persists nothing: the
 * caller saves the record together with the pairing (RelayStore.savePaired),
 * so a failed save never leaves a new key beside an old pairing.
 */
export async function newIdentityForPairing(
  subtle: SubtleCrypto = crypto.subtle,
): Promise<{ record: StoredIdentity; identity: DeviceIdentity }> {
  const key = await generateWebIdentityKey(subtle);
  return { record: { ...key, createdAt: Date.now() }, identity: identityFromWebKey(key, subtle) };
}
