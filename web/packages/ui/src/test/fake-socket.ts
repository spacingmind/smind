import type { WireEnvelope } from "@/lib/ws-client";

/**
 * A minimal, in-process fake of the browser WebSocket surface WsClient
 * needs (see SocketLike in lib/ws-client.ts). Tests drive it directly --
 * inspecting what was sent via `sent`, and feeding it fake server-pushed
 * messages via `emit`/`emitClose` -- rather than talking to a live daemon.
 * Originally written for ws-client.test.ts (which keeps its own copy, kept
 * standalone there to avoid disturbing an already-passing, unrelated
 * file); shared here for reconnect.test.ts and App.test.tsx, both of which
 * need a real WsClient wrapping a real socket-shaped fake to exercise
 * reconnect (a new WsClient instance per attempt), not just the
 * call/callStream surface FakeWsClient (test/fake-ws-client.ts) fakes one
 * level up.
 */
export class FakeSocket {
  sent: WireEnvelope[] = [];
  private listeners: { message: Array<(ev: { data: string }) => void>; close: Array<() => void>; error: Array<() => void> } = {
    message: [],
    close: [],
    error: [],
  };

  send(data: string): void {
    this.sent.push(JSON.parse(data) as WireEnvelope);
  }

  close(): void {
    this.emitClose();
  }

  addEventListener(type: "message", listener: (ev: { data: string }) => void): void;
  addEventListener(type: "close" | "error", listener: () => void): void;
  addEventListener(type: string, listener: (...args: never[]) => void): void {
    (this.listeners[type as keyof typeof this.listeners] as Array<typeof listener>).push(listener);
  }

  /** Simulates the server sending one envelope down the wire. */
  emit(env: WireEnvelope): void {
    const data = JSON.stringify(env);
    for (const l of this.listeners.message) l({ data });
  }

  emitClose(): void {
    for (const l of this.listeners.close) l();
  }

  /** Returns the last sent envelope for the given method. */
  lastSent(method: string): WireEnvelope | undefined {
    return [...this.sent].reverse().find((e) => e.method === method);
  }
}
