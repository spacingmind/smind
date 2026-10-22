import { act } from "react";
import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { mergeAttachments, useRunTimeline } from "@/hooks/use-run-timeline";
import { FakeWsClient } from "@/test/fake-ws-client";
import type { DaemonEvents } from "@/hooks/use-daemon-events";
import type { AttachmentMeta, RunLogsResult, RunSummary, RunUsage } from "@/lib/types";

function usage(overrides: Partial<RunUsage> = {}): RunUsage {
  return { inputTokens: 100, outputTokens: 20, source: "claude_result", ...overrides };
}

function summary(overrides: Partial<RunSummary> = {}): RunSummary {
  return {
    ID: "run-1",
    TaskID: 1,
    ChatID: 10,
    Provider: "claude-native",
    Prompt: "do the thing",
    Status: "done",
    StartedAt: "2024-01-01T00:00:00Z",
    FinishedAt: "2024-01-01T00:00:05Z",
    StopReason: "end_turn",
    Err: "",
    PermissionMode: "",
    AutoAccept: false,
    ThinkingLevel: "",
    ...overrides,
  };
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

describe("useRunTimeline disposition (ADR-0027 D13)", () => {
  it("carries the daemon's needs-continue disposition from run.list and a run.logs backfill", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useRunTimeline(client, 1, 10));

    client.nth("run.list", 0).resolve([summary({ StopReason: "max_turn_requests", Disposition: "needs-continue" })]);
    await flush();
    expect(result.current.runs?.[0]?.disposition).toBe("needs-continue");

    await act(async () => {
      client.nth("run.logs", 0).resolve({ runId: "run-1", status: "done", stopReason: "max_turn_requests", disposition: "needs-continue", events: [] } satisfies RunLogsResult);
    });
    await waitFor(() => expect(result.current.runs?.[0]?.disposition).toBe("needs-continue"));
  });

  it("a normal finish has no disposition", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useRunTimeline(client, 1, 10));
    client.nth("run.list", 0).resolve([summary()]);
    await flush();
    expect(result.current.runs?.[0]?.disposition).toBeUndefined();
  });
});

describe("useRunTimeline usage", () => {
  it("run.list usage seeds the run entry", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useRunTimeline(client, 1, 10));

    client.nth("run.list", 0).resolve([summary({ usage: usage({ inputTokens: 5 }) })]);
    await flush();

    expect(result.current.runs?.[0]?.usage).toEqual(usage({ inputTokens: 5 }));
  });

  it("a live usage event replaces the run's usage instead of adding to it", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useRunTimeline(client, 1, 10));

    client.nth("run.list", 0).resolve([summary({ Status: "running", FinishedAt: null, usage: usage({ inputTokens: 10, outputTokens: 2 }) })]);
    await flush();
    expect(result.current.runs?.[0]?.usage).toEqual(usage({ inputTokens: 10, outputTokens: 2 }));

    await act(async () => {
      client.emit("run.attach", 0, "usage", { usage: usage({ inputTokens: 30, outputTokens: 4 }) });
    });
    expect(result.current.runs?.[0]?.usage).toEqual(usage({ inputTokens: 30, outputTokens: 4 }));

    await act(async () => {
      client.emit("run.attach", 0, "usage", { usage: usage({ inputTokens: 40, outputTokens: 5 }) });
    });
    expect(result.current.runs?.[0]?.usage).toEqual(usage({ inputTokens: 40, outputTokens: 5 }));

    // A usage event still renders no transcript row.
    expect(result.current.runs?.[0]?.items).toHaveLength(0);
  });

  it("the latest usage event of a run.logs backfill wins", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useRunTimeline(client, 1, 10));

    client.nth("run.list", 0).resolve([summary({ usage: usage({ inputTokens: 1 }) })]);
    await flush();

    await act(async () => {
      client.nth("run.logs", 0).resolve({
        runId: "run-1",
        status: "done",
        stopReason: "end_turn",
        events: [
          { type: "usage", usage: usage({ inputTokens: 7, outputTokens: 1 }) },
          { type: "usage", usage: usage({ inputTokens: 9, outputTokens: 2 }) },
        ],
      } satisfies RunLogsResult);
    });

    await waitFor(() => expect(result.current.runs?.[0]?.usage).toEqual(usage({ inputTokens: 9, outputTokens: 2 })));
  });

  it("a run without a usage row has undefined usage", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useRunTimeline(client, 1, 10));

    client.nth("run.list", 0).resolve([summary()]);
    await flush();

    await act(async () => {
      client.nth("run.logs", 0).resolve({ runId: "run-1", status: "done", events: [] } satisfies RunLogsResult);
    });

    await waitFor(() => expect(result.current.runs?.[0]?.items).toHaveLength(0));
    expect(result.current.runs?.[0]?.usage).toBeUndefined();
    expect("usage" in (result.current.runs?.[0] ?? {})).toBe(false);
  });
});

