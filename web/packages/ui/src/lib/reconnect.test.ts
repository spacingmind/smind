import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import * as daemonLib from "@/lib/daemon";
import { WsClient } from "@/lib/ws-client";
import { watchForReconnect, type ConnectionStatus } from "@/lib/reconnect";
import { FakeSocket } from "@/test/fake-socket";

// Every scenario here needs to control when a reconnect attempt's backoff
// delay elapses, without a real wall-clock wait -- vitest's own fake timers
// (already a devDependency via vitest itself, no new dependency needed)
// cover that.
beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
});

/** Advances past the largest possible backoff delay (the 10s max) so a pending retry timer always fires, regardless of its jittered value. */
async function advancePastBackoff(): Promise<void> {
  await vi.advanceTimersByTimeAsync(10_000);
}

describe("watchForReconnect", () => {
  it("an unexpected close triggers a reconnect attempt", async () => {
    const socket = new FakeSocket();
    const client = new WsClient(socket);
    const connect = vi.fn(() => new Promise<WsClient>(() => {}));

    const handle = watchForReconnect(client, { connect, onClient: () => {} });
    socket.emitClose();

    expect(connect).not.toHaveBeenCalled();
    await advancePastBackoff();
    expect(connect).toHaveBeenCalledTimes(1);

    handle.close();
  });

  it("an explicit close() never triggers a reconnect attempt", async () => {
    const client = new WsClient(new FakeSocket());
    const connect = vi.fn(() => new Promise<WsClient>(() => {}));

    const handle = watchForReconnect(client, { connect, onClient: () => {} });
    handle.close();

    await advancePastBackoff();
    await advancePastBackoff();
    expect(connect).not.toHaveBeenCalled();
  });

  it("a failed reconnect attempt retries again with backoff", async () => {
    const socket = new FakeSocket();
    const client = new WsClient(socket);
    const connect = vi.fn(() => Promise.reject(new Error("dial failed")));
    const statuses: ConnectionStatus[] = [];

    const handle = watchForReconnect(client, { connect, onClient: () => {}, onStatusChange: (s) => statuses.push(s) });
    socket.emitClose();

    // Advance to exactly the next pending timer each time, not a blanket
    // window -- backoff delay is jittered (uniform-random within a
    // growing cap), so a window wide enough to safely cover the first
    // attempt can easily be wide enough to also cover the next several,
    // cascading through multiple retries in one step instead of one.
    await vi.advanceTimersToNextTimerAsync();
    expect(connect).toHaveBeenCalledTimes(1);

    await vi.advanceTimersToNextTimerAsync();
    expect(connect).toHaveBeenCalledTimes(2);

    expect(statuses).toEqual(["reconnecting"]);
    handle.close();
  });

  it("a successful reconnect resolves to a usable client, callable against its new fake socket", async () => {
    const socket1 = new FakeSocket();
    const client1 = new WsClient(socket1);
    const socket2 = new FakeSocket();
    const connect = vi.fn(() => Promise.resolve(new WsClient(socket2)));

    let latest: WsClient | null = null;
    const handle = watchForReconnect(client1, { connect, onClient: (c) => (latest = c) });
    socket1.emitClose();
    await advancePastBackoff();

    expect(connect).toHaveBeenCalledTimes(1);
    expect(latest).not.toBeNull();

    const promise = latest!.call<{ ok: boolean }>("workspace.list");
    expect(socket2.sent).toHaveLength(1);
    socket2.emit({ id: socket2.sent[0]!.id!, result: { ok: true } });

    await expect(promise).resolves.toEqual({ ok: true });
    handle.close();
  });

  it("re-arms itself on the new client, so a second unexpected close also triggers a reconnect", async () => {
    const socket1 = new FakeSocket();
    const client1 = new WsClient(socket1);
    const socket2 = new FakeSocket();
    const client2 = new WsClient(socket2);
    const socket3 = new FakeSocket();

    const connect = vi.fn().mockResolvedValueOnce(client2).mockResolvedValueOnce(new WsClient(socket3));

    let latest: WsClient | null = null;
    const handle = watchForReconnect(client1, { connect, onClient: (c) => (latest = c) });

    socket1.emitClose();
    await advancePastBackoff();
    expect(latest).toBe(client2);

    socket2.emitClose();
    await advancePastBackoff();
    expect(connect).toHaveBeenCalledTimes(2);
    expect(latest).not.toBe(client2);

    handle.close();
  });

  it("uses the default connect (connectDaemon) fresh on every attempt -- refetching /api/token each time, not reusing the original", async () => {
    const socket = new FakeSocket();
    const client = new WsClient(socket);
    const spy = vi
      .spyOn(daemonLib, "connectDaemon")
      .mockRejectedValueOnce(new Error("still down"))
      .mockResolvedValueOnce(new WsClient(new FakeSocket()));

    const handle = watchForReconnect(client, { onClient: () => {} });
    socket.emitClose();

    await vi.advanceTimersToNextTimerAsync();
    expect(spy).toHaveBeenCalledTimes(1);

    await vi.advanceTimersToNextTimerAsync();
    expect(spy).toHaveBeenCalledTimes(2);

    handle.close();
  });
});
