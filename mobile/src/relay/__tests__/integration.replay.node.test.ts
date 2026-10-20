// integration.replay.node.test.ts is ADR-0035's live replay test: against a real
// Go relay and a real daemon hub, an attacker who can open a route (the relay
// operator, a host-secret holder) replays a CAPTURED client handshake (M1, and
// then the captured R_c) into a new route. The daemon draws a fresh ephemeral
// per handshake, so everything it sends differs from the original session, and
// since the attacker has no client ephemeral secret it never gets past key
// confirmation: the daemon emits only M2 and R_d and NEVER a data frame (no
// device challenge, no refusal), so there is no keystream to reuse.
//
// Run with: npm run test:integration (from mobile/); needs `go` on PATH.

/// <reference types="node" />
import { afterAll, beforeAll, describe, expect, it } from 'vitest';
import { FRAME_DATA, FRAME_HELLO, FRAME_READY, FrameReader } from '../e2ee';
import { DataStream, GRPCWebSocketStream } from '../grpcweb';
import { parsePairingURL } from '../pairing';
import { decodeFrame, Direction, encodeFrame, Frame } from '../proto';
import { grpcWebRelayNet, RelayConnection, RelayNet } from '../RelayConnection';
import { DevHarness, requireSelfSignedHarnessTrust, testConnectOptions } from './devHarness';

let harness: DevHarness;
const connections: RelayConnection[] = [];

/** The real network, remembering every frame the client SENT (so the test can capture its handshake as a relay operator could). */
class CapturingNet implements RelayNet {
  readonly sent: Frame[] = [];
  /** Frames the daemon sent the device, as the client's own receive path decoded them. */
  readonly received: Frame[] = [];
  admit = grpcWebRelayNet.admit;
  openStream(wsUrl: string, headers: Record<string, string>): DataStream {
    const s = new GRPCWebSocketStream(wsUrl, headers);
    const send = s.send.bind(s);
    s.send = (bytes: Uint8Array) => {
      this.sent.push(decodeFrame(bytes));
      send(bytes);
    };
    const receive = s.receiveMessage.bind(s);
    s.receiveMessage = (async (decode: (b: Uint8Array) => unknown) =>
      receive((b) => {
        this.received.push(decodeFrame(b));
        return decode(b);
      })) as typeof s.receiveMessage;
    return s;
  }
}

beforeAll(async () => {
  requireSelfSignedHarnessTrust();
  harness = await DevHarness.start(1);
}, 90_000);

afterAll(() => {
  for (const c of connections) c.close();
  harness?.stop();
});

const hex = (b: Uint8Array) => Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('');

describe('a captured client handshake replayed into a new route (real relay, real daemon)', () => {
  it('gets a fresh M2 and R_d but never a data frame, even when the captured R_c is replayed too', { timeout: 60_000 }, async () => {
    // 1. A genuine session; the "attacker" records the frames the device sent.
    const net = new CapturingNet();
    const conn = RelayConnection.open(harness.pairingUrl, testConnectOptions(61, { net }));
    connections.push(conn);
    await conn.whenConnected();
    await expect(conn.call('workspace.list')).resolves.toEqual([]);

    const first = net.sent.filter((f) => f.direction === Direction.DEVICE_TO_DAEMON && !f.keepalive && f.payload.length > 0);
    const m1Frame = first[0];
    const rcFrame = first[1];
    expect(m1Frame.payload.length).toBe(4 + 1 + 34); // u32 length + hello type + (version, role, client ephemeral)
    expect(Array.from(m1Frame.payload.slice(4, 7))).toEqual([FRAME_HELLO, 2, 2]);
    expect(rcFrame.payload[4]).toBe(FRAME_READY);
    expect(rcFrame.payload.length).toBe(4 + 1 + 32);
    const deviceId = m1Frame.deviceId;
    // The original session's M2 (the daemon's first non-empty frame: 4 + 1 + 66 bytes).
    const originalM2 = net.received.find((f) => !f.keepalive && f.payload.length > 0)!.payload;
    expect(originalM2.length).toBe(4 + 1 + 66);

    // 2. A new route (new session id, same device id), admitted like any holder of the relay secret.
    const offer = parsePairingURL(harness.pairingUrl);
    const admission = await grpcWebRelayNet.admit(offer.relay, offer.workspaceId, offer.secret, `device-${deviceId}`);
    const ws = (offer.relay.startsWith('https://') ? 'wss://' : 'ws://') + offer.relay.replace(/^https?:\/\//, '');
    const stream = new GRPCWebSocketStream(`${ws}/relay.v1.Relay/OpenData`, { 'admission-id': hex(admission) });
    await stream.ready();
    const sessionId = new TextEncoder().encode('s-replayed-route');
    const out = (payload: Uint8Array, sequence: number) =>
      stream.send(encodeFrame({ workspaceId: offer.workspaceId, sessionId, deviceId, direction: Direction.DEVICE_TO_DAEMON, sequence, payload, keepalive: false }));
    out(new Uint8Array(0), 0); // registers the route
    out(m1Frame.payload, 1); // the captured M1, byte for byte

    // 3. What the daemon sends back, as raw E2EE frames.
    const reader = new FrameReader();
    const received: Array<{ type: number; payload: Uint8Array }> = [];
    const collect = async (ms: number, want: number) => {
      const deadline = Date.now() + ms;
      while (Date.now() < deadline && received.length < want) {
        const f = await Promise.race([
          stream.receiveMessage(decodeFrame).catch(() => null),
          new Promise<null>((r) => setTimeout(() => r(null), Math.max(1, deadline - Date.now()))),
        ]);
        if (!f || f.keepalive || f.payload.length === 0) continue;
        reader.push(f.payload);
        for (let e = reader.tryRead(); e; e = reader.tryRead()) received.push(e);
      }
    };
    await collect(10_000, 2);
    expect(received.map((f) => f.type)).toEqual([FRAME_HELLO, FRAME_READY]); // M2 and R_d, nothing else yet

    // Not a copy of the original session's replies: the daemon ephemeral is fresh (M2 = version, role, daemon_pub, daemon_eph).
    const m2 = received[0].payload;
    expect(m2.length).toBe(66);
    expect(m2[0]).toBe(2);
    expect(m2[1]).toBe(1);
    expect(hex(m2.slice(2, 34))).toBe(hex(originalM2.slice(7, 39))); // the same daemon identity key...
    expect(hex(m2.slice(34))).not.toBe(hex(originalM2.slice(39))); // ...and a different, fresh ephemeral

    // 4. The attacker has no client ephemeral secret: the best it can do is replay the captured R_c.
    out(rcFrame.payload, 2);
    await collect(2_500, 3);
    expect(received.filter((f) => f.type === FRAME_DATA)).toHaveLength(0); // no challenge, no refusal, no data frame, ever
    expect(received.map((f) => f.type)).toEqual([FRAME_HELLO, FRAME_READY]);
    stream.close();
  });
});
