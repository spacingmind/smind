import type { InboxCounts, InboxItem } from "@/lib/inbox-types";
import { FakeUsageDaemon } from "@/test/usage-test-client";

export function permissionItem(over: Partial<InboxItem> = {}): InboxItem {
  return {
    key: "perm:q1",
    kind: "permission",
    taskId: 5,
    title: "Add the widget",
    summary: "Write src/widget.ts",
    createdAt: new Date(Date.now() - 2 * 60_000).toISOString(),
    state: "new",
    attention: "high",
    links: { taskId: 5, runId: "run-1", permissionRequestId: "q1" },
    ...over,
  };
}

export function verdictItem(over: Partial<InboxItem> = {}): InboxItem {
  return {
    key: "verdict:7",
    kind: "verdict",
    taskId: 6,
    title: "Fix the thing",
    summary: "Validation passed at the current commit: ready for your merge decision",
    createdAt: new Date(Date.now() - 3_600_000).toISOString(),
    state: "new",
    attention: "normal",
    links: { taskId: 6, validationId: 7 },
    ...over,
  };
}

export function stallItem(over: Partial<InboxItem> = {}): InboxItem {
  return {
    key: "stall:8:1000",
    kind: "stall",
    taskId: 8,
    title: "Long running lead",
    summary: "This task tree has gone quiet",
    createdAt: new Date(Date.now() - 86_400_000).toISOString(),
    state: "seen",
    attention: "normal",
    links: { taskId: 8 },
    ...over,
  };
}

/**
 * A FakeUsageDaemon that answers the inbox surface: inbox.list / inbox.count from `items` (the daemon's rules: a
 * permission cannot be snoozed or resolved), the three setters (recorded; they change `items` like the daemon does),
 * run.logs for the permission request behind a row, and run.respondPermission (recorded).
 */
export class InboxDaemon extends FakeUsageDaemon {
  items: InboxItem[] = [];
  /** Answers run.logs for a run: the events it replays. */
  logs: Record<string, unknown[]> = {};

  constructor() {
    super([]);
    const holder = this.client as unknown as { call: (m: string, p?: unknown) => Promise<unknown> };
    const inner = holder.call;
    holder.call = async (method, params) => {
      const p = (params ?? {}) as Record<string, unknown>;
      switch (method) {
        case "inbox.list":
          this.calls.push({ method, params });
          return { items: this.items.filter((i) => i.state === "new" || i.state === "seen") };
        case "inbox.count": {
          this.calls.push({ method, params });
          const visible = this.items.filter((i) => i.state === "new" || i.state === "seen");
          const counts: InboxCounts = {
            total: visible.length,
            unseen: visible.filter((i) => i.state === "new" || i.kind === "permission").length,
            byKind: {},
          };
          for (const i of visible) counts.byKind[i.kind] = (counts.byKind[i.kind] ?? 0) + 1;
          return counts;
        }
        case "inbox.seen":
        case "inbox.snooze":
        case "inbox.resolve": {
          this.calls.push({ method, params });
          const keys = (p.keys as string[]) ?? [];
          if (method !== "inbox.seen" && keys.some((k) => k.startsWith("perm:"))) throw new Error("a permission request cannot be answered here");
          const state = method === "inbox.seen" ? "seen" : method === "inbox.snooze" ? "snoozed" : "resolved";
          this.items = this.items.map((i) => (keys.includes(i.key) ? { ...i, state } : i));
          return { ok: true };
        }
        case "run.logs":
          this.calls.push({ method, params });
          return { runId: p.runId, status: "running", events: this.logs[p.runId as string] ?? [] };
        case "run.respondPermission":
          this.calls.push({ method, params });
          this.items = this.items.filter((i) => i.links.permissionRequestId !== p.requestId);
          return {};
      }
      return inner.call(this.client, method, params);
    };
  }

  callsOf(method: string): { method: string; params: unknown }[] {
    return this.calls.filter((c) => c.method === method);
  }
}

export const REQUEST_EVENTS = [
  {
    type: "permission_request",
    requestId: "q1",
    summary: "Write src/widget.ts",
    options: [
      { id: "allow-1", label: "Allow", kind: "allow_once" },
      { id: "deny-1", label: "Deny", kind: "reject_once" },
    ],
    detail: { kind: "write", path: "src/widget.ts", newBytes: 120, newLines: 6 },
  },
];
