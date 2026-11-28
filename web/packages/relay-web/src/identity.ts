// identity.ts is the browser half of the device identity: a non-extractable
// WebCrypto Ed25519 key. The private key never leaves the closure.

import { deviceIdFor, type DeviceIdentity } from '@smind/relay-client/src/deviceIdentity';

export class UnsupportedBrowserError extends Error {
  constructor() {
    super(
      'this browser cannot pair: WebCrypto Ed25519 is not supported (needs Chrome 137, Firefox 129 or Safari 17 or newer)',
    );
    this.name = 'UnsupportedBrowserError';
  }
}

export async function webCryptoSupportsEd25519(
  subtle: SubtleCrypto = crypto.subtle,
): Promise<boolean> {
  try {
    await subtle.generateKey({ name: 'Ed25519' }, false, ['sign', 'verify']);
    return true;
  } catch {
    return false;
  }
}

export async function generateWebIdentityKey(
  subtle: SubtleCrypto = crypto.subtle,
): Promise<{ privateKey: CryptoKey; publicKey: Uint8Array }> {
  try {
    const pair = (await subtle.generateKey({ name: 'Ed25519' }, false, [
      'sign',
      'verify',
    ])) as CryptoKeyPair;
    const publicKey = new Uint8Array(await subtle.exportKey('raw', pair.publicKey));
    return { privateKey: pair.privateKey, publicKey };
  } catch {
    throw new UnsupportedBrowserError();
  }
}

export function identityFromWebKey(
  rec: { privateKey: CryptoKey; publicKey: Uint8Array },
  subtle: SubtleCrypto = crypto.subtle,
): DeviceIdentity {
  const { privateKey } = rec;
  const publicKey = rec.publicKey;
  return {
    publicKey,
    deviceId: deviceIdFor(publicKey),
    sign: async (msg: Uint8Array) =>
      new Uint8Array(await subtle.sign({ name: 'Ed25519' }, privateKey, msg as BufferSource)),
  };
}
