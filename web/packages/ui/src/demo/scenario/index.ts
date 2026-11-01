import type { InboxItem } from "@/lib/inbox-types";
import type { Chat, RunLogEvent, RunLogsResult, RunSummary, Space, Task, Workspace } from "@/lib/types";

import type { DemoScenario } from "../types";
import { DIFFS, FILES, LIVE_EXTRA_DIFF } from "./diffs";
import {
  DARKMODE_RUN_EVENTS,
  FLAKY_RUN_EVENTS,
  GUIDE_RUN_EVENTS,
  LIVE_SCRIPT,
  PAGINATION_RUN_EVENTS,
  TOOLCHAIN_RUN_EVENTS,
  cannedReply,
} from "./runs";
import { TERMINALS } from "./terminal";
import { PROVIDERS, usageFixtures } from "./usage";

const MIN = 60_000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;

/**
 * The shipped "tour" scenario (ADR-0051 section 2). Every name, path, branch and number is synthetic; every timestamp is
 * written relative to `now`, so the demo never says "3 years ago".
 */
export function buildTour(now: number): DemoScenario {
  const ago = (ms: number) => new Date(now - ms).toISOString();

  const workspaces: Workspace[] = [
    { ID: 1, Path: "/home/demo/acme-api", Title: "acme-api", RoutingPolicy: "", DefaultBase: "main", CreatedAt: ago(30 * DAY), UpdatedAt: ago(DAY) },
    { ID: 2, Path: "/home/demo/web-dashboard", Title: "web-dashboard", RoutingPolicy: "", DefaultBase: "main", CreatedAt: ago(20 * DAY), UpdatedAt: ago(2 * DAY) },
    { ID: 3, Path: "/home/demo/docs-site", Title: "docs-site", RoutingPolicy: "", DefaultBase: "main", CreatedAt: ago(12 * DAY), UpdatedAt: ago(3 * DAY) },
  ];

  const spaces: Space[] = [{ ID: 1, WorkspaceID: 1, Title: "Q4 hardening", EnvData: "{}", CreatedAt: ago(10 * DAY), UpdatedAt: ago(DAY) }];

  const task = (ID: number, WorkspaceID: number, SpaceID: number | null, Title: string, Branch: string, updated: number): Task => ({
    ID,
    WorkspaceID,
    SpaceID,
    Title,
    Status: "active",
    WorktreePath: `/home/demo/${workspaces[WorkspaceID - 1].Title}/.smind/worktrees/${Branch.replace("/", "-")}`,
    Branch,
    CreatedAt: ago(updated + HOUR),
    UpdatedAt: ago(updated),
    ArchivedAt: null,
    ParentTaskID: null,
    SetupStatus: "",
  });

  const tasks: Task[] = [
    task(1, 1, 1, "Add rate limiting to the public API", "feat/rate-limit", 2 * MIN),
    task(2, 1, null, "Fix flaky TestRetryBacksOff in the queue worker", "fix/flaky-retry-test", 3 * HOUR),
    task(3, 1, 1, "Upgrade the module to Go 1.24", "chore/go-1-24", 5 * HOUR),
    task(4, 2, null, "Dark mode toggle on the settings page", "feat/dark-mode", DAY),
    task(5, 2, null, "Paginate the audit log", "feat/audit-pagination", 2 * DAY),
    task(6, 3, null, "Write the v1 to v2 migration guide", "docs/migrate-v2", 3 * DAY),
  ];

  const chats: Chat[] = tasks.map((t) => ({ ID: t.ID, TaskID: t.ID, Title: "Chat", Provider: "claude-code", AgentSession: null, CreatedAt: t.CreatedAt, ArchivedAt: null }));

  const finished = (ID: string, taskId: number, prompt: string, started: number, status: RunSummary["Status"], stopReason: string): RunSummary => ({
    ID,
    TaskID: taskId,
    ChatID: taskId,
    Provider: "claude-code",
    Prompt: prompt,
    Status: status,
    StartedAt: ago(started),
    FinishedAt: ago(started - 4 * MIN),
    StopReason: stopReason,
    Err: status === "error" ? "The Go 1.24 toolchain could not be downloaded." : "",
    PermissionMode: "default",
    AutoAccept: false,
    ThinkingLevel: "",
    Model: "claude-sonnet-5-5",
  });

  const run = (id: string, events: RunLogEvent[], status: RunLogsResult["status"], stopReason: string): RunLogsResult => ({ runId: id, status, stopReason, events });
  const PROMPTS = {
    flaky: "TestRetryBacksOff is flaky on CI. Find out why and fix it without adding sleeps.",
    go: "Upgrade the module to Go 1.24 and fix whatever breaks.",
    dark: "Add a dark mode toggle to the settings page.",
    page: "Paginate the audit log table, 50 rows per page.",
    guide: "Write the v1 to v2 migration guide for the docs site.",
  };

  const runs: RunSummary[] = [
    finished("demo-run-flaky", 2, PROMPTS.flaky, 3 * HOUR, "done", "end_turn"),
    finished("demo-run-go", 3, PROMPTS.go, 5 * HOUR, "error", "error"),
    finished("demo-run-dark", 4, PROMPTS.dark, DAY, "done", "end_turn"),
    finished("demo-run-page", 5, PROMPTS.page, 2 * DAY, "done", "end_turn"),
    finished("demo-run-guide", 6, PROMPTS.guide, 3 * DAY, "done", "end_turn"),
  ];

  const runLogs: Record<string, RunLogsResult> = {
    "demo-run-flaky": run("demo-run-flaky", FLAKY_RUN_EVENTS, "done", "end_turn"),
    "demo-run-go": { ...run("demo-run-go", TOOLCHAIN_RUN_EVENTS, "error", "error"), err: "The Go 1.24 toolchain could not be downloaded." },
    "demo-run-dark": run("demo-run-dark", DARKMODE_RUN_EVENTS, "done", "end_turn"),
    "demo-run-page": run("demo-run-page", PAGINATION_RUN_EVENTS, "done", "end_turn"),
    "demo-run-guide": run("demo-run-guide", GUIDE_RUN_EVENTS, "done", "end_turn"),
  };

  const inbox: InboxItem[] = [
    {
      key: "perm:demo-req-1",
      kind: "permission",
      taskId: 1,
      title: "Add rate limiting to the public API",
      summary: "Write internal/ratelimit/limiter.go",
      createdAt: ago(0),
      state: "new",
      attention: "high",
      links: { taskId: 1, runId: "demo-live", permissionRequestId: "demo-req-1" },
    },
    {
      key: "run_failed:demo-run-go",
      kind: "run_failed",
      taskId: 3,
      title: "Upgrade the module to Go 1.24",
      summary: "The run failed: the Go 1.24 toolchain could not be downloaded",
      createdAt: ago(5 * HOUR),
      state: "new",
      attention: "normal",
      links: { taskId: 3, runId: "demo-run-go" },
    },
    {
      key: "verdict:1",
      kind: "verdict",
      taskId: 4,
      title: "Dark mode toggle on the settings page",
      summary: "Validation passed at the current commit: ready for your merge decision",
      createdAt: ago(20 * HOUR),
      state: "new",
      attention: "normal",
      links: { taskId: 4, validationId: 1 },
    },
    {
      key: "stall:5:1",
      kind: "stall",
      taskId: 5,
      title: "Paginate the audit log",
      summary: "This task has been waiting for a reply for two days",
      createdAt: ago(DAY),
      state: "seen",
      attention: "normal",
      links: { taskId: 5 },
    },
  ];

  return {
    version: 1,
    now,
    daemon: { version: "demo", commit: "demo", files: { maxReadBytes: 1 << 20, maxChunkBytes: 1 << 16, previewKinds: [] } },
    workspaces,
    spaces,
    tasks,
    chats,
    runs,
    runLogs,
    live: {
      taskId: 1,
      chatId: 1,
      runId: "demo-live",
      provider: "claude-code",
      prompt: "Add rate limiting to the public API: 100 requests per minute per API key, answer 429 with a Retry-After header, and add tests.",
      script: LIVE_SCRIPT,
    },
    reply: cannedReply,
    diffs: DIFFS,
    liveTaskExtraDiff: LIVE_EXTRA_DIFF,
    terminals: TERMINALS,
    inbox,
    usage: usageFixtures(now),
    providers: PROVIDERS,
    accounts: [{ id: 1, provider: "anthropic", label: "Claude (demo)", credentialType: "oauth", createdAt: ago(30 * DAY), updatedAt: ago(DAY), hasIdentity: true }],
    profiles: [
      { ID: 1, Name: "Careful coder", Provider: "claude-code", PermissionMode: "default", AutoAccept: false, ThinkingLevel: "", Model: "claude-sonnet-5-5", Notes: "Asks before every edit or command.", CreatedAt: ago(20 * DAY), UpdatedAt: ago(20 * DAY) },
      { ID: 2, Name: "Fast fixer", Provider: "claude-code", PermissionMode: "acceptEdits", AutoAccept: false, ThinkingLevel: "", Model: "claude-haiku-5-5", Notes: "Small, well-scoped changes.", CreatedAt: ago(15 * DAY), UpdatedAt: ago(15 * DAY) },
    ],
    files: FILES,
  };
}
