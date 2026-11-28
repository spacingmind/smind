// admission.test.ts pins the SCRAM-style admission proof (ADR-0011, protocol
// v2) against the same fixed vector internal/relay/admission's
// TestProofKnownVector and desktop/daemon-client's admission.rs assert, so a
// drift in any one implementation's transcript/XOR construction fails that
// implementation's own suite.

import { describe, expect, it } from 'vitest';
import { PROTOCOL_VERSION, computeProof, hashSecret, proofMask } from '../admission';

const toHex = (b: Uint8Array) => Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');

const secret = new Uint8Array(32).fill(0x42);
const transcript = {
  protocolVersion: PROTOCOL_VERSION,
  workspaceId: 'ws-vector',
  clientNonce: new Uint8Array(32).fill(0x01),
  serverNonce: new Uint8Array(32).fill(0x02),
  daemonKeyId: 'key-vector',
};

describe('admission proof', () => {
  it('speaks protocol v2', () => {
    expect(PROTOCOL_VERSION).toBe(2);
  });

  it('matches the cross-language known vector', () => {
    expect(toHex(computeProof(secret, transcript))).toBe(
      'b007e940ba3c295ffd33e247373d4d150370b3702d90d9a4757d435227b37cfb'
    );
  });

  it('is ClientKey XOR HMAC(StoredKey, transcript), so the stored hash alone cannot produce it', () => {
    const storedKey = hashSecret(secret);
    const mask = proofMask(storedKey, transcript);
    const proof = computeProof(secret, transcript);
    // recover ClientKey the way the relay does, then hash it back to StoredKey
    const recovered = proof.map((b, i) => b ^ mask[i]);
    expect(toHex(recovered)).toBe(toHex(secret));
    expect(toHex(hashSecret(recovered))).toBe(toHex(storedKey));
    // The v1 value (the bare mask) is not a valid proof.
    expect(toHex(mask)).not.toBe(toHex(proof));
  });
});
