import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { useDaemonEvents } from "@/hooks/use-daemon-events";
import { WsClient, type DaemonNotification } from "@/lib/ws-client";
import { FakeSocket } from "@/test/fake-socket";

/** Flushes pending microtasks, wrapped in `act` so React commits any resulting state updates before the caller asserts. */
async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

/** Feeds one ADR-0005 notification down the socket, wrapped in act. */
function notify(socket: FakeSocket, topic: string, payload: unknown): void {
  act(() => {
    socket.emit({ event: { topic, seq: 1, payload } } as never);
  });
}

describe("useDaemonEvents", () => {
  it("issues one events.subscribe with the three topics per connection, regardless of consumer count", async () => {
    const socket = new FakeSocket();
    const client = new WsClient(socket);
    const { result, rerender } = renderHook(() => useDaemonEvents(client));
    await flush();

    const subs = socket.sent.filter((e) => e.method === "events.subscribe");
    expect(subs).toHaveLength(1);
    expect(subs[0]!.params).toEqual({ topics: ["task.status", "run.status", "permission.pending"] });

    // Two consumers on the same surface, still one daemon subscription.
    act(() => {
      result.current!.subscribe("run.status", () => {});
      result.current!.subscribe("run.status", () => {});
    });
    expect(socket.sent.filter((e) => e.method === "events.subscribe")).toHaveLength(1);

    // Re-rendering with the same client never re-subscribes.
    rerender();
    expect(socket.sent.filter((e) => e.method === "events.subscribe")).toHaveLength(1);
  });

  it("delivers topic-filtered notifications to registered listeners, and unregistration stops delivery", async () => {
    const socket = new FakeSocket();
    const client = new WsClient(socket);
    const { result } = renderHook(() => useDaemonEvents(client));
    await flush();

    const runStatus = vi.fn();
    const off = result.current!.subscribe("run.status", runStatus);

    notify(socket, "run.status", { runId: "r1", taskId: 1, status: "done" });
    expect(runStatus).toHaveBeenCalledWith({ runId: "r1", taskId: 1, status: "done" });

    // A different topic never reaches the run.status listener.
    notify(socket, "task.status", { taskId: 1, status: "idle" });
    expect(runStatus).toHaveBeenCalledTimes(1);

    off();
    notify(socket, "run.status", { runId: "r1", taskId: 1, status: "error" });
    expect(runStatus).toHaveBeenCalledTimes(1);
  });

  it("isolates a throwing listener -- the other listeners on the same topic still run", async () => {
    const socket = new FakeSocket();
    const client = new WsClient(socket);
    const { result } = renderHook(() => useDaemonEvents(client));
    await flush();

    const throwing = vi.fn(() => {
      throw new Error("boom");
    });
    const after = vi.fn();
    const consoleError = vi.spyOn(console, "error").mockImplementation(() => {});
    act(() => {
      result.current!.subscribe("task.status", throwing);
      result.current!.subscribe("task.status", after);
    });

    expect(() => notify(socket, "task.status", { taskId: 1, status: "done" })).not.toThrow();
    expect(throwing).toHaveBeenCalledTimes(1);
    expect(after).toHaveBeenCalledTimes(1);
    consoleError.mockRestore();
  });

  it("re-subscribes on reconnect (client change) and drops listeners from the old connection", async () => {
    const socket1 = new FakeSocket();
    const socket2 = new FakeSocket();
    const client1 = new WsClient(socket1);
    const client2 = new WsClient(socket2);
    const { result, rerender } = renderHook(({ client }) => useDaemonEvents(client), {
      initialProps: { client: client1 as WsClient | null },
    });
    await flush();

    const listener = vi.fn();
    act(() => {
      result.current!.subscribe("run.status", listener);
    });

    rerender({ client: client2 });
    await flush();

    // One subscribe per connection.
    expect(socket1.sent.filter((e) => e.method === "events.subscribe")).toHaveLength(1);
    expect(socket2.sent.filter((e) => e.method === "events.subscribe")).toHaveLength(1);

    // The old connection's notification no longer reaches listeners...
    notify(socket1, "run.status", { runId: "r1", taskId: 1, status: "done" });
    expect(listener).not.toHaveBeenCalled();

    // ...but the new connection's does (consumers re-register via their
    // own client-keyed effects in the app; here we re-register manually).
    act(() => {
      result.current!.subscribe("run.status", listener);
    });
    notify(socket2, "run.status", { runId: "r2", taskId: 2, status: "done" });
    expect(listener).toHaveBeenCalledTimes(1);
  });

  it("a failed events.subscribe (daemon without the RPC) never throws into the render", async () => {
    const socket = new FakeSocket();
    const client = new WsClient(socket);
    renderHook(() => useDaemonEvents(client));
    await flush();

    const sub = socket.sent.find((e) => e.method === "events.subscribe");
    expect(sub?.id).toBeDefined();
    await act(async () => {
      socket.emit({ id: sub!.id!, error: { message: "unknown method" } });
    });
  });
});

/** Unused-type guard: DaemonNotification is the shape listeners see end-to-end. */
export type _NotificationShape = DaemonNotification;
