import { describe, expect, it, vi } from "vitest";
import { CallAbortedError, RpcError, WsClient, type WireEnvelope } from "@/lib/ws-client";

/**
 * A minimal, in-process fake of the browser WebSocket surface WsClient
 * needs (see SocketLike in ws-client.ts). Tests drive it directly --
 * inspecting what the client sent via `sent`, and feeding it fake
 * server-pushed messages via `emit`/`emitClose` -- rather than talking to
 * a live daemon, which is what the Go-side wsclient/wsapi test suites
 * already cover server-side. This file is about the TS client's own
 * request-tracking/streaming/cancellation logic.
 */
class FakeSocket {
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

  /** Returns (and removes) the last sent envelope for the given method. */
  lastSent(method: string): WireEnvelope | undefined {
    return [...this.sent].reverse().find((e) => e.method === method);
  }
}

/** Flushes pending microtasks (promise chains) and one macrotask tick. */
function flush(): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

describe("WsClient", () => {
  it("round-trips a request/response call", async () => {
    const socket = new FakeSocket();
    const client = new WsClient(socket);

    const promise = client.call<{ echoed: string }>("workspace.get", { id: 1 });

    expect(socket.sent).toHaveLength(1);
    expect(socket.sent[0]).toMatchObject({ id: "1", method: "workspace.get", params: { id: 1 } });

    socket.emit({ id: "1", result: { echoed: "hi" } });

    await expect(promise).resolves.toEqual({ echoed: "hi" });
  });

  it("rejects the call when the server's terminal response is an error", async () => {
    const socket = new FakeSocket();
    const client = new WsClient(socket);

    const promise = client.call("task.create", { title: "x" });
    const id = socket.sent[0]!.id!;
    socket.emit({ id, error: { message: "workspace not found" } });

    await expect(promise).rejects.toBeInstanceOf(RpcError);
    await expect(promise).rejects.toThrow("workspace not found");
  });

  it("invokes the event callback for each streamed event before the terminal result, in order", async () => {
    const socket = new FakeSocket();
    const client = new WsClient(socket);

    const events: Array<{ event: string; params: unknown }> = [];
    let resolved = false;
    const promise = client
      .callStream<{ runId: string }>("task.prompt", { taskId: 1 }, (event, params) => {
        events.push({ event, params });
      })
      .then((result) => {
        resolved = true;
        return result;
      });

    const id = socket.sent[0]!.id!;

    socket.emit({ id, event: "chunk", params: { text: "hello " } });
    socket.emit({ id, event: "chunk", params: { text: "world" } });

    // The callback must fire as each event arrives, not buffered until the
    // terminal response -- assert it already ran twice while the call is
    // still unresolved.
    expect(events).toEqual([
      { event: "chunk", params: { text: "hello " } },
      { event: "chunk", params: { text: "world" } },
    ]);
    expect(resolved).toBe(false);

    socket.emit({ id, result: { runId: "run-1" } });

    await expect(promise).resolves.toEqual({ runId: "run-1" });
    expect(resolved).toBe(true);
  });

  it("cancels only the aborted request, leaving a concurrent request unaffected", async () => {
    const socket = new FakeSocket();
    const client = new WsClient(socket);

    const controller = new AbortController();
    const cancelled = client.call("task.prompt", { taskId: 1 }, { signal: controller.signal });
    const other = client.call<{ ok: boolean }>("workspace.list");

    const cancelledId = socket.sent.find((e) => e.method === "task.prompt")!.id!;
    const otherId = socket.sent.find((e) => e.method === "workspace.list")!.id!;
    expect(cancelledId).not.toBe(otherId);

    controller.abort();
    // Let the abort propagate through the client's internal promise chain
    // before asserting the cancel message was sent.
    await flush();

    const cancelMsg = socket.lastSent("task.cancel");
    expect(cancelMsg).toBeDefined();
    expect(cancelMsg?.params).toEqual({ id: cancelledId });
    // No cancel should reference the other, still in-flight request.
    expect(cancelMsg?.params).not.toEqual({ id: otherId });

    // The other request completes normally -- the cancel didn't touch it.
    socket.emit({ id: otherId, result: { ok: true } });
    await expect(other).resolves.toEqual({ ok: true });

    // The server's terminal response for the cancelled request, once it
    // arrives, is reported as a cancellation rather than swallowed.
    socket.emit({ id: cancelledId, error: { message: "cancelled" } });
    await expect(cancelled).rejects.toBeInstanceOf(CallAbortedError);
  });

  it("resolves with the real result if the server's completion races and wins the cancel", async () => {
    const socket = new FakeSocket();
    const client = new WsClient(socket);

    const controller = new AbortController();
    const promise = client.call<{ runId: string }>("task.prompt", {}, { signal: controller.signal });
    const id = socket.sent[0]!.id!;

    controller.abort();
    await flush();

    // The real completion arrives after the cancel was sent, but is still
    // a success -- that should win over reporting a cancellation.
    socket.emit({ id, result: { runId: "run-2" } });

    await expect(promise).resolves.toEqual({ runId: "run-2" });
  });

  it("fails every in-flight request when the connection closes", async () => {
    const socket = new FakeSocket();
    const client = new WsClient(socket);

    const a = client.call("workspace.list");
    const b = client.call("task.list", { workspaceId: 1 });

    socket.emitClose();

    await expect(a).rejects.toThrow();
    await expect(b).rejects.toThrow();
  });

  it("rejects new calls made after the connection has already closed", async () => {
    const socket = new FakeSocket();
    const client = new WsClient(socket);

    socket.emitClose();

    await expect(client.call("workspace.list")).rejects.toThrow(/closed/);
  });

  describe("onClose", () => {
    it("fires every registered callback once when the connection closes", () => {
      const socket = new FakeSocket();
      const client = new WsClient(socket);

      const a = vi.fn();
      const b = vi.fn();
      client.onClose(a);
      client.onClose(b);

      socket.emitClose();
      expect(a).toHaveBeenCalledTimes(1);
      expect(b).toHaveBeenCalledTimes(1);

      // Only fires once, even if the socket somehow reports closing again.
      socket.emitClose();
      expect(a).toHaveBeenCalledTimes(1);
      expect(b).toHaveBeenCalledTimes(1);
    });

    it("fires immediately, synchronously, if the connection is already closed", () => {
      const socket = new FakeSocket();
      const client = new WsClient(socket);
      socket.emitClose();

      const callback = vi.fn();
      client.onClose(callback);

      expect(callback).toHaveBeenCalledTimes(1);
    });

    it("still fires every other callback even if one of them throws", () => {
      const socket = new FakeSocket();
      const client = new WsClient(socket);

      const before = vi.fn();
      const throwing = vi.fn(() => {
        throw new Error("boom");
      });
      const after = vi.fn();
      client.onClose(before);
      client.onClose(throwing);
      client.onClose(after);

      const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
      expect(() => socket.emitClose()).not.toThrow();
      consoleError.mockRestore();

      expect(before).toHaveBeenCalledTimes(1);
      expect(throwing).toHaveBeenCalledTimes(1);
      // Registered *after* the throwing callback -- must still fire.
      expect(after).toHaveBeenCalledTimes(1);
    });
  });
});
