// WsClient implements the browser-side counterpart to smind's
// internal/wsapi WebSocket RPC protocol -- see internal/wsapi/wsapi.go's
// doc comment for the wire protocol, and internal/wsclient/wsclient.go for
// the Go client this is a second implementation of. It is not a new
// design: same envelope shape, same semantics (one persistent connection,
// many concurrent in-flight requests correlated by id, a streaming call
// variant whose event callback fires per-event off the wire before its
// terminal result, per-request cancellation via task.cancel without
// closing the connection), translated idiomatically to TS/browser
// WebSocket.
//
// One JSON object is sent per WebSocket text message, in one of four
// shapes:
//   - client -> server request:  {id, method, params}
//   - server -> client response: {id, result} or {id, error: {message}}
//   - server -> client event:    {id, event, params}
//   - client -> server cancel:   {method: "task.cancel", params: {id}}

/** Wire envelope -- mirrors internal/wsapi's envelope exactly. */
export interface WireEnvelope {
  id?: string;
  method?: string;
  params?: unknown;
  result?: unknown;
  error?: { message: string };
  event?: string;
}

/**
 * Thrown when the server's terminal response for a request was an error,
 * as opposed to a transport-level failure (connection lost, cancelled,
 * etc.) -- callers that care about the distinction can check
 * `instanceof RpcError`.
 */
export class RpcError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "RpcError";
  }
}

/**
 * Thrown when a call's AbortSignal fired before its terminal response
 * arrived (mirroring the Go client's ctx.Err() return in the same
 * situation) -- either because the connection died while waiting on the
 * cancel to land, or because the server's own terminal response, once it
 * did land, was itself an error (in which case that error is discarded in
 * favor of reporting the cancellation, exactly like wsclient.go's
 * CallStream does).
 */
export class CallAbortedError extends Error {
  constructor(message = "call aborted") {
    super(message);
    this.name = "CallAbortedError";
  }
}

/**
 * Thrown when a call can't be completed because the connection is
 * (already, or becomes) closed.
 */
export class ConnectionClosedError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ConnectionClosedError";
  }
}

/**
 * Invoked once per server-pushed event a streaming call receives, in
 * arrival order, before its terminal result/error. It must not block for
 * long: it runs synchronously off the socket's message handler, so a slow
 * EventFunc delays delivery of every other in-flight request's messages on
 * the same connection too -- exactly like wsclient.go's EventFunc doc
 * comment warns.
 */
export type EventFunc = (event: string, params: unknown) => void;

/**
 * The minimal surface WsClient needs from a WebSocket-like object --
 * satisfied by the real browser `WebSocket` and by lightweight fakes in
 * tests, so the RPC engine itself (this file) never has to construct a
 * live socket to be tested.
 */
export interface SocketLike {
  send(data: string): void;
  close(): void;
  addEventListener(type: "message", listener: (ev: { data: string }) => void): void;
  addEventListener(type: "close" | "error", listener: () => void): void;
}

interface InflightRequest {
  resolveTerm: (env: WireEnvelope) => void;
  onEvent?: EventFunc;
}

/** Options accepted by WsClient.call/callStream. */
export interface CallOptions {
  /**
   * If provided and it fires before the terminal response arrives, sends
   * task.cancel for this request's own id (not the whole connection) and
   * keeps waiting for its terminal response, so the caller can be sure the
   * request is no longer in flight server-side by the time call/callStream
   * returns -- see wsclient.go's CallStream doc comment.
   */
  signal?: AbortSignal;
}

/**
 * One WebSocket connection to a smind daemon's /ws endpoint, supporting
 * any number of concurrent in-flight requests (each identified by its own
 * request id), matching internal/wsclient.Client's contract.
 */
export class WsClient {
  private readonly socket: SocketLike;
  private readonly inflight = new Map<string, InflightRequest>();
  private nextId = 0;
  private closed = false;
  private closeError: Error | null = null;
  private closeWaiters: Array<() => void> = [];

  /**
   * Wraps an already-connected SocketLike. Prefer `WsClient.connect` to
   * open a real browser WebSocket and wait for it to be usable; this
   * constructor is what makes the RPC engine directly testable against a
   * fake socket without a real network connection.
   */
  constructor(socket: SocketLike) {
    this.socket = socket;
    socket.addEventListener("message", (ev) => this.handleMessage(ev.data));
    socket.addEventListener("close", () => this.failAll(new ConnectionClosedError("wsclient: connection closed")));
    socket.addEventListener("error", () => this.failAll(new ConnectionClosedError("wsclient: connection error")));
  }

  /**
   * Connects to `url` (expected to be a `ws(s)://.../ws?token=...` URL --
   * see `daemonWsUrl`) and resolves once the connection is open and usable,
   * mirroring wsclient.go's Dial.
   */
  static connect(url: string): Promise<WsClient> {
    return new Promise((resolve, reject) => {
      const ws = new WebSocket(url);
      const onOpen = () => {
        ws.removeEventListener("error", onError);
        resolve(new WsClient(ws));
      };
      const onError = () => {
        ws.removeEventListener("open", onOpen);
        reject(new ConnectionClosedError(`wsclient: failed to connect to ${url}`));
      };
      ws.addEventListener("open", onOpen, { once: true });
      ws.addEventListener("error", onError, { once: true });
    });
  }

  /** Closes the underlying connection; any request still in flight fails. */
  close(): void {
    this.socket.close();
  }

