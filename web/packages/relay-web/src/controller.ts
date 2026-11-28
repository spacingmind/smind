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

/** The pasted pairing link is not usable; the message is fixed and never quotes the link. */
export class InvalidOfferError extends Error {
  constructor() {
    super('this is not a valid pairing link');
    this.name = 'InvalidOfferError';
  }
}

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
  /** The last state that came from the connection or a finished operation (not the optimistic 'connecting'). */
  private real: ControllerState = { kind: 'unpaired' };
  private listeners = new Set<(s: ControllerState) => void>();
  /** Tail of the mutex: pair, resume, unpair and close each start after the previous settles. */
  private chain: Promise<void> = Promise.resolve();
  /** Operations queued or running; while any is, connection events are recorded but not announced. */
  private busy = 0;

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

  pair(offerUrl: string, label: string): Promise<void> {
    let offer: ReturnType<typeof parsePairingURL>;
    try {
      offer = parsePairingURL(offerUrl);
    } catch {
      // The parser's messages can quote part of the decoded payload (which holds the admission secret): say nothing of it.
      return Promise.reject(new InvalidOfferError());
    }
    return this.run(async () => {
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
      this.start(offerUrl, identity, label);
    }, true);
  }

  resume(): Promise<void> {
    return this.run(async () => {
      try {
        const { identity, pairing } = await loadIdentityForConnect(this.o.store, this.o.subtle);
        this.start(pairing.offerUrl, identity, pairing.label);
      } catch (e) {
        if (e instanceof RepairRequiredError || e instanceof NotPairedError) {
          this.detach();
          this.settle(e instanceof RepairRequiredError ? { kind: 'needs-repair' } : { kind: 'unpaired' });
        }
        throw e;
      }
    }, true);
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

  unpair(): Promise<void> {
    return this.run(async () => {
      this.detach();
      await this.o.store.deletePairing();
      await this.o.store.deleteIdentity();
      this.settle({ kind: 'unpaired' });
    }, false);
  }

  close(): Promise<void> {
    return this.run(async () => {
      this.detach();
      this.settle({ kind: 'unpaired' });
    }, false);
  }

  /** Queues fn behind every earlier operation; `optimistic` reports 'connecting' at once (nothing says connected while a connection is being replaced). */
  private run(fn: () => Promise<void>, optimistic: boolean): Promise<void> {
    this.busy++;
    if (optimistic) this.set({ kind: 'connecting' });
    const result = this.chain.then(fn);
    this.chain = result.then(
      () => undefined,
      () => undefined,
    );
    return result.then(
      () => {
        this.busy--;
      },
      (e: unknown) => {
        this.busy--;
        // A failed operation leaves the previous connection (if any) in place: report what it is doing.
        if (this.busy === 0 && optimistic) this.set(this.real);
        throw e;
      },
    );
  }

  private settle(s: ControllerState): void {
    this.real = s;
    this.set(s);
  }

  private start(offerUrl: string, identity: Parameters<typeof RelayConnection.open>[1]['identity'], label: string): void {
    this.detach();
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
      if (this.conn !== conn) return;
      // detach() unhooks this listener before closing, so a 'closed' that arrives
      // here is one the controller did not ask for.
      const next: ControllerState =
        s.kind === 'closed' ? { kind: 'failed', message: 'the relay connection closed' } : mapState(s);
      this.real = next;
      if (this.busy === 0) this.set(next);
    });
    // A queued operation behind this one has already reported 'connecting'; it replaces this connection next.
    this.real = mapState(conn.state);
    if (this.busy <= 1) this.set(this.real);
  }

  private detach(): void {
    this.offConn?.();
    this.offConn = null;
    this.conn?.close();
    this.conn = null;
  }

  private set(s: ControllerState): void {
    this.current = s;
    for (const cb of [...this.listeners]) {
      try {
        cb(s);
      } catch {
        // a throwing listener must not affect the others or the connection
      }
    }
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
