import type { PermissionRequestEventParams, RunLogEvent, RunUsage } from "@/lib/types";

import type { Script, Step } from "../types";

// Synthetic agent behaviour of the demo tour (ADR-0051): run events exactly as run.attach streams them.

const read = (id: string, path: string, result: string): RunLogEvent[] => [
  { type: "tool_call", toolCallId: id, toolName: "Read", title: path, status: "running", input: { file_path: path } },
  { type: "tool_call", toolCallId: id, toolName: "Read", title: path, status: "success", input: { file_path: path }, result },
];

const bash = (id: string, command: string, result: string, status: "success" | "failure" = "success"): RunLogEvent[] => [
  { type: "tool_call", toolCallId: id, toolName: "Bash", title: command, status: "running", input: { command } },
  { type: "tool_call", toolCallId: id, toolName: "Bash", title: command, status, input: { command }, result },
];

const usage = (inputTokens: number, outputTokens: number, costUsd: number, contextUsed: number): RunLogEvent => {
  const u: RunUsage = { inputTokens, outputTokens, cachedInputTokens: Math.round(inputTokens * 4), costUsd, model: "claude-sonnet-5-5", contextUsed, contextSize: 200_000, source: "provider" };
  return { type: "usage", usage: u };
};

/** Spaces the events of one burst `gap` ms apart, starting at `start`. */
function sequence(start: number, gap: number, events: RunLogEvent[]): Step[] {
  return events.map((event, i) => ({ at: start + i * gap, event }));
}

export const PERMISSION_REQUEST: PermissionRequestEventParams = {
  requestId: "demo-req-1",
  summary: "Write internal/ratelimit/limiter.go",
  options: [
    { id: "allow", label: "Allow", kind: "allow_once" },
    { id: "deny", label: "Deny", kind: "reject_once" },
  ],
  detail: {
    kind: "write",
    path: "internal/ratelimit/limiter.go",
    newBytes: 812,
    newLines: 30,
    replacesExisting: false,
    diff: [
      "@@ -0,0 +1,12 @@",
      "+package ratelimit",
      "+",
      "+import (",
      '+\t"sync"',
      '+\t"time"',
      "+)",
      "+",
      "+// Limiter is a per-key token bucket.",
      "+type Limiter struct {",
      "+\tmu      sync.Mutex",
      "+\tbuckets map[string]*bucket",
      "+}",
    ].join("\n"),
  },
};

const ALLOW_BRANCH: Step[] = [
  { at: 400, event: { type: "tool_call", toolCallId: "t-write", toolName: "Write", title: "internal/ratelimit/limiter.go", status: "success", input: { file_path: "internal/ratelimit/limiter.go" }, result: "File created" } },
  ...sequence(1200, 700, [
    { type: "tool_call", toolCallId: "t-edit", toolName: "Edit", title: "cmd/api/router.go", status: "success", input: { file_path: "cmd/api/router.go", old_string: "return middleware.Auth(cfg.Keys)(mux)", new_string: "limited := ratelimit.Middleware(ratelimit.New(cfg.RateLimit))(mux)\n\treturn middleware.Auth(cfg.Keys)(limited)" }, result: "Edited" },
  ]),
  ...sequence(2200, 1400, bash("t-test2", "go test ./...", "?   \tacme/cmd/api\t[no test files]\nok  \tacme/internal/middleware\t0.412s\nok  \tacme/internal/ratelimit\t0.201s\nok  \tacme/internal/queue\t1.108s")),
  { at: 5200, event: { type: "chunk", text: "Done. Requests are now limited to **100 per minute per API key** (burst of 20); anything over that gets a `429` with a `Retry-After` header.\n\n" } },
  { at: 5500, event: { type: "chunk", text: "- new `internal/ratelimit` package (token bucket, one per key)\n- wired in after auth in `cmd/api/router.go`\n- `RATE_LIMIT_RPM` / `RATE_LIMIT_BURST` in `cmd/api/config.go`\n\nAll tests pass. This is a scripted demo — in smind you would now review the diff and commit." } },
  { at: 5900, event: usage(61_200, 7_400, 1.12, 68_000) },
  { at: 6100, event: { type: "done", stopReason: "end_turn" } },
];

const DENY_BRANCH: Step[] = [
  { at: 500, event: { type: "chunk", text: "Understood — I won't write that file. Here is what I would do instead:\n\n" } },
  { at: 900, event: { type: "chunk", text: "1. keep the limiter behind a small interface so you can pick the storage\n2. add it to the router after auth\n3. cover the `429` and `Retry-After` paths with tests\n\nTell me which part to start with. (This is a scripted demo reply.)" } },
  { at: 1500, event: usage(52_800, 3_900, 0.74, 60_000) },
  { at: 1700, event: { type: "done", stopReason: "end_turn" } },
];

