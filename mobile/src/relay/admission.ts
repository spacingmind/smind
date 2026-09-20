// admission.ts mirrors internal/relay/admission's client-side transcript
// construction: HashSecret and ComputeHMAC, byte-for-byte, since the relay
// verifies this exact HMAC (see admission.go's doc comment on the
// canonical concatenation).

import { hmac } from '@noble/hashes/hmac.js';
import { sha256 } from '@noble/hashes/sha2.js';

export const PROTOCOL_VERSION = 1;
export const NONCE_SIZE = 32;

/** SHA-256 of the raw workspace secret -- the HMAC key, matching admission.go's HashSecret. */
export function hashSecret(secret: Uint8Array): Uint8Array {
  return sha256(secret);
}

function writeUint32BE(value: number): Uint8Array {
  const out = new Uint8Array(4);
  new DataView(out.buffer).setUint32(0, value, false);
  return out;
}

function lengthPrefixedString(s: string): Uint8Array {
  const bytes = new TextEncoder().encode(s);
  const out = new Uint8Array(4 + bytes.length);
  out.set(writeUint32BE(bytes.length), 0);
  out.set(bytes, 4);
  return out;
}

function concatBytes(...parts: Uint8Array[]): Uint8Array {
  const total = parts.reduce((n, p) => n + p.length, 0);
  const out = new Uint8Array(total);
  let offset = 0;
  for (const p of parts) {
    out.set(p, offset);
    offset += p.length;
  }
  return out;
}

export interface AdmitTranscript {
  protocolVersion: number;
  workspaceId: string;
  clientNonce: Uint8Array;
  serverNonce: Uint8Array;
  daemonKeyId: string;
}

/**
 * ComputeHMAC: HMAC-SHA256(key, protocol_version || LP(workspace_id) ||
 * client_nonce || server_nonce || LP(daemon_key_id)), the exact transcript
 * admission.go's ComputeHMAC builds -- LP(s) is a 4-byte big-endian length
 * prefix followed by the UTF-8 bytes, so concatenation is unambiguous.
 */
export function computeHMAC(key: Uint8Array, t: AdmitTranscript): Uint8Array {
  const message = concatBytes(
    writeUint32BE(t.protocolVersion),
    lengthPrefixedString(t.workspaceId),
    t.clientNonce,
    t.serverNonce,
    lengthPrefixedString(t.daemonKeyId)
  );
  return hmac(sha256, key, message);
}

/** A fresh random client nonce, NONCE_SIZE bytes. */
export function randomNonce(): Uint8Array {
  const nonce = new Uint8Array(NONCE_SIZE);
  crypto.getRandomValues(nonce);
  return nonce;
}
