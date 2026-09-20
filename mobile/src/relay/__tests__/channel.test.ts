// channel.test.ts exercises the full Channel class (handshake + send/
// receive) over an in-memory pair of RawTransports, the TS equivalent of
// internal/relay/e2ee/pipe_test.go and handshake_test.go's happy-path
// coverage -- proves the frame state machine (not just Session's crypto)
// works end-to-end between two Channel instances.

import { describe, expect, it } from 'vitest';
import { Channel, generateKeyPair, RawTransport, Role } from '../e2ee';

/** Two connected in-memory RawTransports, each pushing to the other's receive queue. */
function pipe(): [RawTransport, RawTransport] {
  const queueA: Uint8Array[] = [];
  const queueB: Uint8Array[] = [];
  const waitersA: Array<(v: Uint8Array) => void> = [];
  const waitersB: Array<(v: Uint8Array) => void> = [];

  function makeSide(sendQueue: Uint8Array[], sendWaiters: Array<(v: Uint8Array) => void>, recvQueue: Uint8Array[], recvWaiters: Array<(v: Uint8Array) => void>): RawTransport {
    return {
      send(bytes: Uint8Array) {
        const waiter = sendWaiters.shift();
        if (waiter) waiter(bytes);
        else sendQueue.push(bytes);
      },
      receiveChunk(): Promise<Uint8Array> {
        const chunk = recvQueue.shift();
        if (chunk) return Promise.resolve(chunk);
        return new Promise((resolve) => recvWaiters.push(resolve));
      },
    };
  }

  const a = makeSide(queueA, waitersA, queueB, waitersB);
  const b = makeSide(queueB, waitersB, queueA, waitersA);
  return [a, b];
}

describe('Channel handshake + send/receive', () => {
  it('completes a handshake and exchanges one message each way, verifying the pairing-offer key', async () => {
    const [aTransport, bTransport] = pipe();
    const daemonKeyPair = generateKeyPair();
    const mobileKeyPair = generateKeyPair();

    const daemonChannel = new Channel(aTransport, daemonKeyPair, Role.Daemon);
    const mobileChannel = new Channel(bTransport, mobileKeyPair, Role.Mobile);

    await Promise.all([daemonChannel.handshake(), mobileChannel.handshake(daemonKeyPair.publicKey)]);

    daemonChannel.send(new TextEncoder().encode('workspace.list request'));
    const received = await mobileChannel.receive();
    expect(new TextDecoder().decode(received)).toBe('workspace.list request');

    mobileChannel.send(new TextEncoder().encode('workspace.list response'));
    const reply = await daemonChannel.receive();
    expect(new TextDecoder().decode(reply)).toBe('workspace.list response');
  });

  it('rejects a handshake against the wrong daemon public key', async () => {
    const [aTransport, bTransport] = pipe();
    const daemonKeyPair = generateKeyPair();
    const mobileKeyPair = generateKeyPair();
    const wrongKeyPair = generateKeyPair();

    const daemonChannel = new Channel(aTransport, daemonKeyPair, Role.Daemon);
    const mobileChannel = new Channel(bTransport, mobileKeyPair, Role.Mobile);

    const daemonHandshake = daemonChannel.handshake();
    await expect(mobileChannel.handshake(wrongKeyPair.publicKey)).rejects.toThrow(/does not match the pairing offer/);
    // Let the daemon side's handshake settle (it will hang waiting for a
    // ready it'll never get, in this specific rejection scenario) --
    // avoid an unhandled rejection/hang in the test itself.
    await Promise.race([daemonHandshake.catch(() => undefined), new Promise((r) => setTimeout(r, 50))]);
  });
});
