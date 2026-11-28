// pairing.ts parses the pairing deep link internal/relay/pairing/offer.go
// produces: the payload lives only in the URL fragment (never a query
// string -- see offer.go's package doc comment on why), base64url-encoded
// JSON.

import { base64UrlDecode } from './base64';

export interface PairingOffer {
  daemonId: string;
  publicKey: Uint8Array;
  relay: string;
  relayFingerprint: string;
  /** See internal/relay/pairing.Offer.Secret's doc comment: a milestone-1
   * simplification, not the long-term design -- required for this
   * client's Admit call, but not a narrowly-scoped per-device credential. */
  secret: Uint8Array;
  /** See internal/relay/pairing.Offer.WorkspaceID's doc comment: the relay
   * workspace this client must Admit under. */
  workspaceId: string;
}

const FRAGMENT_KEY = 'offer';
const OFFER_VERSION = 1;

interface WireOffer {
  v: number;
  id: string;
  pk: string;
  relay: string;
  fp?: string;
  sec?: string;
  ws?: string;
}

export class InvalidOfferError extends Error {}

/** Extracts and decodes a pairing offer from a full pairing URL. */
export function parsePairingURL(raw: string): PairingOffer {
  const hashIndex = raw.indexOf('#');
  if (hashIndex === -1) throw new InvalidOfferError('pairing URL has no fragment');
  const fragment = raw.slice(hashIndex + 1);

  const params = new URLSearchParams(fragment);
  const payload = params.get(FRAGMENT_KEY);
  if (!payload) throw new InvalidOfferError(`pairing URL has no "${FRAGMENT_KEY}" fragment parameter`);

  return decodeOfferPayload(payload);
}

export function decodeOfferPayload(payload: string): PairingOffer {
  let json: string;
  try {
    json = new TextDecoder().decode(base64UrlDecode(payload));
  } catch (e) {
    throw new InvalidOfferError(`decode payload: ${(e as Error).message}`);
  }

  let wire: WireOffer;
  try {
    wire = JSON.parse(json);
  } catch (e) {
    throw new InvalidOfferError(`parse payload: ${(e as Error).message}`);
  }

  if (wire.v !== OFFER_VERSION) {
    throw new InvalidOfferError(`unsupported offer version ${wire.v}`);
  }
  if (!wire.id || !wire.relay || !wire.pk) {
    throw new InvalidOfferError('offer is missing required fields');
  }

  const publicKey = base64UrlDecode(wire.pk);
  if (publicKey.length !== 32) {
    throw new InvalidOfferError(`public key is ${publicKey.length} bytes, want 32`);
  }

  return {
    daemonId: wire.id,
    publicKey,
    relay: wire.relay,
    relayFingerprint: wire.fp ?? '',
    secret: wire.sec ? base64UrlDecode(wire.sec) : new Uint8Array(0),
    workspaceId: wire.ws ?? '',
  };
}