describe("useRunTimeline provenance", () => {
  it("run.list BaseSHA/HeadSHA/Dirty seed the run entry", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useRunTimeline(client, 1, 10));

    client.nth("run.list", 0).resolve([
      summary({ BaseSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", HeadSHA: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Dirty: true }),
    ]);
    await flush();

    expect(result.current.runs?.[0]?.baseSha).toBe("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa");
    expect(result.current.runs?.[0]?.headSha).toBe("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb");
    expect(result.current.runs?.[0]?.dirty).toBe(true);
  });

  it("null/absent provenance fields leave the keys unset", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useRunTimeline(client, 1, 10));

    client.nth("run.list", 0).resolve([summary({ BaseSHA: null, HeadSHA: null, Dirty: null })]);
    await flush();
    await act(async () => {
      client.nth("run.logs", 0).resolve({ runId: "run-1", status: "done", events: [] } satisfies RunLogsResult);
    });

    await waitFor(() => expect(result.current.runs?.[0]?.items).toHaveLength(0));
    const run = result.current.runs?.[0] ?? {};
    expect("baseSha" in run).toBe(false);
    expect("headSha" in run).toBe(false);
    expect("dirty" in run).toBe(false);
  });

  it("the run.logs recovery path (stream failed) fills in head sha and dirty", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useRunTimeline(client, 1, 10));
    client.nth("run.list", 0).resolve([summary({ BaseSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Status: "running", FinishedAt: null })]);
    await flush();

    await act(async () => {
      client.nth("run.attach", 0).reject(new Error("socket dropped"));
    });
    await flush();
    await act(async () => {
      client
        .nth("run.logs", 0)
        .resolve({ runId: "run-1", status: "done", headSha: "dddddddddddddddddddddddddddddddddddddddd", dirty: true, events: [] } satisfies RunLogsResult);
    });

    await waitFor(() => expect(result.current.runs?.[0]?.headSha).toBe("dddddddddddddddddddddddddddddddddddddddd"));
    expect(result.current.runs?.[0]?.baseSha).toBe("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa");
    expect(result.current.runs?.[0]?.dirty).toBe(true);
  });

  it("a run.status event patches only the provenance keys it carries", async () => {
    const client = new FakeWsClient();
    const ev = fakeEvents();
    const { result } = renderHook(() => useRunTimeline(client, 1, 10, ev.events));
    client.nth("run.list", 0).resolve([summary({ BaseSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Status: "running", FinishedAt: null })]);
    await flush();
    await act(async () => {
      client.emit("run.attach", 0, "chunk", { text: "hi" });
    });

    await ev.fire("run.status", { runId: "run-1", taskId: 1, chatId: 10, status: "done", headSha: "cccccccccccccccccccccccccccccccccccccccc", dirty: false });

    expect(result.current.runs?.[0]?.baseSha).toBe("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa");
    expect(result.current.runs?.[0]?.headSha).toBe("cccccccccccccccccccccccccccccccccccccccc");
    expect(result.current.runs?.[0]?.dirty).toBe(false);
  });
});

