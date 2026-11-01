import type { Chat, Task, TerminalSessionStatus } from "@/lib/types";

import { diffOf, filesOf, gitOf, splitDiff } from "./git";
import type { DemoSession } from "./session";
import { DemoTerminal } from "./terminal";

/** An RPC error the scripted daemon answers with: `code` travels as the wire error's `code`. */
export class DemoError extends Error {
  constructor(
    readonly code: string,
    message: string,
  ) {
    super(message);
  }
}

export const READ_ONLY_MESSAGE = "Read-only in the demo.";

export interface DemoCtx {
  // The wire params of one request. Rules narrow what they read; the demo daemon is not a validator.
  params: any;
  session: DemoSession;
  /** Sends a streaming event for this request (`{id, event, params}`), before its terminal result. */
  stream(event: string, params: unknown): void;
  /** Aborted by task.cancel (the client's AbortSignal) or when the connection closes. */
  signal: AbortSignal;
}

/**
 * The four kinds of rule (ADR-0051 section 1): `read` answers from fixtures, `scripted` plays a script or replay,
 * `local` mutates the in-memory session, `refused` answers `demo_read_only`.
 */
export interface DemoRule {
  kind: "read" | "scripted" | "local" | "refused";
  run(ctx: DemoCtx): unknown | Promise<unknown>;
}

function read(run: (ctx: DemoCtx) => unknown): DemoRule;
function read(value: unknown): DemoRule;
function read(value: unknown): DemoRule {
  return { kind: "read", run: typeof value === "function" ? (value as (ctx: DemoCtx) => unknown) : () => value };
}
const local = (run: (ctx: DemoCtx) => unknown): DemoRule => ({ kind: "local", run });
const scripted = (run: (ctx: DemoCtx) => unknown | Promise<unknown>): DemoRule => ({ kind: "scripted", run });
const refused = (): DemoRule => ({
  kind: "refused",
  run() {
    throw new DemoError("demo_read_only", READ_ONLY_MESSAGE);
  },
});

const notFound = (what: string): never => {
  throw new DemoError("not_found", `${what} not found`);
};

function taskOf(s: DemoSession, id: number): Task {
  return s.tasks.find((t) => t.ID === id) ?? notFound(`task ${id}`);
}

/** Waits until `signal` aborts; used to hold a stream open. */
function untilAborted(signal: AbortSignal): Promise<never> {
  return new Promise((_, reject) => {
    const fail = () => reject(new DemoError("cancelled", "cancelled"));
    if (signal.aborted) fail();
    else signal.addEventListener("abort", fail, { once: true });
  });
}

function terminalsOf(s: DemoSession, taskId: number): DemoTerminal[] {
  return [...s.terminals.values()].filter((t) => t.taskId === taskId);
}

/** A task.tree node (a root with no children): its run state is derived from the task's latest run. */
function treeNode(s: DemoSession, t: Task) {
  const runs = s.runList({ taskId: t.ID });
  const latest = runs[runs.length - 1];
  const pending = [...s.runs.values()].filter((r) => r.taskId === t.ID && r.pending).length;
  const running = runs.filter((r) => r.Status === "running").map((r) => r.ID);
  const state = pending > 0 ? "waiting_permission" : running.length > 0 ? "running" : !latest ? "idle" : latest.Status === "error" ? "error" : latest.Status === "stopped" ? "stopped" : "done";
  const totals = { runs: runs.length, inputTokens: null, outputTokens: null, cacheReadTokens: null, cacheWriteTokens: null, reasoningTokens: null, costUsd: null };
  return {
    id: t.ID,
    parentId: null,
    workspaceId: t.WorkspaceID,
    title: t.Title,
    depth: 0,
    taskStatus: t.Status,
    tier: null,
    childCount: 0,
    run: { state, runningRunIds: running, pendingPermissions: pending },
    counts: { running: state === "running" ? 1 : 0, waitingPermission: pending, failed: state === "error" ? 1 : 0, done: state === "done" ? 1 : 0, idle: state === "idle" ? 1 : 0 },
    usage: { own: totals, subtree: totals, archivedDescendants: 0 },
  };
}

