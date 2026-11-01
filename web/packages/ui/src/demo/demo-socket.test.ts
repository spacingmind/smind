import { beforeEach, describe, expect, it } from "vitest";

import type { RunLogEvent, Task, TerminalCreateResult } from "@/lib/types";
import { CallAbortedError, RpcError, type WsClient } from "@/lib/ws-client";

import type { DemoClock } from "./clock";
import { connectDemo, resetDemoSession } from "./connect";
import { buildTour } from "./scenario";

const NOW = Date.parse("2026-11-02T10:00:00Z");

/** A manual clock: timers fire only when the test advances it, so no test races a real timer (the repo's test rule). */
class ManualClock implements DemoClock {
  private t = NOW;
  private timers: { at: number; fn: () => void; id: number }[] = [];
  private nextId = 1;
  now(): number {
    return this.t;
  }
  after(ms: number, fn: () => void): () => void {
    const id = this.nextId++;
    this.timers.push({ at: this.t + ms, fn, id });
    return () => {
      this.timers = this.timers.filter((x) => x.id !== id);
    };
  }
  /** Runs every timer due within `ms`, in order (a timer may schedule more), flushing the socket's promise hops between them. */
  async advance(ms: number): Promise<void> {
    const end = this.t + ms;
    for (;;) {
      await flush();
      const due = this.timers.filter((x) => x.at <= end).sort((a, b) => a.at - b.at || a.id - b.id)[0];
      if (!due) break;
      this.timers = this.timers.filter((x) => x.id !== due.id);
      this.t = Math.max(this.t, due.at);
      due.fn();
    }
    this.t = end;
    await flush();
  }
}

/** Lets the socket's asynchronous answers (one microtask hop per layer) land. */
const flush = () => new Promise<void>((r) => setTimeout(r, 0));

interface Harness {
  client: WsClient;
  clock: ManualClock;
}

async function start(reducedMotion = false): Promise<Harness> {
  resetDemoSession();
  const clock = new ManualClock();
  const client = await connectDemo({ scenario: buildTour(NOW), clock, reducedMotion });
  return { client, clock };
}

/** Attaches to a run and collects what it streams until the terminal result. */
function attach(client: WsClient, runId: string, signal?: AbortSignal) {
  const events: { event: string; params: Record<string, unknown> }[] = [];
  const done = client.callStream<{ runId: string; stopReason: string }>("run.attach", { runId }, (event, params) => events.push({ event, params: params as Record<string, unknown> }), { signal });
  return { events, done };
}

const ALLOW = { runId: "demo-live", requestId: "demo-req-1", optionId: "allow" };
const DENY = { runId: "demo-live", requestId: "demo-req-1", optionId: "deny" };
const rejects = async (p: Promise<unknown>): Promise<RpcError> => {
  try {
    await p;
  } catch (e) {
    expect(e).toBeInstanceOf(RpcError);
    return e as RpcError;
  }
  throw new Error("expected the call to be rejected");
};

