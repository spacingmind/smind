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
  readonly reason: 'identity_missing' | 'record_invalid';
  constructor(reason: 'identity_missing' | 'record_invalid' = 'identity_missing') {
    super(
      reason === 'identity_missing'
        ? "this browser's device key is gone (cleared site data or browser eviction): pair again"
        : "this browser's saved pairing is damaged: pair again",
    );
    this.name = 'RepairRequiredError';
    this.reason = reason;
  }
}

/** Never creates a key: a missing or damaged record under a pairing means re-pair (nothing is deleted). */
export async function loadIdentityForConnect(
  store: RelayStore,
  subtle: SubtleCrypto = crypto.subtle,
): Promise<{ identity: DeviceIdentity; pairing: StoredPairing }> {
  const pairing = await store.inspectPairing();
  const rec = await store.inspectIdentity();
  if (pairing.status === 'invalid' || rec.status === 'invalid') throw new RepairRequiredError('record_invalid');
  if (pairing.status === 'absent') throw new NotPairedError();
  if (rec.status === 'absent') throw new RepairRequiredError('identity_missing');
  return { identity: identityFromWebKey(rec.value, subtle), pairing: pairing.value };
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
