import type { InboxItem } from "@/lib/inbox-types";
import type { Chat, RunLogEvent, RunLogsResult, RunSummary, Task, Workspace } from "@/lib/types";

import type { DemoScenario } from "../types";
import { DIFFS, FILES, LIVE_EXTRA_DIFF } from "./diffs";
import {
  ANALYSIS_EVENTS,
  DESIGN_EVENTS,
  LIVE_SCRIPT,
  ONBOARDING_EVENTS,
  PARSE_DATE_EVENTS,
  PEER_FAILOVER_EVENTS,
  PEER_SCHEMA_EVENTS,
  RETRY_EVENTS,
  REVIEW_EVENTS,
  WEBHOOK_PROMPT,
  cannedReply,
} from "./runs";
import { TERMINALS } from "./terminal";
import { PROVIDERS, usageFixtures } from "./usage";

const MIN = 60_000;
const HOUR = 60 * MIN;
const DAY = 24 * HOUR;

/** Task ids: the Lead of the design task is #12, as in the landing hero's `task_new ... parent: #12`. */
export const TASK = { webhook: 1, retry: 2, parseDate: 3, review: 4, analysis: 5, onboarding: 6, design: 12, peerSchema: 13, peerFailover: 14 } as const;

/**
 * The shipped "tour" scenario (ADR-0051 section 2): the nine tasks of the landing page's hero, in the real UI. Every name, path,
 * branch and number is synthetic; every timestamp is written relative to `now`, so the demo never says "3 years ago".
 */
