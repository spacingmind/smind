// keepalive.ts is the TypeScript mirror of internal/relay/client/keepalive.go
// (ADR-0031 D9, flow 8): application-level liveness for one OpenData stream.
//
// A proxy in front of the relay (Cloudflare: 125 s read timeout) drops a
// stream that carries no bytes, so a connected session sends a keepalive
// Frame every 30 s; the relay consumes it and answers with a keepalive frame
// carrying the same sequence on the same stream. A stream the relay has said
// nothing on for 75 s is dead and the caller reconnects instead of waiting on
// a connection nothing flows through.
//
// It is negotiated by probing: a relay from before this feature would
// forward the probe to the peer instead of echoing it, so the first probe has
// to be echoed within the probe timeout or keepalive stays off for that
// stream. The probe carries a per-connection random nonce in Frame.sequence;
// only an echo with OUR nonce proves support (a keepalive frame with
// somebody else's nonce is a peer's probe a relay without the feature merely
// forwarded, which counts as liveness but not as support).

import { Frame } from './proto';

export interface KeepaliveTiming {
  /** How often an idle stream probes the relay (30 s). */
  intervalMs: number;
  /** How long a stream may stay silent before it is declared dead (75 s). */
  deadAfterMs: number;
  /** How long the first probe may go unanswered before the relay is assumed not to support keepalive (10 s). */
  probeTimeoutMs: number;
}

export const DEFAULT_KEEPALIVE: KeepaliveTiming = {
  intervalMs: 30_000,
  deadAfterMs: 75_000,
  probeTimeoutMs: 10_000,
};

type Support = 'unknown' | 'yes' | 'no';

/** A random probe nonce in [1, 2^52): exact as a JS number and as a protobuf uint64. */
export function randomProbeNonce(): number {
  const words = new Uint32Array(2);
  for (;;) {
    crypto.getRandomValues(words);
    const nonce = (words[0] & 0xfffff) * 2 ** 32 + words[1];
    if (nonce !== 0) return nonce;
  }
}

/**
 * Keepalive drives probing and the dead-stream rule for one stream. Create it
 * when the stream opens (after the route registration frame), feed it every
 * frame the relay sends with noteFrame(), and stop() it when the stream ends.
 */
export class Keepalive {
  private support: Support = 'unknown';
  private lastRecv: number;
  private readonly startedAt: number;
  private nextSend: number;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private stopped = false;
  readonly nonce: number;

  constructor(
    private readonly timing: KeepaliveTiming,
    private readonly sendProbe: (nonce: number) => void,
    private readonly onDead: () => void,
    nonce: number = randomProbeNonce(),
  ) {
    this.nonce = nonce;
    this.startedAt = Date.now();
    this.lastRecv = this.startedAt;
    this.nextSend = this.startedAt; // first probe immediately
    this.tick();
  }

  /** Records that the relay sent a frame: any frame is evidence the stream is alive; our own nonce echoed back proves the relay supports keepalive. */
  noteFrame(frame: Frame): void {
    this.lastRecv = Date.now();
    if (frame.keepalive && frame.sequence === this.nonce && this.support !== 'yes') {
      this.support = 'yes';
      this.reschedule();
    }
  }

  /** Whether the relay has proven it echoes keepalive probes on this stream. */
  get supported(): boolean {
    return this.support === 'yes';
  }

  stop(): void {
    this.stopped = true;
    if (this.timer !== null) clearTimeout(this.timer);
    this.timer = null;
  }

  private reschedule(): void {
    if (this.stopped) return;
    if (this.timer !== null) clearTimeout(this.timer);
    this.timer = null;
    this.tick();
  }

  private tick(): void {
    if (this.stopped) return;
    this.timer = null;
    const now = Date.now();

    if (this.support === 'no') return;
    if (this.support === 'unknown' && now >= this.startedAt + this.timing.probeTimeoutMs) {
      // Never echoed: the relay predates keepalive. Not a verdict on the
      // relay for good -- the next stream probes again.
      this.support = 'no';
      return;
    }
    if (this.support === 'yes' && now >= this.lastRecv + this.timing.deadAfterMs) {
      this.stopped = true;
      this.onDead();
      return;
    }
    if (now >= this.nextSend) {
      this.nextSend = now + this.timing.intervalMs;
      try {
        this.sendProbe(this.nonce);
      } catch {
        // A send on a stream that is going down: the receive side reports it.
      }
    }

    // Sleep until the earliest of: next probe, the probe deadline, the dead line.
    let wake = this.nextSend;
    if (this.support === 'unknown') wake = Math.min(wake, this.startedAt + this.timing.probeTimeoutMs);
    if (this.support === 'yes') wake = Math.min(wake, this.lastRecv + this.timing.deadAfterMs);
    this.timer = setTimeout(() => this.tick(), Math.max(0, wake - now));
  }
}
