// pairing.test.ts parses the exact same fixture URL
// internal/relay/pairing/fixture_test.go's TestFixtureOfferURLForTypeScriptPort
// generates and pins Go-side, proving this module's parser is wire-
// compatible with the real Go encoder -- not just internally consistent
// with a hand-written TypeScript encoder that could share the same bug.

import { describe, expect, it } from 'vitest';
import { base64UrlEncode } from '../base64';
import { decodeOfferPayload, InvalidOfferError, parsePairingURL } from '../pairing';

// Copied verbatim from internal/relay/pairing/fixture_test.go's own
// fixtureOfferURL constant.
const FIXTURE_URL =
  'https://spacingmind.sh/pair#offer=eyJ2IjoxLCJpZCI6ImRhZW1vbi1maXh0dXJlLTEiLCJwayI6IkVSRVJFUkVSRVJFUkVSRVJFUkVSRVJFUkVSRVJFUkVSRVJFUkVSRVJFUkUiLCJyZWxheSI6Imh0dHBzOi8vcmVsYXkuZXhhbXBsZS50ZXN0Ojc0MDEiLCJmcCI6ImRlYWRiZWVmIiwic2VjIjoicTZ1cnE2dXJxNnVycTZ1cnE2dXJxNnVycTZ1cnE2dXJxNnVycTZ1cnE2cyIsIndzIjoid3MtZml4dHVyZS0xIn0';

function hex(bytes: Uint8Array): string {
  return Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');
}

describe('pairing offer parser (cross-language fixture)', () => {
  it('parses the Go-generated fixture URL exactly', () => {
    const offer = parsePairingURL(FIXTURE_URL);
    expect(offer.daemonId).toBe('daemon-fixture-1');
    expect(hex(offer.publicKey)).toBe('11'.repeat(32));
    expect(offer.relay).toBe('https://relay.example.test:7401');
    expect(offer.relayFingerprint).toBe('deadbeef');
    expect(hex(offer.secret)).toBe('ab'.repeat(32));
    expect(offer.workspaceId).toBe('ws-fixture-1');
  });

  it('rejects a URL with no fragment', () => {
    expect(() => parsePairingURL('https://spacingmind.sh/pair')).toThrow(InvalidOfferError);
  });

  it('rejects a payload that is not valid base64url JSON', () => {
    expect(() => decodeOfferPayload('!!!not base64!!!')).toThrow(InvalidOfferError);
  });

  it('rejects an unsupported offer version', () => {
    const payload = base64UrlEncode(new TextEncoder().encode(JSON.stringify({ v: 99, id: 'a', pk: '', relay: 'https://r' })));
    expect(() => decodeOfferPayload(payload)).toThrow(InvalidOfferError);
  });
});