export function buildTour(now: number): DemoScenario {
  const ago = (ms: number) => new Date(now - ms).toISOString();

  const workspaces: Workspace[] = [
    { ID: 1, Path: "/home/demo/checkout-api", Title: "checkout-api", RoutingPolicy: "", DefaultBase: "main", CreatedAt: ago(30 * DAY), UpdatedAt: ago(DAY) },
    { ID: 2, Path: "/home/demo/web-app", Title: "web-app", RoutingPolicy: "", DefaultBase: "main", CreatedAt: ago(20 * DAY), UpdatedAt: ago(2 * DAY) },
    { ID: 3, Path: "/home/demo/platform", Title: "platform", RoutingPolicy: "", DefaultBase: "main", CreatedAt: ago(12 * DAY), UpdatedAt: ago(3 * HOUR) },
  ];

  const task = (ID: number, WorkspaceID: number, Title: string, Branch: string, updated: number, ParentTaskID: number | null = null): Task => ({
    ID,
    WorkspaceID,
    SpaceID: null,
    Title,
    Status: "active",
    WorktreePath: `/home/demo/${workspaces[WorkspaceID - 1].Title}/.smind/worktrees/${Branch.replace("/", "-")}`,
    Branch,
    CreatedAt: ago(updated + HOUR),
    UpdatedAt: ago(updated),
    ArchivedAt: null,
    ParentTaskID,
    SetupStatus: "",
  });

  const tasks: Task[] = [
    task(TASK.webhook, 1, "Rate-limit the webhook handler", "feat/webhook-rate-limit", 9 * MIN),
    task(TASK.retry, 1, "Why does TestRetry fail on CI only?", "fix/retry-ci-flake", 2 * MIN),
    task(TASK.parseDate, 1, "Add a unit test for parseDate", "test/parse-date", 8 * MIN),
    task(TASK.review, 1, "Review PR #42 for race conditions", "review/pr-42", HOUR),
    task(TASK.analysis, 1, "Why did p99 latency double since Tuesday?", "analysis/p99-latency", 3 * HOUR),
    task(TASK.onboarding, 2, "Explain this repo to me", "docs/explain-repo", DAY),
    task(TASK.design, 3, "Design a multi-region job queue", "design/multi-region-queue", 2 * HOUR),
    task(TASK.peerSchema, 3, "Queue schema and leases", "design/queue-schema", 2 * HOUR, TASK.design),
    task(TASK.peerFailover, 3, "Failover modes", "design/failover-modes", 2 * HOUR, TASK.design),
  ];

  const chats: Chat[] = tasks.map((t) => ({ ID: t.ID, TaskID: t.ID, Title: "Chat", Provider: "claude-code", AgentSession: null, CreatedAt: t.CreatedAt, ArchivedAt: null }));

  const PROMPT = {
    retry: "Why does TestRetry fail on CI only?",
    parseDate: "Add a unit test for parseDate",
    review: "Review PR #42 for race conditions",
    analysis: "Why did p99 latency double since Tuesday?",
    onboarding: "Explain this repo to me",
    design: "Design a multi-region job queue",
    peerSchema: "Draft the queue table and the lease protocol",
    peerFailover: "List the failure modes and what each costs",
  };

  /** A finished run; `age` is how long ago it started, `secs` how long it ran (the landing hero's run footers). */
  const finished = (ID: string, taskId: number, prompt: string, age: number, secs: number): RunSummary => ({
    ID,
    TaskID: taskId,
    ChatID: taskId,
    Provider: "claude-code",
    Prompt: prompt,
    Status: "done",
    StartedAt: ago(age),
    FinishedAt: ago(age - secs * 1000),
    StopReason: "end_turn",
    Err: "",
    PermissionMode: "default",
    AutoAccept: false,
    ThinkingLevel: "",
    Model: "claude-sonnet-5-5",
  });
  const logs = (id: string, events: RunLogEvent[]): RunLogsResult => ({ runId: id, status: "done", stopReason: "end_turn", events });

  const runs: RunSummary[] = [
    finished("demo-run-retry", TASK.retry, PROMPT.retry, 2 * MIN + 9_000, 9),
    finished("demo-run-test", TASK.parseDate, PROMPT.parseDate, 8 * MIN + 12_000, 12),
    finished("demo-run-review", TASK.review, PROMPT.review, HOUR + 18_000, 18),
    finished("demo-run-analysis", TASK.analysis, PROMPT.analysis, 3 * HOUR + 31_000, 31),
    finished("demo-run-onboarding", TASK.onboarding, PROMPT.onboarding, DAY + 6_000, 6),
    finished("demo-run-design", TASK.design, PROMPT.design, 2 * HOUR + 42_000, 42),
    finished("run-14", TASK.peerSchema, PROMPT.peerSchema, 2 * HOUR + 40_000, 14),
    finished("run-15", TASK.peerFailover, PROMPT.peerFailover, 2 * HOUR + 40_000, 16),
  ];

  const runLogs: Record<string, RunLogsResult> = {
    "demo-run-retry": logs("demo-run-retry", RETRY_EVENTS),
    "demo-run-test": logs("demo-run-test", PARSE_DATE_EVENTS),
    "demo-run-review": logs("demo-run-review", REVIEW_EVENTS),
    "demo-run-analysis": logs("demo-run-analysis", ANALYSIS_EVENTS),
    "demo-run-onboarding": logs("demo-run-onboarding", ONBOARDING_EVENTS),
    "demo-run-design": logs("demo-run-design", DESIGN_EVENTS),
    "run-14": logs("run-14", PEER_SCHEMA_EVENTS),
    "run-15": logs("run-15", PEER_FAILOVER_EVENTS),
  };

  const inbox: InboxItem[] = [
    {
      key: "perm:demo-req-1",
      kind: "permission",
      taskId: TASK.webhook,
      title: "Rate-limit the webhook handler",
      summary: "Write internal/webhook/limiter.go",
      createdAt: ago(0),
      state: "new",
      attention: "high",
      links: { taskId: TASK.webhook, runId: "demo-live", permissionRequestId: "demo-req-1" },
    },
    {
      key: "verdict:1",
      kind: "verdict",
      taskId: TASK.retry,
      title: "Why does TestRetry fail on CI only?",
      summary: "Validation passed at the current commit: ready for your merge decision",
      createdAt: ago(2 * MIN),
      state: "new",
      attention: "normal",
      links: { taskId: TASK.retry, validationId: 1 },
    },
    {
      key: "verdict:2",
      kind: "verdict",
      taskId: TASK.parseDate,
      title: "Add a unit test for parseDate",
      summary: "Validation passed at the current commit: ready for your merge decision",
      createdAt: ago(8 * MIN),
      state: "new",
      attention: "normal",
      links: { taskId: TASK.parseDate, validationId: 2 },
    },
  ];

  return {
    version: 1,
    now,
    daemon: { version: "demo", commit: "demo", files: { maxReadBytes: 1 << 20, maxChunkBytes: 1 << 16, previewKinds: [] } },
    workspaces,
    spaces: [],
    tasks,
    chats,
    runs,
    runLogs,
    live: {
      taskId: TASK.webhook,
      chatId: TASK.webhook,
      runId: "demo-live",
      provider: "claude-code",
      prompt: WEBHOOK_PROMPT,
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
    tiers: { [TASK.design]: "leader", [TASK.peerSchema]: "coder", [TASK.peerFailover]: "coder" },
  };
}
