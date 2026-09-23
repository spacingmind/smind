// e2ee.test.ts is the plan's single most important test
// (docs/plans/active/mobile-app-milestone-1.md's Item 3 Test Scenarios):
// it hardcodes the exact same fixed public keys and ciphertext literals
// internal/relay/e2ee/fixture_test.go computes Go-side (via the real,
// unexported newSession -- not a reimplementation), so both languages are
// checked against the same numbers. A subtly wrong HKDF info string or
// nonce construction would otherwise fail silently as "handshake hangs"
// rather than a clear test failure -- this is exactly the failure mode
// this test exists to catch instead.

import { describe, expect, it } from 'vitest';
import { x25519 } from '@noble/curves/ed25519.js';
import { Role, Session, encodeFrame, PROTOCOL_VERSION } from '../e2ee';

function hex(bytes: Uint8Array): string {
  return Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');
}

function fromHex(s: string): Uint8Array {
  const bytes = new Uint8Array(s.length / 2);
  for (let i = 0; i < bytes.length; i++) bytes[i] = parseInt(s.slice(i * 2, i * 2 + 2), 16);
  return bytes;
}

function fixtureSeed(byte: number): Uint8Array {
  return new Uint8Array(32).fill(byte);
}

// Literals copied from internal/relay/e2ee/fixture_test.go's own
// TestFixtureVectorsForTypeScriptPort / TestFixtureHelloFrameForTypeScriptPort
// output -- do not hand-derive these; they are the Go side's ground truth.
const DAEMON_PUB_HEX = '7b4e909bbe7ffe44c465a220037d608ee35897d31ef972f07f74892cb0f73f13';
const MOBILE_PUB_HEX = '0faa684ed28867b97f4a6a2dee5df8ce974e76b7018e3f22a1c4cf2678570f20';
const DAEMON_TO_MOBILE_CIPHER0 =
  'd6a248cac0f4b322f162664fc9c16b2ef390e3809baa230e1d23499e9d8e64c16442705fa096806590f5bc4b37';
const MOBILE_TO_DAEMON_CIPHER0 = '233f25b1c411311a8ea4dd9f9034e63a44f9fb4808b83c89ee9de79cca7b2398c27a50e891de';
const PLAINTEXT_D2M = 'hello from smind e2ee fixture';
const PLAINTEXT_M2D = 'hello back from mobile';
const DAEMON_HELLO_FRAME_HEX = '000000230101017b4e909bbe7ffe44c465a220037d608ee35897d31ef972f07f74892cb0f73f13';

describe('e2ee cross-language fixture', () => {
  it('derives the same public keys as the Go fixture', () => {
    const daemonSecret = fixtureSeed(0x11);
    const mobileSecret = fixtureSeed(0x22);
    expect(hex(x25519.getPublicKey(daemonSecret))).toBe(DAEMON_PUB_HEX);
    expect(hex(x25519.getPublicKey(mobileSecret))).toBe(MOBILE_PUB_HEX);
  });

  it('seals byte-identical ciphertext to the Go fixture, both directions', () => {
    const daemonSecret = fixtureSeed(0x11);
    const mobileSecret = fixtureSeed(0x22);
    const daemonPub = x25519.getPublicKey(daemonSecret);
    const mobilePub = x25519.getPublicKey(mobileSecret);

    const daemonSession = Session.derive(daemonSecret, mobilePub, Role.Daemon, daemonPub, mobilePub);
    const mobileSession = Session.derive(mobileSecret, daemonPub, Role.Mobile, daemonPub, mobilePub);

    const sealed = daemonSession.seal(new TextEncoder().encode(PLAINTEXT_D2M));
    expect(sealed.counter).toBe(0);
    expect(hex(sealed.ciphertext)).toBe(DAEMON_TO_MOBILE_CIPHER0);

    const opened = mobileSession.open(0, sealed.ciphertext);
    expect(new TextDecoder().decode(opened)).toBe(PLAINTEXT_D2M);

    const reply = mobileSession.seal(new TextEncoder().encode(PLAINTEXT_M2D));
    expect(reply.counter).toBe(0);
    expect(hex(reply.ciphertext)).toBe(MOBILE_TO_DAEMON_CIPHER0);

    const openedReply = daemonSession.open(0, reply.ciphertext);
    expect(new TextDecoder().decode(openedReply)).toBe(PLAINTEXT_M2D);
  });

  it('rejects tampered ciphertext (authentication actually runs)', () => {
    const daemonSecret = fixtureSeed(0x11);
    const mobileSecret = fixtureSeed(0x22);
    const daemonPub = x25519.getPublicKey(daemonSecret);
    const mobilePub = x25519.getPublicKey(mobileSecret);
    const daemonSession = Session.derive(daemonSecret, mobilePub, Role.Daemon, daemonPub, mobilePub);
    const mobileSession = Session.derive(mobileSecret, daemonPub, Role.Mobile, daemonPub, mobilePub);

    const { ciphertext } = daemonSession.seal(new TextEncoder().encode('hi'));
    const tampered = new Uint8Array(ciphertext);
    tampered[0] ^= 0xff;
    expect(() => mobileSession.open(0, tampered)).toThrow();
  });

  it('rejects a replayed counter', () => {
    const daemonSecret = fixtureSeed(0x11);
    const mobileSecret = fixtureSeed(0x22);
    const daemonPub = x25519.getPublicKey(daemonSecret);
    const mobilePub = x25519.getPublicKey(mobileSecret);
    const daemonSession = Session.derive(daemonSecret, mobilePub, Role.Daemon, daemonPub, mobilePub);
    const mobileSession = Session.derive(mobileSecret, daemonPub, Role.Mobile, daemonPub, mobilePub);

    const { ciphertext } = daemonSession.seal(new TextEncoder().encode('one'));
    mobileSession.open(0, ciphertext);
    expect(() => mobileSession.open(0, ciphertext)).toThrow(/replayed or out of order/);
  });

  it('encodes a hello frame byte-identical to the Go fixture', () => {
    const daemonSecret = fixtureSeed(0x11);
    const daemonPub = x25519.getPublicKey(daemonSecret);
    const payload = new Uint8Array(1 + 1 + 32);
    payload[0] = PROTOCOL_VERSION;
    payload[1] = Role.Daemon;
    payload.set(daemonPub, 2);
    const frame = encodeFrame(0x01, payload);
    expect(hex(frame)).toBe(DAEMON_HELLO_FRAME_HEX);
  });
});