function fakeEvents() {
  const listeners = new Map<string, Set<(payload: unknown) => void>>();
  const events: DaemonEvents = {
    subscribe(topic, listener) {
      const set = listeners.get(topic) ?? new Set();
      set.add(listener);
      listeners.set(topic, set);
      return () => set.delete(listener);
    },
  };
  const fire = async (topic: string, payload: unknown) => {
    await act(async () => {
      for (const l of listeners.get(topic) ?? []) l(payload);
    });
  };
  return { events, fire };
}

const runStatus = (runId: string, chatId = 10) => ({ runId, taskId: 1, chatId, status: "running" });
const permPending = (runId: string, chatId = 10, requestId = "req-1") => ({
  runId,
  taskId: 1,
  chatId,
  requestId,
  summary: "run ls",
  options: [],
});
const attaches = (client: FakeWsClient) => client.calls.filter((c) => c.method === "run.attach");
const running = (id: string) => summary({ ID: id, Status: "running", FinishedAt: null });

describe("useRunTimeline live adoption", () => {
  async function setup() {
    const client = new FakeWsClient();
    const ev = fakeEvents();
    const hook = renderHook(() => useRunTimeline(client, 1, 10, ev.events));
    client.nth("run.list", 0).resolve([]);
    await flush();
    return { client, ev, result: hook.result };
  }

  it("adopts a run started outside the UI on run.status and attaches once", async () => {
    const { client, ev, result } = await setup();

    await ev.fire("run.status", runStatus("cli-1"));
    client.nth("run.list", 1).resolve([running("cli-1")]);
    await flush();

    expect(result.current.runs?.map((r) => r.id)).toEqual(["cli-1"]);
    expect(attaches(client)).toHaveLength(1);
  });

  it("adopts a run on permission.pending when no run.status was seen, and shows its pending permission", async () => {
    const { client, ev, result } = await setup();

    await ev.fire("permission.pending", permPending("cli-1"));
    client.nth("run.list", 1).resolve([running("cli-1")]);
    await flush();
    await act(async () => {
      client.emit("run.attach", 0, "permission_request", { requestId: "req-1", summary: "run ls", options: [] });
    });

    expect(result.current.runs?.map((r) => r.id)).toEqual(["cli-1"]);
    expect(result.current.runs?.[0]?.pendingPermission?.requestId).toBe("req-1");
    expect(attaches(client)).toHaveLength(1);
  });

  it("a live Deny lands a permission row annotated with the reject option's kind", async () => {
    const { client, ev, result } = await setup();

    await ev.fire("run.status", runStatus("cli-1"));
    client.nth("run.list", 1).resolve([running("cli-1")]);
    await flush();
    await act(async () => {
      client.emit("run.attach", 0, "permission_request", {
        requestId: "req-1",
        summary: "run ls",
        options: [
          { id: "allow", label: "Allow", kind: "allow_once" },
          { id: "deny", label: "Deny", kind: "reject_once" },
        ],
      });
    });
    await act(async () => {
      client.emit("run.attach", 0, "permission_resolved", { requestId: "req-1", optionId: "deny", reason: "human" });
    });

    const item = result.current.runs?.[0]?.items.find((i) => i.kind === "permission");
    expect(item).toMatchObject({ kind: "permission", optionId: "deny", optionKind: "reject_once", reason: "human" });
  });

  it("permission.pending for a known run does not touch its pendingPermission", async () => {
    const { client, ev, result } = await setup();

    await ev.fire("run.status", runStatus("cli-1"));
    client.nth("run.list", 1).resolve([running("cli-1")]);
    await flush();
    await act(async () => {
      client.emit("run.attach", 0, "permission_request", { requestId: "req-1", summary: "run ls", options: [] });
    });
    await act(async () => {
      client.emit("run.attach", 0, "permission_resolved", { requestId: "req-1", reason: "user" });
    });
    expect(result.current.runs?.[0]?.pendingPermission).toBeUndefined();

    await ev.fire("permission.pending", permPending("cli-1"));
    await flush();

    expect(result.current.runs?.[0]?.pendingPermission).toBeUndefined();
    expect(client.calls.filter((c) => c.method === "run.list")).toHaveLength(2);
    expect(attaches(client)).toHaveLength(1);
  });

  it("run.status + permission.pending burst for one run yields one entry and one run.attach", async () => {
    const { client, ev, result } = await setup();

    await ev.fire("run.status", runStatus("cli-1"));
    await ev.fire("permission.pending", permPending("cli-1"));
    await ev.fire("run.status", runStatus("cli-1"));
    client.nth("run.list", 1).resolve([running("cli-1")]);
    await flush();
    await ev.fire("permission.pending", permPending("cli-1"));
    await flush();

    expect(result.current.runs).toHaveLength(1);
    expect(attaches(client)).toHaveLength(1);
  });

  it("run.start response racing an adoption does not duplicate", async () => {
    const { client, ev, result } = await setup();

    let submitted: Promise<unknown> = Promise.resolve();
    await act(async () => {
      submitted = result.current.submitPrompt("claude-native", "hi");
    });
    await ev.fire("run.status", runStatus("r-9"));
    client.nth("run.list", 1).resolve([running("r-9")]);
    await flush();
    await act(async () => {
      client.nth("run.start", 0).resolve({ runId: "r-9" });
      await submitted;
    });
    await flush();

    expect(result.current.runs?.filter((r) => r.id === "r-9")).toHaveLength(1);
    expect(attaches(client)).toHaveLength(1);
  });

  it("events for another chat are ignored", async () => {
    const { client, ev, result } = await setup();

    await ev.fire("run.status", runStatus("other-1", 99));
    await ev.fire("permission.pending", permPending("other-1", 99));
    await flush();

    expect(client.calls.filter((c) => c.method === "run.list")).toHaveLength(1);
    expect(result.current.runs).toEqual([]);
  });
});

