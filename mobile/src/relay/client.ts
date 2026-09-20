// client.ts orchestrates the milestone's single concrete "it works" proof
// (docs/plans/active/mobile-app-milestone-1.md's Item 3 Decisions): admit
// to the relay -> open the E2EE-layered OpenData stream -> handshake ->
// send one workspace.list request -> render the raw JSON response.

import { computeHMAC, hashSecret, PROTOCOL_VERSION, randomNonce } from './admission';
import { Channel, generateKeyPair, Role } from './e2ee';
import { GRPCWebSocketStream, grpcWebUnary } from './grpcweb';
import { PairingOffer, parsePairingURL } from './pairing';
import {
  decodeAdmitChallengeResponse,
  decodeAdmitResponse,
  decodeFrame,
  Direction,
  encodeAdmitChallengeRequest,
  encodeAdmitRequest,
  encodeFrame,
  Frame,
} from './proto';

/** The daemon-side fixed session identity this milestone bridges (see internal/relay/bridge's DefaultSessionID/DefaultDeviceID doc comment for why). */
const DEFAULT_SESSION_ID = 'm1-default-session';
const DEFAULT_DEVICE_ID = 'm1-default-device';
const MOBILE_DAEMON_KEY_ID = 'mobile-app-milestone-1';

export interface ConnectResult {
  offer: PairingOffer;
  workspaceListJSON: string;
}

function hexEncode(bytes: Uint8Array): string {
  return Array.from(bytes)
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');
}

async function admit(baseUrl: string, workspaceId: string, secret: Uint8Array): Promise<Uint8Array> {
  const clientNonce = randomNonce();
  const challenge = await grpcWebUnary(
    baseUrl,
    'AdmitChallenge',
    encodeAdmitChallengeRequest({
      protocolVersion: PROTOCOL_VERSION,
      workspaceId,
      clientNonce,
      daemonKeyId: MOBILE_DAEMON_KEY_ID,
    }),
    decodeAdmitChallengeResponse
  );

  const hmacValue = computeHMAC(hashSecret(secret), {
    protocolVersion: PROTOCOL_VERSION,
    workspaceId,
    clientNonce,
    serverNonce: challenge.serverNonce,
    daemonKeyId: MOBILE_DAEMON_KEY_ID,
  });

  const admitResp = await grpcWebUnary(
    baseUrl,
    'Admit',
    encodeAdmitRequest({
      protocolVersion: PROTOCOL_VERSION,
      workspaceId,
      clientNonce,
      daemonKeyId: MOBILE_DAEMON_KEY_ID,
      serverNonce: challenge.serverNonce,
      hmac: hmacValue,
    }),
    decodeAdmitResponse
  );
  if (admitResp.admissionId.length === 0) throw new Error('admit: relay returned an empty admission id');
  return admitResp.admissionId;
}

function wsBaseURL(httpsBaseUrl: string): string {
  if (httpsBaseUrl.startsWith('https://')) return 'wss://' + httpsBaseUrl.slice('https://'.length);
  if (httpsBaseUrl.startsWith('http://')) return 'ws://' + httpsBaseUrl.slice('http://'.length);
  throw new Error(`relay URL must be http(s)://, got ${httpsBaseUrl}`);
}

/**
 * Adapts one GRPCWebSocketStream (relay.v1.Frame messages) to e2ee's
 * RawTransport (opaque byte chunks) -- the TypeScript mirror of Go's
 * frameConn (internal/relay/client/client.go), one layer below the E2EE
 * Channel.
 */
function frameTransport(stream: GRPCWebSocketStream, workspaceId: string, sessionId: string, deviceId: string) {
  let seq = 0;
  return {
    send(bytes: Uint8Array): void {
      const frame: Frame = {
        workspaceId,
        sessionId: new TextEncoder().encode(sessionId),
        deviceId,
        direction: Direction.DEVICE_TO_DAEMON,
        sequence: seq++,
        payload: bytes,
      };
      stream.send(encodeFrame(frame));
    },
    receiveChunk(): Promise<Uint8Array> {
      return stream.receiveMessage(decodeFrame).then((f) => f.payload);
    },
  };
}

/**
 * connectAndFetchWorkspaceList runs the full chain a tap on "Connect"
 * needs: parse the pairing offer, admit to the relay, open the bridged
 * E2EE data session as the mobile role, complete the handshake (verifying
 * the daemon's public key against the one the offer carries), send one
 * workspace.list request, and return the raw JSON response text.
 */
export async function connectAndFetchWorkspaceList(pairingUrl: string): Promise<ConnectResult> {
  const offer = parsePairingURL(pairingUrl);
  if (!offer.workspaceId) throw new Error('pairing offer has no workspace id');
  if (offer.secret.length === 0) throw new Error('pairing offer has no admission secret');

  const admissionId = await admit(offer.relay, offer.workspaceId, offer.secret);

  const stream = new GRPCWebSocketStream(`${wsBaseURL(offer.relay)}/relay.v1.Relay/OpenData`, {
    'admission-id': hexEncode(admissionId),
  });
  await stream.ready();

  const transport = frameTransport(stream, offer.workspaceId, DEFAULT_SESSION_ID, DEFAULT_DEVICE_ID);
  // The relay derives a stream's route from its first frame, so an empty
  // registration frame is sent immediately -- internal/relay/client's
  // frameConn.register does the same thing on the Go side.
  transport.send(new Uint8Array(0));

  const mobileKeyPair = generateKeyPair();
  const channel = new Channel(transport, mobileKeyPair, Role.Mobile);
  await channel.handshake(offer.publicKey);

  channel.send(new TextEncoder().encode(JSON.stringify({ id: '1', method: 'workspace.list' })));
  const response = await channel.receive();
  const workspaceListJSON = new TextDecoder().decode(response);

  stream.close();
  return { offer, workspaceListJSON };
}
