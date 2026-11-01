import type { InboxItem } from "@/lib/inbox-types";
import type {
  Account,
  AgentProfile,
  Chat,
  PermissionRequestEventParams,
  RunLogEvent,
  RunLogsResult,
  RunSummary,
  Space,
  Task,
  UsageGroupBy,
  UsageSummaryRow,
  Workspace,
} from "@/lib/types";

/**
 * One step of a script (ADR-0051 section 2). `at` is milliseconds since the script (or, inside an ask's branch, since
 * the visitor answered) started.
 */
export type Step =
  /** A run event: chunk, thinking, tool_call, usage, done, ... exactly what run.attach streams. */
  | { at: number; event: RunLogEvent }
  /** A daemon notification (ADR-0005), so lists and the sidebar update live. */
  | { at: number; notify: { topic: string; payload: unknown } }
  /** Pauses the script on a permission request until run.respondPermission answers it; then plays the matching branch. */
  | { at: number; ask: PermissionRequestEventParams; allow: Step[]; deny: Step[] };

export interface Script {
  steps: Step[];
}

/** Canned terminal output: base64 chunks with the delay (ms) before each one. */
export interface TerminalReplay {
  frames: { afterMs: number; text: string }[];
}

/** The usage fixtures the usage views read (shapes are those of the daemon's results; typed loosely on purpose). */
export interface UsageFixtures {
  /** usage.summary rows by the wire's groupBy (the key is the task/workspace id, the provider id or the model). */
  summary: Record<UsageGroupBy, UsageSummaryRow[]>;
  providerUsage: unknown;
  accountUsage: unknown;
}

export interface DemoScenario {
  version: 1;
  /** Epoch ms every timestamp is written relative to: Date.now() in the browser, fixed in tests. */
  now: number;
  daemon: Record<string, unknown>;
  workspaces: Workspace[];
  spaces: Space[];
  tasks: Task[];
  chats: Chat[];
  /** Run summaries (run.list) of the finished runs; the live run is created when the session starts. */
  runs: RunSummary[];
  /** run.logs of the finished runs, by run id. */
  runLogs: Record<string, RunLogsResult>;
  /** The run that is already in progress on first paint (the landing task). */
  live: { taskId: number; chatId: number; runId: string; provider: string; prompt: string; script: Script };
  /** Builds the canned reply script for a prompt the visitor sends (n = how many prompts were sent before). */
  reply(prompt: string, n: number): Script;
  /** task.diff text by task id; `liveTaskExtra` is appended once the live run's permission is allowed. */
  diffs: Record<number, string>;
  liveTaskExtraDiff: string;
  terminals: Record<number, TerminalReplay>;
  inbox: InboxItem[];
  usage: UsageFixtures;
  /** provider.list's result: the providers the composer's model picker shows. */
  providers: { providers: unknown[] };
  /** account.list and profile.list: what Settings shows as already set up. */
  accounts: Account[];
  profiles: AgentProfile[];
  /** Text files of each task's worktree (file.list / file.read), by task id then relative path. */
  files: Record<number, Record<string, string>>;
}
