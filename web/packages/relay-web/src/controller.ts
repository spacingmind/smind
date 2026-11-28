// controller.ts owns the browser's one relay connection: it pairs (the only
// place a device key is created), resumes from the stored pairing, maps the
// connection's lifecycle to a UI-facing state and hands out RelaySockets.
// It installs nothing global and logs nothing.

import {
  RelayConnection,
  type LivenessTiming,
  type RelayNet,
  type ReconnectTiming,
} from '@smind/relay-client/src/RelayConnection';
import type { KeepaliveTiming } from '@smind/relay-client/src/keepalive';
import { parsePairingURL } from '@smind/relay-client/src/pairing';
import { loadIdentityForConnect, newIdentityForPairing, NotPairedError, RepairRequiredError } from './identity-manager';
import { RelaySocket } from './relay-socket';
import type { RelayStore } from './store';

export type ControllerState =
  | { kind: 'unpaired' }
  | { kind: 'needs-repair' }
  | { kind: 'connecting' }
  | { kind: 'connected' }
  | { kind: 'reconnecting'; attempt: number }
  | { kind: 'pending'; deviceId: string; label: string }
  | { kind: 'revoked' }
  | { kind: 'password'; reason: 'required' | 'incorrect' }
  | { kind: 'failed'; message: string; versionMismatch?: boolean };

/** socket()/whenConnected() could not get a connected relay: `state` says why. */
export class RelayStateError extends Error {
  constructor(readonly state: ControllerState) {
    super(`relay is not available: ${state.kind}`);
    this.name = 'RelayStateError';
  }
}

export interface ControllerOptions {
  store: RelayStore;
  net?: RelayNet;
  subtle?: SubtleCrypto;
  open?: typeof RelayConnection.open;
  label?: string;
  timings?: Partial<{
    reconnect: Partial<ReconnectTiming>;
    keepalive: Partial<KeepaliveTiming>;
    liveness: Partial<LivenessTiming>;
    jitter: (ms: number) => number;
  }>;
}

type ConnState = RelayConnection['state'];

function mapState(s: ConnState): ControllerState {
  switch (s.kind) {
    case 'connecting':
      return { kind: 'connecting' };
    case 'connected':
      return { kind: 'connected' };
    case 'reconnecting':
      return { kind: 'reconnecting', attempt: s.attempt };
    case 'pending':
      return { kind: 'pending', deviceId: s.deviceId, label: s.label };
    case 'password':
      return { kind: 'password', reason: s.reason };
    case 'locked':
      return { kind: 'connecting' }; // waiting out the daemon's password rate limit
    case 'revoked':
      return { kind: 'revoked' };
    case 'failed':
      return s.versionMismatch
        ? { kind: 'failed', message: s.message, versionMismatch: true }
        : { kind: 'failed', message: s.message };
    case 'closed':
      return { kind: 'unpaired' };
  }
}

// States that end a wait for the connection. pending (not approved yet) and
// password (the person must type one) are NOT in it: like
// RelayConnection.whenConnected, a wait goes on through them; socket() alone
// refuses at once when it is called while the device is in one of them.
const TERMINAL = new Set(['revoked', 'failed', 'needs-repair', 'unpaired']);
const NEEDS_PERSON = new Set(['pending', 'password']);

export class RelayController {
  private readonly o: ControllerOptions;
  private conn: RelayConnection | null = null;
  private offConn: (() => void) | null = null;
  private current: ControllerState = { kind: 'unpaired' };
  private listeners = new Set<(s: ControllerState) => void>();

  constructor(opts: ControllerOptions) {
    this.o = opts;
  }

  get state(): ControllerState {
    return this.current;
  }

  onState(cb: (s: ControllerState) => void): () => void {
    this.listeners.add(cb);
    return () => this.listeners.delete(cb);
  }

  async pair(offerUrl: string, label: string): Promise<void> {
    const offer = parsePairingURL(offerUrl);
    const { record, identity } = await newIdentityForPairing(this.o.subtle);
    await this.o.store.savePaired(record, {
      offerUrl,
      daemonId: offer.daemonId,
      relay: offer.relay,
      relayFingerprint: offer.relayFingerprint,
      label,
      pairedAt: Date.now(),
    });
    // Only now is the previous connection replaced: any failure above leaves the
    // old identity, the old pairing and the old connection as they were.
    this.detach();
    this.start(offerUrl, identity, label);
  }

  async resume(): Promise<void> {
    this.detach();
    try {
      const { identity, pairing } = await loadIdentityForConnect(this.o.store, this.o.subtle);
      this.start(pairing.offerUrl, identity, pairing.label);
    } catch (e) {
      if (e instanceof RepairRequiredError) this.set({ kind: 'needs-repair' });
      else if (e instanceof NotPairedError) this.set({ kind: 'unpaired' });
      throw e;
    }
  }

  whenConnected(): Promise<void> {
    return this.waitConnected().then(() => undefined);
  }

  async socket(): Promise<RelaySocket> {
    if (NEEDS_PERSON.has(this.current.kind)) throw new RelayStateError(this.current);
    const conn = await this.waitConnected();
    return new RelaySocket(conn);
  }

  submitPassword(password: string): void {
    this.conn?.submitPassword(password);
  }

  async unpair(): Promise<void> {
    this.detach();
    await this.o.store.deletePairing();
    await this.o.store.deleteIdentity();
    this.set({ kind: 'unpaired' });
  }

  close(): void {
    this.detach();
    this.set({ kind: 'unpaired' });
  }

  private start(offerUrl: string, identity: Parameters<typeof RelayConnection.open>[1]['identity'], label: string): void {
    const open = this.o.open ?? RelayConnection.open;
    const t = this.o.timings ?? {};
    const conn = open(offerUrl, {
      identity,
      label: this.o.label ?? label,
      net: this.o.net,
      reconnect: t.reconnect,
      keepalive: t.keepalive,
      liveness: t.liveness,
      jitter: t.jitter,
    });
    this.conn = conn;
    this.offConn = conn.onState((s) => {
      if (this.conn === conn && s.kind !== 'closed') this.set(mapState(s));
    });
    this.set(mapState(conn.state));
  }

  private detach(): void {
    this.offConn?.();
    this.offConn = null;
    this.conn?.close();
    this.conn = null;
  }

  private set(s: ControllerState): void {
    this.current = s;
    for (const cb of [...this.listeners]) cb(s);
  }

  private waitConnected(): Promise<RelayConnection> {
    return new Promise((resolve, reject) => {
      const check = (s: ControllerState): boolean => {
        if (s.kind === 'connected' && this.conn) {
          resolve(this.conn);
          return true;
        }
        if (TERMINAL.has(s.kind)) {
          reject(new RelayStateError(s));
          return true;
        }
        return false;
      };
      if (check(this.current)) return;
      const off = this.onState((s) => {
        if (check(s)) off();
      });
    });
  }
}