describe("demo socket", () => {
  beforeEach(() => resetDemoSession());

  it("D-01 connectDemo resolves a real WsClient and daemon.info answers the demo daemon", async () => {
    const { client } = await start();
    const info = await client.call<{ version: string; files: { previewKinds: string[] } }>("daemon.info");
    expect(info.version).toBe("demo");
    expect(info.files.previewKinds).toEqual([]);
    const workspaces = await client.call<{ Title: string }[]>("workspace.list");
    expect(workspaces.map((w) => w.Title)).toEqual(["acme-api", "web-dashboard", "docs-site"]);
  });

  it("D-02 an unlisted method answers RpcError demo_unsupported; a write answers demo_read_only with the read-only message", async () => {
    const { client } = await start();
    const unsupported = await rejects(client.call("nope.method"));
    expect(unsupported.code).toBe("demo_unsupported");
    const refused = await rejects(client.call("task.archive", { id: 1 }));
    expect(refused.code).toBe("demo_read_only");
    expect(refused.message).toBe("Read-only in the demo.");
  });

  it("D-04 a script plays its steps in order on an injected clock: events reach onEvent, and the run pauses on its permission request", async () => {
    const { client, clock } = await start();
    const { events, done } = attach(client, "demo-live");
    let ended = false;
    void done.then(() => (ended = true));
    await clock.advance(300);
    expect(events).toHaveLength(0); // nothing is due yet: the first event is scripted at 500 ms
    await clock.advance(30_000);
    const types = events.map((e) => e.event);
    expect(types.indexOf("thinking")).toBeLessThan(types.indexOf("tool_call"));
    expect(types.filter((t) => t === "tool_call").length).toBeGreaterThanOrEqual(6); // three tools, each running then success
    expect(types).toContain("usage");
    expect(types.at(-1)).toBe("permission_request");
    expect(ended).toBe(false);
    const tools = events.filter((e) => e.event === "tool_call").map((e) => e.params.toolName);
    expect(new Set(tools)).toEqual(new Set(["Read", "Bash"]));
  });

  it("D-05 an ask step pauses until run.respondPermission: allow plays the allow branch, deny the deny branch, an unknown request is an error", async () => {
    // allow
    let h = await start();
    let run = attach(h.client, "demo-live");
    await h.clock.advance(30_000);
    expect((await rejects(h.client.call("run.respondPermission", { ...ALLOW, requestId: "nope" }))).code).toBe("not_found");
    expect((await rejects(h.client.call("run.respondPermission", { ...ALLOW, optionId: "bogus" }))).code).toBe("not_found");
    await h.clock.advance(60_000);
    expect(run.events.map((e) => e.event)).not.toContain("done"); // an unanswered ask stays paused, however long it takes
    await h.client.call("run.respondPermission", ALLOW);
    await h.clock.advance(20_000);
    await expect(run.done).resolves.toMatchObject({ stopReason: "end_turn" });
    const allowTools = run.events.filter((e) => e.event === "tool_call").map((e) => e.params.toolName);
    expect(allowTools).toContain("Write");
    expect(run.events.some((e) => e.event === "permission_resolved" && e.params.optionId === "allow")).toBe(true);
    // the diff gains the file the request was about
    const diff = await h.client.call<{ diff: string }>("task.diff", { taskId: 1 });
    expect(diff.diff).toContain("internal/ratelimit/limiter.go");
    // answering the request resolves its inbox row
    expect(await h.client.call("inbox.count")).toMatchObject({ total: 3 });

    // deny
    h = await start();
    run = attach(h.client, "demo-live");
    await h.clock.advance(30_000);
    await h.client.call("run.respondPermission", DENY);
    await h.clock.advance(20_000);
    await expect(run.done).resolves.toMatchObject({ stopReason: "end_turn" });
    const denyTools = run.events.filter((e) => e.event === "tool_call").map((e) => e.params.toolName);
    expect(denyTools).not.toContain("Write");
    const text = run.events.filter((e) => e.event === "chunk").map((e) => String(e.params.text)).join("");
    expect(text).toContain("I won't write that file");
    const after = await h.client.call<{ diff: string }>("task.diff", { taskId: 1 });
    expect(after.diff).not.toContain("internal/ratelimit/limiter.go");
    expect(await h.client.call("inbox.count")).toMatchObject({ total: 3 }); // Deny resolves the row too
  });

  it("D-06 task.cancel and run.stop abort the matching script; nothing is emitted afterwards", async () => {
    const { client, clock } = await start();
    const ac = new AbortController();
    const first = attach(client, "demo-live", ac.signal);
    await clock.advance(3_000);
    ac.abort();
    await expect(first.done).rejects.toBeInstanceOf(CallAbortedError);
    const seen = first.events.length;
    await clock.advance(1_500); // the script itself keeps playing: an attach cancel only detaches
    expect(first.events.length).toBe(seen);

    await client.call("run.stop", { runId: "demo-live" });
    const logsAtStop = await client.call<{ status: string; events: RunLogEvent[] }>("run.logs", { runId: "demo-live" });
    expect(logsAtStop.status).toBe("stopped");
    await clock.advance(60_000);
    const logsLater = await client.call<{ events: RunLogEvent[] }>("run.logs", { runId: "demo-live" });
    expect(logsLater.events.length).toBe(logsAtStop.events.length);
  });

  it("D-07 prefers-reduced-motion collapses step delays", async () => {
    const { client, clock } = await start(true);
    const run = attach(client, "demo-live");
    await clock.advance(200); // 20 steps at a few ms each, not ~10 s of scripted time
    expect(run.events.at(-1)?.event).toBe("permission_request");
  });

  it("D-08 notifications carry a per-connection monotonic seq and respect events.subscribe topics", async () => {
    const { client, clock } = await start();
    const got: { topic: string; seq: number }[] = [];
    client.onNotification((n) => got.push({ topic: n.topic, seq: n.seq }));
    await client.call("events.subscribe", { topics: ["run.status", "inbox.changed"] });
    attach(client, "demo-live");
    await clock.advance(30_000);
    await client.call("run.respondPermission", ALLOW);
    await clock.advance(20_000);
    expect(got.length).toBeGreaterThan(0);
    expect(got.map((n) => n.seq)).toEqual(got.map((_, i) => i + 1));
    expect(new Set(got.map((n) => n.topic))).toEqual(new Set(["run.status", "inbox.changed"])); // permission.pending and task.git were not subscribed
  });

  it("D-09 sending a prompt streams the canned reply; twice in a row works; it is listed with the chat's runs", async () => {
    const { client, clock } = await start();
    const send = async (prompt: string) => {
      const { runId } = await client.call<{ runId: string }>("run.start", { taskId: 2, chatId: 2, provider: "claude-code", prompt });
      const run = attach(client, runId);
      await clock.advance(10_000);
      await expect(run.done).resolves.toMatchObject({ stopReason: "end_turn" });
      return { runId, text: run.events.filter((e) => e.event === "chunk").map((e) => String(e.params.text)).join("") };
    };
    const a = await send("hello there");
    const b = await send("and again");
    expect(a.runId).not.toBe(b.runId);
    expect(a.text).toContain("scripted demo");
    expect(b.text).toContain("scripted demo");
    const runs = await client.call<{ ID: string; Prompt: string }[]>("run.list", { chatId: 2 });
    expect(runs.map((r) => r.Prompt)).toEqual(expect.arrayContaining(["hello there", "and again"]));
  });

  it("D-10 terminal replay: attach emits base64 data in order, resize is a no-op, write echoes locally, Enter prints the dim demo line", async () => {
    const { client, clock } = await start();
    const { terminalId } = await client.call<TerminalCreateResult>("terminal.create", { taskId: 1 });
    expect((await client.call<unknown[]>("terminal.list", { taskId: 1 })).length).toBe(1);
    const chunks: string[] = [];
    void client.callStream("terminal.attach", { terminalId }, (event, params) => {
      if (event === "data") chunks.push(atob((params as { data: string }).data));
    });
    await clock.advance(10_000);
    const replay = chunks.join("");
    expect(replay.indexOf("git status")).toBeLessThan(replay.indexOf("go test"));
    expect(replay.endsWith("$ ")).toBe(true);
    await client.call("terminal.resize", { terminalId, cols: 80, rows: 24 });
    await client.call("terminal.write", { terminalId, data: "ls" });
    await client.call("terminal.write", { terminalId, data: "\r" });
    await clock.advance(10);
    const after = chunks.join("").slice(replay.length);
    expect(after.startsWith("ls")).toBe(true);
    expect(after).toContain("nothing runs here");
  });

  it("D-11 inbox seen/snooze/resolve mutate the in-memory list and push the inbox notification", async () => {
    const { client } = await start();
    const topics: string[] = [];
    client.onNotification((n) => topics.push(n.topic));
    await client.call("events.subscribe", { topics: ["inbox.changed"] });
    expect(await client.call("inbox.count")).toMatchObject({ total: 4, unseen: 3 });
    await client.call("inbox.seen", { keys: ["run_failed:demo-run-go"] });
    expect(await client.call("inbox.count")).toMatchObject({ total: 4, unseen: 2 });
    await client.call("inbox.resolve", { keys: ["verdict:1"] });
    await client.call("inbox.snooze", { keys: ["stall:5:1"], until: "2099-01-01T00:00:00Z" });
    const { items } = await client.call<{ items: { key: string }[] }>("inbox.list", {});
    expect(items.map((i) => i.key).sort()).toEqual(["perm:demo-req-1", "run_failed:demo-run-go"]);
    expect(topics.filter((t) => t === "inbox.changed")).toHaveLength(3);
  });

  it("D-12 a second connectDemo (a reconnect) shares the page-load session state", async () => {
    const { client } = await start();
    await client.call("inbox.resolve", { keys: ["verdict:1"] });
    const again = await connectDemo(); // no options: the existing session is reused
    expect(await again.call("inbox.count")).toMatchObject({ total: 3 });
  });

  it("D-13 refused rules never mutate state", async () => {
    const { client } = await start();
    const before = await client.call<Task[]>("task.list", { workspaceId: 1 });
    for (const method of ["task.archive", "task.commit", "workspace.delete", "space.delete", "profile.create", "provider.setSecret"]) {
      expect((await rejects(client.call(method, { id: 1, taskId: 1 }))).code).toBe("demo_read_only");
    }
    expect(await client.call<Task[]>("task.list", { workspaceId: 1 })).toEqual(before);
    expect(await client.call<unknown[]>("profile.list")).toHaveLength(2);
  });
});