describe("useRunTimeline attachments (ADR-0038)", () => {
  const preview: AttachmentMeta = { id: "1", kind: "image", name: "a.png", mimeType: "image/png", size: 4, previewUrl: "blob:local" };
  const stored: AttachmentMeta = { id: "1", kind: "image", name: "a.png", mimeType: "image/png", size: 4, width: 2, height: 2 };

  it("run.list attachments seed the entry (a reload shows them)", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useRunTimeline(client, 1, 10));
    client.nth("run.list", 0).resolve([summary({ attachments: [stored] })]);
    await flush();
    expect(result.current.runs?.[0]?.attachments).toEqual([stored]);
  });

  it("sends attachments only when non-empty and echoes the previews optimistically", async () => {
    const client = new FakeWsClient();
    const { result } = renderHook(() => useRunTimeline(client, 1, 10));
    client.nth("run.list", 0).resolve([]);
    await flush();
    const req = [{ kind: "image" as const, name: "a.png", mimeType: "image/png", data: "AAAA" }];
    let submitted!: Promise<unknown>;
    await act(async () => {
      submitted = result.current.submitPrompt("claude-native", "hi", undefined, undefined, "queue", undefined, req, [preview]);
    });
    expect(client.nth("run.start", 0).params).toMatchObject({ attachments: req });
    await act(async () => {
      client.nth("run.start", 0).resolve({ runId: "run-2" });
      await submitted;
    });
    expect(result.current.runs?.[0]?.attachments).toEqual([preview]);

    await act(async () => {
      void result.current.submitPrompt("claude-native", "plain");
    });
    expect(client.nth("run.start", 1).params).not.toHaveProperty("attachments");
  });

  it("mergeAttachments keeps the local thumbnail when the daemon's metadata arrives", () => {
    expect(mergeAttachments([preview], [stored])).toEqual([{ ...stored, previewUrl: "blob:local" }]);
    expect(mergeAttachments([preview], undefined)).toEqual([preview]);
    expect(mergeAttachments(undefined, [stored])).toEqual([stored]);
    expect(mergeAttachments([{ ...preview, id: "2" }], [stored])).toEqual([stored]);
  });
});
