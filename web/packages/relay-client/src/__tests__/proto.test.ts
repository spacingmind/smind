// proto.test.ts is a self-consistency check for the hand-rolled protobuf
// codec (round-trip encode/decode) plus one cross-language fixture: a
// Frame message encoded exactly as internal/relay/relaypb (protoc-
// generated Go) would, using a literal computed with Go's own
// proto.Marshal so this file's encoder is checked against the real wire
// format, not just against its own decoder.

import { describe, expect, it } from 'vitest';
import { Direction, decodeAdmitResponse, decodeFrame, encodeFrame, Frame } from '../proto';

function fromHex(s: string): Uint8Array {
  const out = new Uint8Array(s.length / 2);
  for (let i = 0; i < out.length; i++) out[i] = parseInt(s.slice(i * 2, i * 2 + 2), 16);
  return out;
}

describe('proto codec round trip', () => {
  it('round-trips a Frame message', () => {
    const frame: Frame = {
      workspaceId: 'ws-1',
      sessionId: new Uint8Array([1, 2, 3, 4]),
      deviceId: 'device-1',
      direction: Direction.DEVICE_TO_DAEMON,
      sequence: 42,
      payload: new TextEncoder().encode('hello'),
      keepalive: false,
    };
    const decoded = decodeFrame(encodeFrame(frame));
    expect(decoded).toEqual(frame);
  });

  it('decodes an AdmitResponse with a real protoc-encoded payload', () => {
    // bytes for AdmitResponse{ AdmissionId: []byte{0xde,0xad,0xbe,0xef} },
    // i.e. field 1 (bytes): tag 0x0a, length 4, then the bytes.
    const wire = new Uint8Array([0x0a, 0x04, 0xde, 0xad, 0xbe, 0xef]);
    const decoded = decodeAdmitResponse(wire);
    expect(Array.from(decoded.admissionId)).toEqual([0xde, 0xad, 0xbe, 0xef]);
  });

  it('omits zero-value fields, matching proto3 semantics', () => {
    const frame: Frame = {
      workspaceId: '',
      sessionId: new Uint8Array(0),
      deviceId: '',
      direction: Direction.UNSPECIFIED,
      sequence: 0,
      payload: new Uint8Array(0),
      keepalive: false,
    };
    expect(encodeFrame(frame).length).toBe(0);
  });

  it('round-trips the keepalive flag and a large probe nonce', () => {
    const frame: Frame = {
      workspaceId: 'ws',
      sessionId: new Uint8Array([1]),
      deviceId: 'd',
      direction: Direction.DEVICE_TO_DAEMON,
      sequence: Number.MAX_SAFE_INTEGER,
      payload: new Uint8Array(0),
      keepalive: true,
    };
    expect(decodeFrame(encodeFrame(frame))).toEqual(frame);
  });

  it('encodes Frame.keepalive (field 7) exactly as the Go relaypb does', () => {
    // Computed with Go's proto.Marshal (Deterministic) for
    // Frame{WorkspaceId:"ws", SessionId:"s1", DeviceId:"dev", Direction:DEVICE_TO_DAEMON,
    //        Sequence:4503599627370495 (2^52-1), Keepalive:true}.
    const goWire = fromHex('0a027773120273311a03646576200228ffffffffffffff073801');
    const frame: Frame = {
      workspaceId: 'ws',
      sessionId: new TextEncoder().encode('s1'),
      deviceId: 'dev',
      direction: Direction.DEVICE_TO_DAEMON,
      sequence: 4503599627370495,
      payload: new Uint8Array(0),
      keepalive: true,
    };
    expect(Array.from(encodeFrame(frame))).toEqual(Array.from(goWire));
    expect(decodeFrame(goWire)).toEqual(frame);
  });

  it('decodes a frame from a peer that predates the keepalive field as keepalive=false', () => {
    const old = decodeFrame(fromHex('0a02777312027331'));
    expect(old.keepalive).toBe(false);
  });
});
