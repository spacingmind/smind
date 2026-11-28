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
  private readonly subs = new Map<string, () => void>();
  private offState: (() => void) | null = null;
  private closed = false;

  constructor(private readonly conn: RelayConnectionLike) {
    if (conn.state.kind !== 'connected') {
      queueMicrotask(() => this.shutdown());
      return;
    }
    this.offState = conn.onState((s) => {
      if (s.kind !== 'connected') this.shutdown();
    });
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
      const topics = topicsOf(env.params);
      this.subs.get(id)?.();
      const off = this.conn.subscribe(topics, (e) =>
        this.emit({ event: { topic: e.topic, seq: e.seq ?? 0, payload: e.payload } }),
      );
      this.subs.set(id, off);
      this.emit({ id, result: null });
      return;
    }
    if (env.method === 'events.unsubscribe') {
      this.releaseTopics(topicsOf(env.params));
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
      (err: unknown) => {
        if (this.inflight.get(id) !== call) return;
        this.inflight.delete(id);
        const e = err as { message?: unknown; code?: unknown; data?: unknown };
        const error: { message: string; code?: string; data?: unknown } = {
          message: typeof e?.message === 'string' ? e.message : 'call failed',
        };
        if (typeof e?.code === 'string') error.code = e.code;
        if (e?.data !== undefined) error.data = e.data;
        this.emit({ id, error });
      },
    );
  }

  // events.unsubscribe carries topics, but a conn.subscribe handle covers its
  // whole topic set, so a subscription is released when any of its request's
  // topics is named; the browser app subscribes once and unsubscribes whole.
  private releaseTopics(_topics: string[]): void {
    for (const off of this.subs.values()) off();
    this.subs.clear();
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
