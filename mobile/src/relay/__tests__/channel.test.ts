// channel.test.ts exercises the client Channel against the test-only v2
// responder over an in-memory transport pair (the TS equivalent of
// internal/relay/e2ee/pipe_test.go's happy path): handshake with the pairing
// key pinned, then data both ways. The wire-exact and negative coverage is in
// e2ee.test.ts.

import { describe, expect, it } from 'vitest';
import { channelPair, daemonKeysFrom } from './fakeV2Daemon';

describe('Channel handshake + send/receive', () => {
  it('completes a handshake and exchanges messages each way, verifying the pairing-offer key', async () => {
    const keys = daemonKeysFrom();
    const { channel, daemon } = channelPair(keys);
    await Promise.all([daemon.handshake(), channel.handshake(keys.publicKey)]);

    daemon.send(new TextEncoder().encode('workspace.list request'));
    expect(new TextDecoder().decode(await channel.receive())).toBe('workspace.list request');

    channel.send(new TextEncoder().encode('workspace.list response'));
    expect(new TextDecoder().decode(await daemon.receive())).toBe('workspace.list response');
  });

  it('rejects a handshake against the wrong daemon public key, before the responder sees an R_c', async () => {
    const { channel, daemon } = channelPair();
    const daemonSide = daemon.handshake().catch(() => undefined);
    await expect(channel.handshake(daemonKeysFrom().publicKey)).rejects.toMatchObject({ kind: 'peer_key_mismatch' });
    // The responder is left waiting for an R_c that never comes.
    await Promise.race([daemonSide, new Promise((r) => setTimeout(r, 30))]);
  });
});