  /**
   * Issues method with params and resolves with its terminal result once
   * it arrives. Only use this for methods that don't stream events --
   * use callStream for those.
   */
  call<TResult = unknown>(method: string, params?: unknown, options?: CallOptions): Promise<TResult> {
    return this.callStream<TResult>(method, params, undefined, options);
  }

  /**
   * Like call, but onEvent is invoked for every server-pushed event the
   * request receives before its terminal response, in arrival order and as
   * each is received off the wire (not buffered until the terminal
   * response) -- this is what makes real incremental streaming possible.
   */
  callStream<TResult = unknown>(
    method: string,
    params?: unknown,
    onEvent?: EventFunc,
    options?: CallOptions,
  ): Promise<TResult> {
    const id = String(++this.nextId);

    if (this.closed) {
      return Promise.reject(
        new ConnectionClosedError(`wsclient: connection closed: ${this.closeError?.message ?? "unknown"}`),
      );
    }

    const term = new Promise<WireEnvelope>((resolveTerm) => {
      this.inflight.set(id, { resolveTerm, onEvent });
    });

    try {
      this.send({ id, method, params: params ?? null });
    } catch (err) {
      this.inflight.delete(id);
      return Promise.reject(err);
    }

    const signal = options?.signal;
    if (!signal) {
      return term.then((env) => decodeTerminal<TResult>(env));
    }

    return this.awaitWithCancel<TResult>(id, term, signal);
  }

  /**
   * Races term against signal firing. If signal fires first, sends
   * task.cancel for id and keeps waiting for term (or the connection
   * closing) before resolving, matching wsclient.go's CallStream: if the
   * server's own completion raced the cancel and won, the real result
   * (success or not) is what decides the outcome only when it's a
   * success -- an error terminal response after a cancel is reported as
   * CallAbortedError instead, same as ctx.Err() there.
   */
  private async awaitWithCancel<TResult>(
    id: string,
    term: Promise<WireEnvelope>,
    signal: AbortSignal,
  ): Promise<TResult> {
    const aborted = waitForAbort(signal);
    const first = await Promise.race([
      term.then((env) => ({ kind: "term" as const, env })),
      aborted.then(() => ({ kind: "aborted" as const })),
    ]);

    if (first.kind === "term") {
      return decodeTerminal<TResult>(first.env);
    }

    // signal fired first: cancel just this request, not the connection.
    try {
      this.send({ method: "task.cancel", params: { id } });
    } catch {
      // Connection is already gone; failAll will have delivered (or will
      // shortly deliver) a terminal envelope to `term` below.
    }

    const closed = this.waitClosed();
    const second = await Promise.race([
      term.then((env) => ({ kind: "term" as const, env })),
      closed.then(() => ({ kind: "closed" as const })),
    ]);

    if (second.kind === "closed") {
      throw new CallAbortedError();
    }
    try {
      return decodeTerminal<TResult>(second.env);
    } catch {
      // The terminal response, once it arrived, was itself an error --
      // report the cancellation rather than that (possibly-unrelated,
      // possibly-just-"cancelled") server error.
      throw new CallAbortedError();
    }
  }

  private waitClosed(): Promise<void> {
    if (this.closed) return Promise.resolve();
    return new Promise((resolve) => this.closeWaiters.push(resolve));
  }

  private handleMessage(data: string): void {
    let env: WireEnvelope;
    try {
      env = JSON.parse(data);
    } catch {
      return;
    }
    if (!env.id) return;

    const req = this.inflight.get(env.id);
    if (!req) return;

    if (env.event) {
      req.onEvent?.(env.event, env.params);
      return;
    }

    this.inflight.delete(env.id);
    req.resolveTerm(env);
  }

  /**
   * Marks the connection closed and delivers a synthetic error terminal
   * response to every request still waiting on one, so no caller of
   * call/callStream can be left waiting forever past the connection dying.
   */
  private failAll(err: Error): void {
    if (this.closed) return;
    this.closed = true;
    this.closeError = err;

    // Snapshot the still-in-flight requests before clearing: this.inflight
    // and a same-reference alias would both see the clear, silently
    // dropping every pending request instead of failing it.
    const reqs = [...this.inflight.values()];
    this.inflight.clear();
    for (const req of reqs) {
      req.resolveTerm({ error: { message: err.message } });
    }

    const waiters = this.closeWaiters;
    this.closeWaiters = [];
    for (const resolve of waiters) resolve();
  }

  private send(env: WireEnvelope): void {
    this.socket.send(JSON.stringify(env));
  }
}

function waitForAbort(signal: AbortSignal): Promise<void> {
  if (signal.aborted) return Promise.resolve();
  return new Promise((resolve) => signal.addEventListener("abort", () => resolve(), { once: true }));
}

function decodeTerminal<TResult>(env: WireEnvelope): TResult {
  if (env.error) {
    throw new RpcError(env.error.message);
  }
  return env.result as TResult;
}

/**
 * Builds the same-origin `ws(s)://.../ws?token=...` URL the daemon's /ws
 * endpoint expects -- see internal/wsapi.Handler's doc comment for why the
 * token travels as a query param rather than an Authorization header
 * (browser WebSocket can't set request headers on the upgrade).
 */
export function daemonWsUrl(token: string, loc: Pick<Location, "protocol" | "host"> = window.location): string {
  const scheme = loc.protocol === "https:" ? "wss:" : "ws:";
  return `${scheme}//${loc.host}/ws?token=${encodeURIComponent(token)}`;
}
