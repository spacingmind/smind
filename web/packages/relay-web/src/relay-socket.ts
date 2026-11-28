// relay-socket.ts adapts the shared RelayConnection to the SocketLike the
// UI's WsClient drives, so WsClient runs unchanged over the relay: it
// translates wsapi envelopes into conn.call / conn.subscribe and back.

export interface SocketLike {
  send(data: string): void;
  close(): void;
  addEventListener(type: 'message', l: (ev: { data: string }) => void): void;
  addEventListener(type: 'close' | 'error', l: () => void): void;
}

export interface RelayConnectionLike {
  call(
    method: string,
    params?: unknown,
    opts?: { onEvent?: (event: string, params: unknown) => void },
  ): Promise<unknown> & { cancel(): void };
  subscribe(
    topics: string[],
    cb: (e: { topic: string; seq?: number; payload: unknown }) => void,
  ): () => void;
  onState(cb: (s: { kind: string }) => void): () => void;
  /** The daemon's synthetic event.dropped (its per-connection queue overflowed); count is how many events were lost. */
  onDropped(cb: (count: number) => void): () => void;
  readonly state: { kind: string };
}

interface Envelope {
  id?: string;
  method?: string;
  params?: unknown;
}

interface Inflight {
  cancel(): void;
}

export class RelaySocket implements SocketLike {
  private readonly messageListeners: Array<(ev: { data: string }) => void> = [];
  private readonly closeListeners: Array<() => void> = [];
  private readonly errorListeners: Array<() => void> = [];
  private readonly inflight = new Map<string, Inflight>();
  /** One local dispatch registration per topic this socket subscribed to. */
  private readonly subs = new Map<string, () => void>();
  private offState: (() => void) | null = null;
  private offDropped: (() => void) | null = null;
  private closed = false;

  constructor(private readonly conn: RelayConnectionLike) {
    if (conn.state.kind !== 'connected') {
      queueMicrotask(() => this.shutdown());
      return;
    }
    this.offState = conn.onState((s) => {
      if (s.kind !== 'connected') this.shutdown();
    });
    // The daemon's synthetic notification, as it travels over a real WebSocket
    // ({event: {topic: 'event.dropped', seq, payload: {count}}}); RelayConnection
    // intercepts it, so it is rebuilt here for the UI's listeners.
    this.offDropped = conn.onDropped((count) =>
      this.emit({ event: { topic: 'event.dropped', seq: 0, payload: { count } } }),
    );
  }

  addEventListener(type: 'message', l: (ev: { data: string }) => void): void;
  addEventListener(type: 'close' | 'error', l: () => void): void;
  addEventListener(type: string, l: unknown): void {
    if (type === 'message') this.messageListeners.push(l as (ev: { data: string }) => void);
    else if (type === 'close') this.closeListeners.push(l as () => void);
    else if (type === 'error') this.errorListeners.push(l as () => void);
  }

  close(): void {
    this.shutdown();
  }

  send(text: string): void {
    if (this.closed) throw new Error('relay socket is closed');
    let env: Envelope;
    try {
      env = JSON.parse(text) as Envelope;
    } catch {
      return;
    }
    if (!env || typeof env !== 'object' || typeof env.method !== 'string') return;

    if (env.method === 'task.cancel' && env.id === undefined) {
      const id = (env.params as { id?: unknown } | null)?.id;
      if (typeof id !== 'string') return;
      const call = this.inflight.get(id);
      if (!call) return;
      this.inflight.delete(id);
      call.cancel();
      this.emit({ id, error: { message: 'cancelled' } });
      return;
    }
    if (typeof env.id !== 'string') return;
    const id = env.id;

    if (env.method === 'events.subscribe') {
      this.subscribe(id, env.params);
      return;
    }
    if (env.method === 'events.unsubscribe') {
      for (const topic of topicsOf(env.params)) {
        this.subs.get(topic)?.();
        this.subs.delete(topic);
      }
      this.emit({ id, result: null });
      return;
    }

    const call = this.conn.call(env.method, env.params ?? null, {
      onEvent: (event, p) => {
        if (this.inflight.get(id) === call) this.emit({ id, event, params: p });
      },
    });
    this.inflight.set(id, call);
    call.then(
      (result) => {
        if (this.inflight.get(id) !== call) return;
        this.inflight.delete(id);
        this.emit({ id, result });
      },
      (err: unknown) => this.failCall(id, call, err),
    );
  }

  // events.subscribe is the daemon's own RPC, answered with its real outcome
  // (so the UI's tolerant retry sees `unknown topic "x"`). Only after it
  // succeeds is local dispatch registered, one conn.subscribe per topic and
  // never twice for the same topic. conn.subscribe sends its own
  // events.subscribe for the topic internally; that is harmless, the daemon
  // dedups topics per connection.
  private subscribe(id: string, params: unknown): void {
    const topics = topicsOf(params);
    const call = this.conn.call('events.subscribe', params ?? null);
    this.inflight.set(id, call);
    call.then(
      (result) => {
        if (this.inflight.get(id) !== call) return;
        this.inflight.delete(id);
        if (this.closed) return;
        for (const topic of topics) {
          if (this.subs.has(topic)) continue;
          this.subs.set(
            topic,
            this.conn.subscribe([topic], (e) =>
              this.emit({ event: { topic: e.topic, seq: e.seq ?? 0, payload: e.payload } }),
            ),
          );
        }
        this.emit({ id, result });
      },
      (err: unknown) => this.failCall(id, call, err),
    );
  }

  private failCall(id: string, call: Inflight, err: unknown): void {
    if (this.inflight.get(id) !== call) return;
    this.inflight.delete(id);
    const e = err as { message?: unknown; code?: unknown; data?: unknown };
    const error: { message: string; code?: string; data?: unknown } = {
      message: typeof e?.message === 'string' ? e.message : 'call failed',
    };
    if (typeof e?.code === 'string') error.code = e.code;
    if (e?.data !== undefined) error.data = e.data;
    this.emit({ id, error });
  }

  private emit(env: unknown): void {
    const data = JSON.stringify(env);
    queueMicrotask(() => {
      if (this.closed) return;
      for (const l of [...this.messageListeners]) l({ data });
    });
  }

  private shutdown(): void {
    if (this.closed) return;
    this.closed = true;
    this.offState?.();
    this.offState = null;
    this.offDropped?.();
    this.offDropped = null;
    for (const off of this.subs.values()) off();
    this.subs.clear();
    this.inflight.clear();
    for (const l of [...this.closeListeners]) l();
  }
}

function topicsOf(params: unknown): string[] {
  const t = (params as { topics?: unknown } | null)?.topics;
  return Array.isArray(t) ? t.filter((x): x is string => typeof x === 'string') : [];
}