const EMPTY_REPLAY = { frames: [{ afterMs: 100, text: "\x1b[2mdemo terminal\x1b[0m\r\n" }] };

/** Tasks/chats created in the session get a worktree-less shell: they exist until reload. */
function newTask(s: DemoSession, p: { workspaceId: number; title?: string; spaceId?: number | null; parentTaskId?: number | null }): Task {
  const now = s.nowIso();
  const id = s.allocTaskId();
  const task: Task = {
    ID: id,
    WorkspaceID: p.workspaceId,
    SpaceID: p.spaceId ?? null,
    Title: p.title || "New task",
    Status: "active",
    WorktreePath: `/home/demo/acme/${id}`,
    Branch: `demo/task-${id}`,
    CreatedAt: now,
    UpdatedAt: now,
    ArchivedAt: null,
    ParentTaskID: p.parentTaskId ?? null,
  };
  s.tasks.push(task);
  const chat: Chat = { ID: s.allocChatId(), TaskID: id, Title: "Chat", Provider: null, AgentSession: null, CreatedAt: now, ArchivedAt: null };
  s.chats.push(chat);
  s.notify("task.created", { task });
  return task;
}

/** Every method the UI calls has a rule here; the coverage test (demo-coverage.test.ts) fails for one that does not. */
export const RULES: Record<string, DemoRule> = {
  // ---- connection ---------------------------------------------------------------------------------------------------
  "events.subscribe": local(() => ({})),
  "daemon.info": read((c) => c.session.scenario.daemon),

  // ---- the workspace tree -------------------------------------------------------------------------------------------
  "workspace.list": read((c) => c.session.workspaces),
  "workspace.get": read((c) => {
    const w = c.session.workspaces.find((x) => x.ID === c.params.id) ?? notFound("workspace");
    return { ...w, SetupScript: "", TeardownScript: "" };
  }),
  "workspace.branches": read({ defaultBase: "main", current: "main", branches: [{ name: "main", kind: "local" }] }),
  "workspace.defaultBase": read({ ref: "main", detected: true, override: "" }),
  "workspace.readiness": read((c) => ({ workspaceId: c.params.workspaceId ?? c.params.id, findings: [], checkedAt: c.session.nowIso() })),
  "space.list": read((c) => c.session.spaces.filter((x) => x.WorkspaceID === c.params.workspaceId)),
  "task.list": read((c) => {
    const p = c.params ?? {};
    return c.session.tasks.filter(
      (t) =>
        (p.workspaceId === undefined || t.WorkspaceID === p.workspaceId) &&
        (p.parentTaskId === undefined || t.ParentTaskID === p.parentTaskId) &&
        (p.archived ? t.ArchivedAt !== null : t.ArchivedAt === null),
    );
  }),
  "task.get": read((c) => taskOf(c.session, c.params.id)),
  "task.tree": read((c) => ({ nodes: [treeNode(c.session, taskOf(c.session, c.params.taskId))], truncated: false })),
  "chat.list": read((c) => c.session.chats.filter((x) => x.TaskID === c.params.taskId && x.ArchivedAt === null)),
  "chat.get": read((c) => c.session.chats.find((x) => x.ID === c.params.id) ?? notFound("chat")),
  "chat.queueList": read({ items: [] }),

  // ---- runs: the timeline, the stream, sending a prompt -------------------------------------------------------------
  "run.list": read((c) => c.session.runList(c.params ?? {})),
  "run.logs": read((c) => c.session.logsOf(c.params.runId) ?? notFound("run")),
  "run.attach": scripted(async (c) => {
    const run = c.session.runs.get(c.params.runId) ?? notFound("run");
    let detach = () => {};
    const ended = new Promise<{ runId: string; stopReason: string }>((resolve) => {
      detach = run.attach((item) => {
        if ("event" in item) {
          const { type, ...rest } = item.event;
          c.stream(type, rest);
        } else resolve({ runId: run.id, stopReason: item.end.stopReason });
      });
    });
    try {
      return await Promise.race([ended, untilAborted(c.signal)]);
    } finally {
      detach();
    }
  }),
  "run.start": scripted((c) => {
    const p = c.params;
    const run = c.session.startReply(p.taskId, p.chatId, p.provider ?? "claude-code", String(p.prompt ?? ""));
    return { runId: run.id };
  }),
  "task.prompt": scripted((c) => {
    const p = c.params;
    const chat = c.session.chats.find((x) => x.TaskID === p.taskId);
    return { runId: c.session.startReply(p.taskId, p.chatId ?? chat?.ID ?? 0, p.provider ?? "claude-code", String(p.prompt ?? "")).id };
  }),
  "run.respondPermission": scripted((c) => {
    const { runId, requestId, optionId } = c.params;
    if (!c.session.respond(runId, requestId, optionId)) throw new DemoError("not_found", "no such pending permission request");
    return {};
  }),
  "run.stop": local((c) => {
    c.session.stopRun(c.params.runId);
    return {};
  }),
  "run.setPermissionMode": local((c) => ({ permissionMode: c.params.permissionMode ?? "default", autoAccept: Boolean(c.params.autoAccept) })),
  "run.listConfigOptions": read({ options: [] }),
  "run.setConfigOption": refused(),
  "run.attachment": refused(),

  // ---- diff, git, files ---------------------------------------------------------------------------------------------
  "task.diff": read((c) => ({ diff: diffOf(c.session, c.params.taskId) })),
  "task.fileDiff": read((c) => ({ diff: splitDiff(diffOf(c.session, c.params.taskId)).find((f) => f.path === c.params.path)?.chunk ?? "" })),
  "task.files": read((c) => ({ files: filesOf(c.session, c.params.taskId) })),
  "task.git": read((c) => gitOf(c.session, taskOf(c.session, c.params.taskId))),
  "task.gitList": read((c) => ({ items: c.session.tasks.filter((t) => t.WorkspaceID === c.params.workspaceId).map((t) => gitOf(c.session, t)) })),
  "task.gitFocus": local(() => ({})),
  "task.searchIndex": read((c) => ({ paths: Object.keys(c.session.scenario.files[c.params.taskId] ?? {}) })),
  "file.list": read((c) => {
    const files = c.session.scenario.files[c.params.taskId] ?? {};
    const dir = String(c.params.path ?? "").replace(/^\/+|\/+$/g, "");
    const prefix = dir ? `${dir}/` : "";
    const seen = new Map<string, { name: string; isDir: boolean; size: number }>();
    for (const [path, text] of Object.entries(files)) {
      if (!path.startsWith(prefix)) continue;
      const rest = path.slice(prefix.length);
      const slash = rest.indexOf("/");
      if (slash === -1) seen.set(rest, { name: rest, isDir: false, size: text.length });
      else seen.set(rest.slice(0, slash), { name: rest.slice(0, slash), isDir: true, size: 0 });
    }
    return [...seen.values()];
  }),
  "file.read": read((c) => {
    const text = c.session.scenario.files[c.params.taskId]?.[String(c.params.path).replace(/^\/+/, "")];
    if (text === undefined) return notFound("file");
    return { content: text, mtime: c.session.ago(3_600_000) };
  }),
  "file.stat": read((c) => {
    const path = String(c.params.path).replace(/^\/+/, "");
    const text = c.session.scenario.files[c.params.taskId]?.[path];
    if (text === undefined) return notFound("file");
    return { size: text.length, mtime: c.session.ago(3_600_000), kind: "text", mime: "text/plain", ext: path.split(".").pop() ?? "", mismatch: false, previewable: false };
  }),
  "file.write": refused(),
  "fs.listDir": read((c) => ({ path: c.params?.path ?? "/", parent: "/", entries: [] })),

  // ---- terminal -----------------------------------------------------------------------------------------------------
  "terminal.list": read((c): TerminalSessionStatus[] =>
    terminalsOf(c.session, c.params.taskId).map((t) => ({ ID: t.id, TaskID: t.taskId, StartedAt: t.startedAt, Status: t.status, ClosedAt: null })),
  ),
  "terminal.create": scripted((c) => {
    const taskId = c.params.taskId as number;
    const id = `demo-term-${c.session.terminals.size + 1}`;
    const replay = c.session.scenario.terminals[taskId] ?? EMPTY_REPLAY;
    c.session.terminals.set(id, new DemoTerminal(id, taskId, c.session.nowIso(), c.session.clock, replay, c.session.reducedMotion));
    return { terminalId: id };
  }),
  "terminal.attach": scripted(async (c) => {
    const term = c.session.terminals.get(c.params.terminalId) ?? notFound("terminal");
    let detach = () => {};
    const ended = new Promise<{ terminalId: string }>((resolve) => {
      detach = term.attach((item) => {
        if ("data" in item) c.stream("data", { data: item.data });
        else resolve({ terminalId: term.id });
      });
    });
    try {
      return await Promise.race([ended, untilAborted(c.signal)]);
    } finally {
      detach();
    }
  }),
  "terminal.write": local((c) => {
    c.session.terminals.get(c.params.terminalId)?.write(String(c.params.data ?? ""));
    return {};
  }),
  "terminal.resize": local(() => ({})),
  "terminal.close": local((c) => {
    c.session.terminals.get(c.params.terminalId)?.close();
    return {};
  }),

  // ---- inbox --------------------------------------------------------------------------------------------------------
  "inbox.list": read((c) => ({ items: c.session.inbox.filter((i) => i.state === "new" || i.state === "seen") })),
  "inbox.count": read((c) => {
    const open = c.session.inbox.filter((i) => i.state === "new" || i.state === "seen");
    const byKind: Record<string, number> = {};
    for (const i of open) byKind[i.kind] = (byKind[i.kind] ?? 0) + 1;
    return { total: open.length, unseen: open.filter((i) => i.state === "new").length, byKind };
  }),
  "inbox.seen": local((c) => c.session.markInbox(c.params.keys, "seen")),
  "inbox.snooze": local((c) => c.session.markInbox(c.params.keys, "snoozed", c.params.until)),
  "inbox.resolve": local((c) => c.session.markInbox(c.params.keys, "resolved")),

  // ---- usage --------------------------------------------------------------------------------------------------------
  "usage.summary": read((c) => c.session.scenario.usage.summary[c.params?.groupBy as keyof DemoSession["scenario"]["usage"]["summary"]] ?? []),
  "provider.usage": read((c) => c.session.scenario.usage.providerUsage),
  "account.usage": read((c) => c.session.scenario.usage.accountUsage),
  "scratch.usage": read({ totalBytes: 0, totalFiles: 0, dirs: 0, reclaimableDirs: 0, reclaimableBytes: 0 }),

  // ---- creating things in the session (they exist until reload) -----------------------------------------------------
  "task.create": local((c) => newTask(c.session, c.params)),
  "chat.create": local((c) => {
    const now = c.session.nowIso();
    const chat: Chat = { ID: c.session.allocChatId(), TaskID: c.params.taskId, Title: c.params.title || "Chat", Provider: null, AgentSession: null, CreatedAt: now, ArchivedAt: null };
    c.session.chats.push(chat);
    c.session.notify("chat.created", { chat });
    return chat;
  }),
  "chat.rename": local((c) => {
    const chat = c.session.chats.find((x) => x.ID === c.params.id) ?? notFound("chat");
    chat.Title = String(c.params.title ?? chat.Title);
    c.session.notify("chat.updated", { chat });
    return chat;
  }),
  "chat.archive": local((c) => {
    const chat = c.session.chats.find((x) => x.ID === c.params.id) ?? notFound("chat");
    chat.ArchivedAt = c.session.nowIso();
    c.session.notify("chat.archived", { chat });
    return chat;
  }),
  "chat.queueCancel": local(() => ({})),

  // ---- settings and everything else: lists render fixtures, every write is refused ----------------------------------
  "account.list": read((c) => c.session.scenario.accounts),
  "provider.list": read((c) => c.session.scenario.providers),
  "provider.test": refused(),
  "provider.recipes": read({ recipes: [] }),
  "provider.dirSuggestions": read({ suggestions: [] }),
  "profile.list": read((c) => c.session.scenario.profiles),
  "skill.list": read([]),
  "mcp.list": read([]),
  "plugin.list": read({ globalEnabled: false, plugins: [] }),
  "plugin.commands": read([]),
  "plugin.themes": read([]),
  "plugin.panels": read({ panels: [] }),
  "plugin.view": read({}),
  "plugin.call": refused(),
  "learning.status": read({ enabled: false, workspaces: [], budget: null }),
  "learning.skills.list": read([]),
  "learning.proposals.list": read([]),
  "learning.proposals.prevalence": read({}),
  "learning.probe.list": read([]),
  "learning.jobs.list": read([]),
  "policy.get": read({ enabled: false, valid: true, exists: false, rules: [] }),
  "policy.audit": read({ rows: [], next: 0 }),
  "steward.get": refused(),
  "steward.frame": refused(),
  "forge.status": read({ provider: "github", canSetToken: false, cli: { installed: false, version: "" }, hosts: [] }),
  "forge.search": read({ items: [] }),
  "fleet.host.list": read({ hosts: [] }),
  "fleet.host.audit": read({ rows: [] }),
  "automation.list": read({ enabled: false, paused: false, automations: [] }),
  "automation.audit": read({ rows: [] }),
  "automation.memory.get": read({ memory: "" }),
  "task.prDetails": read(() => notFound("pull request")),
  "task.prDetailsFocus": local(() => ({})),
  "task.prPreview": refused(),
  "task.prCheckLog": refused(),
  "task.archivePreview": refused(),
  "learning.skills.get": refused(),
  "learning.proposals.get": refused(),
  "learning.probe.get": refused(),
  "learning.probe.estimate": refused(),
  "learning.probe.start": refused(),
  "learning.probe.cancel": refused(),
  "learning.skills.export": refused(),
  "learning.skills.undo": refused(),
  "learning.proposals.approve": refused(),
  "learning.proposals.reject": refused(),
  "learning.setMode": refused(),
  "learning.reflect": refused(),
  "learning.curate": refused(),
  "learning.forget": refused(),
};

