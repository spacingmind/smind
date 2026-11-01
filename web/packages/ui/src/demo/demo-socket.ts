import type { SocketLike, WireEnvelope } from "@/lib/ws-client";

import { DemoError, READ_ONLY_MESSAGE, ruleFor } from "./rules";
import type { DemoSession } from "./session";

type MessageListener = (ev: { data: string }) => void;

/**
 * The scripted daemon, as the socket `WsClient` talks to (ADR-0051 section 1). It receives the request envelopes the real
 * client sends and answers with the same JSON a daemon would -- responses, streaming events, ADR-0005 notifications --
 * from a `DemoSession`. Nothing here touches the network.
 */
export class DemoSocket implements SocketLike {
  private readonly message: MessageListener[] = [];
  private readonly close_: Array<() => void> = [];
  private readonly error_: Array<() => void> = [];
  private readonly inflight = new Map<string, AbortController>();
  private subscribed: Set<string> | null = null;
  private seq = 0;
  private closed = false;
  private readonly unsubNotify: () => void;

  constructor(private readonly session: DemoSession) {
    this.unsubNotify = session.onNotify((topic, payload) => this.pushNotification(topic, payload));
  }

  addEventListener(type: "message", listener: (ev: { data: string }) => void): void;
  addEventListener(type: "close" | "error", listener: () => void): void;
  addEventListener(type: string, listener: (...args: never[]) => void): void {
    if (type === "message") this.message.push(listener as MessageListener);
    else if (type === "close") this.close_.push(listener as () => void);
    else this.error_.push(listener as () => void);
  }

  close(): void {
    if (this.closed) return;
    this.closed = true;
    this.unsubNotify();
    for (const c of this.inflight.values()) c.abort();
    this.inflight.clear();
    for (const l of this.close_) l();
  }

  send(data: string): void {
    if (this.closed) return;
    let env: WireEnvelope;
    try {
      env = JSON.parse(data) as WireEnvelope;
    } catch {
      return;
    }
    if (env.method === "task.cancel" && !env.id) {
      const target = (env.params as { id?: string } | undefined)?.id;
      if (target) this.inflight.get(target)?.abort();
      return;
    }
    if (!env.id || !env.method) return;
    void this.handle(env.id, env.method, env.params);
  }

  private deliver(env: unknown): void {
    if (this.closed) return;
    const data = JSON.stringify(env);
    for (const l of this.message) l({ data });
  }

  private pushNotification(topic: string, payload: unknown): void {
    if (!this.subscribed || !this.subscribed.has(topic)) return;
    this.deliver({ event: { topic, seq: ++this.seq, payload } });
  }

  private async handle(id: string, method: string, params: unknown): Promise<void> {
    // Answer asynchronously, like a socket would: a caller never sees its response before send() returns.
    await Promise.resolve();
    const rule = ruleFor(method);
    if (!rule) {
      console.warn(`[demo] no rule for ${method}`);
      this.deliver({ id, error: { message: "Not available in the demo.", code: "demo_unsupported" } });
      return;
    }
    const controller = new AbortController();
    this.inflight.set(id, controller);
    try {
      if (method === "events.subscribe") {
        const topics = (params as { topics?: string[] } | undefined)?.topics ?? [];
        this.subscribed ??= new Set();
        for (const t of topics) this.subscribed.add(t);
      }
      const result = await rule.run({
        params,
        session: this.session,
        signal: controller.signal,
        stream: (event, p) => {
          if (!controller.signal.aborted) this.deliver({ id, event, params: p });
        },
      });
      if (controller.signal.aborted) this.deliver({ id, error: { message: "cancelled", code: "cancelled" } });
      else this.deliver({ id, result: result ?? {} });
    } catch (err) {
      if (err instanceof DemoError) this.deliver({ id, error: { message: err.message, code: err.code } });
      else {
        console.error(`[demo] ${method} failed`, err);
        this.deliver({ id, error: { message: READ_ONLY_MESSAGE, code: "demo_error" } });
      }
    } finally {
      this.inflight.delete(id);
    }
  }
}
