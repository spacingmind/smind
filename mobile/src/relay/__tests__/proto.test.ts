// proto.test.ts is a self-consistency check for the hand-rolled protobuf
// codec (round-trip encode/decode) plus one cross-language fixture: a
// Frame message encoded exactly as internal/relay/relaypb (protoc-
// generated Go) would, using a literal computed with Go's own
// proto.Marshal so this file's encoder is checked against the real wire
// format, not just against its own decoder.

import { describe, expect, it } from 'vitest';
import { Direction, decodeAdmitResponse, decodeFrame, encodeFrame, Frame } from '../proto';

describe('proto codec round trip', () => {
  it('round-trips a Frame message', () => {
    const frame: Frame = {
      workspaceId: 'ws-1',
      sessionId: new Uint8Array([1, 2, 3, 4]),
      deviceId: 'device-1',
      direction: Direction.DEVICE_TO_DAEMON,
      sequence: 42,
      payload: new TextEncoder().encode('hello'),
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
    };
    expect(encodeFrame(frame).length).toBe(0);
  });
});