/**
 * Writes (and binary reads) the demo refuses with `demo_read_only`. Listed by name, not guessed from a verb: the coverage test
 * (demo-coverage.test.ts) fails for any method the UI calls that is in neither RULES nor this list.
 */
export const REFUSED_METHODS = [
  "account.add", "account.oauthComplete", "account.oauthStart", "account.remove",
  "automation.delete", "automation.fire", "automation.memory.clear",
  "file.readRange",
  "fleet.host.add", "fleet.host.approve", "fleet.host.disable_reverse", "fleet.host.enable_reverse", "fleet.host.grant", "fleet.host.invite",
  "fleet.host.labels", "fleet.host.reject", "fleet.host.rename", "fleet.host.revoke", "fleet.host.rm",
  "forge.logout", "forge.setToken",
  "mcp.create", "mcp.delete", "mcp.setEnabled", "mcp.update",
  "plugin.disable", "plugin.enable", "plugin.install", "plugin.reload", "plugin.remove", "plugin.update",
  "profile.create", "profile.delete", "profile.update",
  "provider.addFromRecipe", "provider.addInstance", "provider.clearCooldown", "provider.clearSecret", "provider.reload",
  "provider.setDefaultModel", "provider.setEnv", "provider.setSecret", "provider.updateInstance",
  "scratch.clean",
  "space.create", "space.delete",
  "task.archive", "task.archiveFinished", "task.commit", "task.createPr", "task.move", "task.push", "task.restore", "task.retrySetup",
  "task.setBudget", "task.stage", "task.stopTree", "task.updateFromBase", "task.updatePr",
  "workspace.create", "workspace.delete", "workspace.setDefaultBase", "workspace.setScripts",
] as const;

for (const method of REFUSED_METHODS) RULES[method] ??= refused();

export function ruleFor(method: string): DemoRule | undefined {
  return RULES[method];
}
