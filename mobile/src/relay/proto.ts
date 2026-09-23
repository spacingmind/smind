// proto.ts hand-encodes/decodes exactly the relay.v1.Relay messages this
// client needs (internal/relay/relaypb/relay.proto), field-for-field.
//
// There's no protoc-generated JS/TS client here on purpose: the message
// set this milestone's mobile client needs (AdmitChallenge, Admit, Frame)
// is small and fixed, and hand-rolling it avoids adding a protoc/codegen
// toolchain to mobile/'s build for five flat messages -- see this
// project's grpc-web Go test (internal/relay/server/grpcweb_test.go) for
// the identical reasoning on the Go test side.

import { ByteWriter, decodeFields, getBytes, getString, getVarint } from './varint';

export const PROTOCOL_VERSION = 1;

export interface AdmitChallengeRequest {
  protocolVersion: number;
  workspaceId: string;
  clientNonce: Uint8Array;
  daemonKeyId: string;
}

export function encodeAdmitChallengeRequest(m: AdmitChallengeRequest): Uint8Array {
  const w = new ByteWriter();
  w.writeVarintField(1, m.protocolVersion);
  w.writeStringField(2, m.workspaceId);
  w.writeBytesField(3, m.clientNonce);
  w.writeStringField(4, m.daemonKeyId);
  return w.finish();
}

export interface AdmitChallengeResponse {
  serverNonce: Uint8Array;
}

export function decodeAdmitChallengeResponse(buf: Uint8Array): AdmitChallengeResponse {
  const f = decodeFields(buf);
  return { serverNonce: getBytes(f, 1) };
}

export interface AdmitRequest {
  protocolVersion: number;
  workspaceId: string;
  clientNonce: Uint8Array;
  daemonKeyId: string;
  serverNonce: Uint8Array;
  hmac: Uint8Array;
}

export function encodeAdmitRequest(m: AdmitRequest): Uint8Array {
  const w = new ByteWriter();
  w.writeVarintField(1, m.protocolVersion);
  w.writeStringField(2, m.workspaceId);
  w.writeBytesField(3, m.clientNonce);
  w.writeStringField(4, m.daemonKeyId);
  w.writeBytesField(5, m.serverNonce);
  w.writeBytesField(6, m.hmac);
  return w.finish();
}

export interface AdmitResponse {
  admissionId: Uint8Array;
}

export function decodeAdmitResponse(buf: Uint8Array): AdmitResponse {
  const f = decodeFields(buf);
  return { admissionId: getBytes(f, 1) };
}

/** Direction enum values, mirroring relay.proto's Direction exactly. */
export const Direction = {
  UNSPECIFIED: 0,
  DAEMON_TO_DEVICE: 1,
  DEVICE_TO_DAEMON: 2,
} as const;

export interface Frame {
  workspaceId: string;
  sessionId: Uint8Array;
  deviceId: string;
  direction: number;
  sequence: number;
  payload: Uint8Array;
}

export function encodeFrame(m: Frame): Uint8Array {
  const w = new ByteWriter();
  w.writeStringField(1, m.workspaceId);
  w.writeBytesField(2, m.sessionId);
  w.writeStringField(3, m.deviceId);
  w.writeVarintField(4, m.direction);
  w.writeVarintField(5, m.sequence);
  w.writeBytesField(6, m.payload);
  return w.finish();
}

export function decodeFrame(buf: Uint8Array): Frame {
  const f = decodeFields(buf);
  return {
    workspaceId: getString(f, 1),
    sessionId: getBytes(f, 2),
    deviceId: getString(f, 3),
    direction: getVarint(f, 4),
    sequence: getVarint(f, 5),
    payload: getBytes(f, 6),
  };
}