/** The run in progress on first paint: tool calls, a usage event, then a permission request the visitor answers. */
export const LIVE_SCRIPT: Script = {
  steps: [
    { at: 500, event: { type: "thinking", text: "I need to see how the HTTP stack is wired first: where auth runs, and what the API key looks like in the request context." } },
    { at: 1400, event: { type: "chunk", text: "I'll start by reading the router and the auth middleware.\n\n" } },
    ...sequence(2000, 900, read("t-read1", "cmd/api/router.go", 'func NewRouter(cfg Config) http.Handler {\n\tmux := http.NewServeMux()\n\tmux.Handle("/v1/", http.StripPrefix("/v1", v1(cfg)))\n\treturn middleware.Auth(cfg.Keys)(mux)\n}')),
    ...sequence(3800, 900, read("t-read2", "internal/middleware/auth.go", "// Auth resolves the API key and stores its id on the request context.\nfunc KeyID(ctx context.Context) string { ... }")),
    { at: 5600, event: { type: "chunk", text: "Auth already puts the key id on the context, so a limiter can key on it. I'll add a token-bucket limiter in `internal/ratelimit` and wire it in after auth. First, a baseline test run:\n\n" } },
    ...sequence(6400, 1300, bash("t-test1", "go test ./internal/middleware/...", "ok  \tacme/internal/middleware\t0.412s")),
    { at: 8200, event: usage(31_400, 3_100, 0.52, 36_000) },
    { at: 8600, event: { type: "chunk", text: "Baseline is green. Now I'll create the limiter — this needs your approval.\n\n" } },
    { at: 9400, ask: PERMISSION_REQUEST, allow: ALLOW_BRANCH, deny: DENY_BRANCH },
  ],
};

/** The reply to any prompt the visitor types: short, honest about being a demo, and it points at installing smind. */
export function cannedReply(prompt: string, n: number): Script {
  const short = prompt.trim().replace(/\s+/g, " ").slice(0, 80);
  const opening = n === 0 ? "Thanks — here's how this would go in a real session." : "Got it. Same note as before:";
  return {
    steps: [
      { at: 300, event: { type: "thinking", text: `The visitor asked: "${short}". This is the scripted demo, so I will answer with a canned reply.` } },
      { at: 1000, event: { type: "chunk", text: `${opening}\n\n` } },
      { at: 1400, event: { type: "chunk", text: "This is a **scripted demo**: no model is running and nothing you type leaves your browser. " } },
      { at: 1800, event: { type: "chunk", text: "Install smind to run this against your own repositories — it starts an agent in an isolated git worktree, streams its tool calls here, and asks before anything risky." } },
      { at: 2300, event: usage(18_200, 1_100, 0.2, 20_000) },
      { at: 2500, event: { type: "done", stopReason: "end_turn" } },
    ],
  };
}

// ---- finished runs (the timelines of the other tasks) -----------------------------------------------------------

export const FLAKY_RUN_EVENTS: RunLogEvent[] = [
  { type: "thinking", text: "Flaky timing tests usually sleep for real time. Let me read the test and the worker's clock handling." },
  { type: "chunk", text: "Let me look at the test and the worker.\n\n" },
  ...read("f-read1", "internal/queue/worker_test.go", 'time.Sleep(50 * time.Millisecond)\nif w.Attempts() != 2 { ... }'),
  ...read("f-read2", "internal/queue/worker.go", "type Clock interface { Now() time.Time; After(d time.Duration) <-chan time.Time }"),
  { type: "chunk", text: "The test sleeps 50 ms and hopes the retry fired, but the worker already takes an injectable `Clock`. I'll drive the fake clock instead.\n\n" },
  { type: "tool_call", toolCallId: "f-edit", toolName: "Edit", title: "internal/queue/worker_test.go", status: "success", input: { file_path: "internal/queue/worker_test.go", old_string: "time.Sleep(50 * time.Millisecond)", new_string: "clock.Advance(w.BackoffFor(1))" }, result: "Edited" },
  ...bash("f-test", "go test -count=20 -run TestRetryBacksOff ./internal/queue", "ok  \tacme/internal/queue\t0.731s"),
  { type: "chunk", text: "Fixed: the test now advances the fake clock by the worker's own backoff, so it no longer depends on wall-clock timing. Twenty consecutive runs pass." },
  usage(41_000, 4_100, 0.64, 44_000),
  { type: "done", stopReason: "end_turn" },
];

export const TOOLCHAIN_RUN_EVENTS: RunLogEvent[] = [
  { type: "chunk", text: "I'll bump the `go` directive and rebuild.\n\n" },
  ...bash("g-build", "go build ./...", "go: downloading go1.24 (linux/amd64)\ngo: download go1.24: toolchain not available", "failure"),
  { type: "chunk", text: "The Go 1.24 toolchain could not be downloaded in this environment, so I can't verify the upgrade." },
  { type: "done", stopReason: "error" },
];

export const DARKMODE_RUN_EVENTS: RunLogEvent[] = [
  ...read("d-read", "src/settings/SettingsPage.tsx", 'export function SettingsPage() {\n  return <Panel title="Settings" />;\n}'),
  { type: "tool_call", toolCallId: "d-edit", toolName: "Edit", title: "src/settings/SettingsPage.tsx", status: "success", input: { file_path: "src/settings/SettingsPage.tsx", old_string: '<Panel title="Settings">', new_string: '<Panel title="Settings" data-theme={theme}>' }, result: "Edited" },
  { type: "chunk", text: "Added a `ThemeToggle` to the settings page, bound to the existing `useTheme` hook." },
  usage(36_000, 3_000, 0.5, 40_000),
  { type: "done", stopReason: "end_turn" },
];

export const PAGINATION_RUN_EVENTS: RunLogEvent[] = [
  { type: "chunk", text: "Plan: add `?page=` to the audit endpoint, a `Pagination` control under the table, and keep the page in the URL. Say go and I'll start." },
  { type: "done", stopReason: "end_turn" },
];

export const GUIDE_RUN_EVENTS: RunLogEvent[] = [
  { type: "chunk", text: "Wrote `docs/migrate-v2.md`: breaking changes, a before/after config example, and a rollback section." },
  usage(22_000, 3_400, 0.38, 25_000),
  { type: "done", stopReason: "end_turn" },
];
