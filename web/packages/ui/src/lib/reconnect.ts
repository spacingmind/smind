// Automatic reconnect for a browser WsClient, built as a thin wrapper
// around it rather than inside the WsClient class itself -- see
// docs/plans/active/daemon-restart-resync.md's Decisions for why: the
// resync mechanism this backs (App.tsx swapping in a genuinely new
// WsClient instance so every hook keyed on the `client` reference re-runs)
// requires a *new* WsClient per reconnect, not the same instance quietly
// re-plumbed to a new socket. Keeping this logic here also leaves
// ws-client.ts focused on the RPC engine, matching that file's own scope
// (see its header comment).
//
// The initial connect is deliberately NOT this module's job -- App.tsx
// keeps doing that itself, unchanged, so an initial-connect failure keeps
// behaving exactly as it did before this file existed (see the plan's
// "Initial connect failure still shows the existing disconnected/error
// state" test scenario). watchForReconnect takes over only once a first
// WsClient already exists, and re-arms itself on every successful
// reconnect so the loop continues indefinitely.
import * as daemonLib from "./daemon";
import type { WsClient } from "./ws-client";

/** Real-time connection status, driven by actual socket events -- not just the initial connect promise. */
export type ConnectionStatus = "connecting" | "connected" | "reconnecting" | "disconnected";

/**
 * Exponential backoff with full jitter: each retry waits a random delay in
 * [0, cap], where cap starts at initialDelayMs and doubles (by factor) up
 * to maxDelayMs. Concrete defaults (documented in the plan's Decisions):
 * initialDelayMs 500, maxDelayMs 10_000, factor 2 -- a daemon restart
 * during a graceful deploy is expected to take low single-digit seconds,
 * so the first couple of retries land quickly, while the 10s cap keeps a
 * longer outage from hammering the daemon with reconnect attempts.
 */
export interface BackoffOptions {
  initialDelayMs?: number;
  maxDelayMs?: number;
  factor?: number;
}

const DEFAULT_BACKOFF: Required<BackoffOptions> = {
  initialDelayMs: 500,
  maxDelayMs: 10_000,
  factor: 2,
};

export interface ReconnectOptions {
  /**
   * Invoked with each newly-established WsClient produced by a successful
   * reconnect (never for the initial connect -- the caller already has
   * that client, since it's what's passed into watchForReconnect).
   */
  onClient: (client: WsClient) => void;
  /** Invoked whenever the connection's status changes ("reconnecting" as soon as the break is detected, "connected" once a redial succeeds). */
  onStatusChange?: (status: ConnectionStatus) => void;
  /**
   * Performs one full connect attempt (fetch a fresh token, dial the
   * socket) -- defaults to daemon.ts's connectDaemon, which refetches
   * /api/token every call by construction (no caching), satisfying the
   * "token is refetched on each reconnect attempt" requirement for free.
   * Overridable for tests so they don't need a real fetch/WebSocket.
   */
  connect?: () => Promise<WsClient>;
  backoff?: BackoffOptions;
}

export interface ReconnectHandle {
  /**
   * Tears down the reconnect loop permanently: cancels any pending retry
   * timer and closes the current client via its own explicit close() --
   * which is the "explicit close never triggers a reconnect attempt" case,
   * since the teardown flag is set before that close() call fires the
   * underlying onClose hook.
   */
  close(): void;
}

/** delay*(1+factor^n) capped at maxDelayMs, then a uniform-random draw in [0, cap] (full jitter, per AWS's backoff-strategies writeup). */
function nextDelay(attempt: number, backoff: Required<BackoffOptions>): number {
  const cap = Math.min(backoff.maxDelayMs, backoff.initialDelayMs * Math.pow(backoff.factor, attempt));
  return Math.random() * cap;
}

/**
 * Watches `client` for an unexpected close and, once one happens, redials
 * indefinitely (fresh token each attempt, exponential backoff with full
 * jitter between attempts) until a new client connects -- then watches
 * *that* one the same way, so the loop continues across any number of
 * reconnects. Never gives up on its own; only `close()` stops it, per the
 * plan's "no give up after N tries" requirement (a daemon restart is
 * expected to be transient, and there is no more useful fallback state
 * than "still trying").
 */
export function watchForReconnect(client: WsClient, opts: ReconnectOptions): ReconnectHandle {
  const connect = opts.connect ?? (() => daemonLib.connectDaemon());
  const backoff: Required<BackoffOptions> = { ...DEFAULT_BACKOFF, ...opts.backoff };

  let current = client;
  let tornDown = false;
  let retryTimer: ReturnType<typeof setTimeout> | undefined;

  function armCloseWatch(c: WsClient): void {
    c.onClose(() => {
      if (tornDown || c !== current) return;
      opts.onStatusChange?.("reconnecting");
      attemptReconnect(0);
    });
  }

  function attemptReconnect(attempt: number): void {
    retryTimer = setTimeout(() => {
      if (tornDown) return;
      connect()
        .then((next) => {
          if (tornDown) {
            next.close();
            return;
          }
          current = next;
          armCloseWatch(next);
          opts.onStatusChange?.("connected");
          opts.onClient(next);
        })
        .catch(() => {
          if (tornDown) return;
          attemptReconnect(attempt + 1);
        });
    }, nextDelay(attempt, backoff));
  }

  armCloseWatch(client);

  return {
    close(): void {
      tornDown = true;
      if (retryTimer !== undefined) clearTimeout(retryTimer);
      current.close();
    },
  };
}
