import { describe, expect, it } from 'vitest';
import { ed25519 } from '@noble/curves/ed25519.js';
import {
  UnsupportedBrowserError,
  generateWebIdentityKey,
  identityFromWebKey,
  webCryptoSupportsEd25519,
} from '../identity';

describe('web identity', () => {
  it('identity_key_is_non_extractable', async () => {
    const rec = await generateWebIdentityKey();
    expect(rec.publicKey.length).toBe(32);
    expect(rec.privateKey.extractable).toBe(false);
    await expect(crypto.subtle.exportKey('pkcs8', rec.privateKey)).rejects.toBeDefined();
    await expect(crypto.subtle.exportKey('jwk', rec.privateKey)).rejects.toBeDefined();

    const id = identityFromWebKey(rec);
    const msg = new TextEncoder().encode('hello');
    const sig = await id.sign(msg);
    expect(ed25519.verify(sig, msg, rec.publicKey)).toBe(true);

    for (const v of Object.values(id)) expect(v).not.toBeInstanceOf(CryptoKey);
    expect(Object.values(id).includes(rec.privateKey)).toBe(false);
    expect(JSON.stringify(id)).not.toContain('privateKey');
  });

  it('unsupported_browser_fails_typed', async () => {
    const fake = {
      generateKey: () => Promise.reject(new DOMException('nope', 'NotSupportedError')),
    } as unknown as SubtleCrypto;
    expect(await webCryptoSupportsEd25519(fake)).toBe(false);
    const err = await generateWebIdentityKey(fake).catch((e) => e);
    expect(err).toBeInstanceOf(UnsupportedBrowserError);
    expect(err.message).toMatch(/Ed25519/);

    // a missing subtle (null; undefined would fall back to the default)
    const nul = null as unknown as SubtleCrypto;
    expect(await webCryptoSupportsEd25519(nul)).toBe(false);
    await expect(generateWebIdentityKey(nul)).rejects.toBeInstanceOf(UnsupportedBrowserError);
  });
});
