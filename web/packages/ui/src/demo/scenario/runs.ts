import type { PermissionRequestEventParams, RunLogEvent, RunUsage } from "@/lib/types";

import type { Script, Step } from "../types";

// Synthetic agent behaviour of the demo tour (ADR-0051): run events exactly as run.attach streams them. The tasks, prompts and
// answers are the landing page's hero scenarios (spacing-mind-landing-page, app-mock.tsx), so the hero and "Try it live" tell the
// same story; the demo plays them through the real timeline, permission card, diff and terminal.

// ---- tool-call builders: a tool call is one event while running and one when it finishes ---------------------------------

type Status = "success" | "failure";

function call(toolCallId: string, toolName: string, title: string, input: unknown, result?: string, status: Status = "success"): RunLogEvent[] {
  return [
    { type: "tool_call", toolCallId, toolName, title, status: "running", input },
    { type: "tool_call", toolCallId, toolName, title, status, input, ...(result === undefined ? {} : { result }) },
  ];
}
const read = (id: string, path: string, result = "") => call(id, "Read", path, { file_path: path }, result);
const grep = (id: string, pattern: string, path: string, result = "") => call(id, "Grep", `grep -rn "${pattern}" ${path}`, { pattern, path }, result);
const edit = (id: string, path: string, oldS: string, newS: string) => call(id, "Edit", path, { file_path: path, old_string: oldS, new_string: newS }, "Edited");
const write = (id: string, path: string) => call(id, "Write", path, { file_path: path }, "File created");
const bash = (id: string, command: string, result: string, status: Status = "success") => call(id, "Bash", command, { command }, result, status);
const agent = (id: string, toolName: "task_new" | "task_wait", title: string, input: unknown, result: string) => call(id, toolName, title, input, result);

const usage = (inputTokens: number, outputTokens: number, costUsd: number, contextUsed: number): RunLogEvent => {
  const u: RunUsage = { inputTokens, outputTokens, cachedInputTokens: Math.round(inputTokens * 4), costUsd, model: "claude-sonnet-5-5", contextUsed, contextSize: 200_000, source: "provider" };
  return { type: "usage", usage: u };
};
const text = (body: string): RunLogEvent => ({ type: "chunk", text: body });
const done: RunLogEvent = { type: "done", stopReason: "end_turn" };

/** Spaces the events of one burst `gap` ms apart, starting at `start`. */
function sequence(start: number, gap: number, events: RunLogEvent[]): Step[] {
  return events.map((event, i) => ({ at: start + i * gap, event }));
}

// ---- the live task: "Rate-limit the webhook handler", with the permission request -----------------------------------------

export const WEBHOOK_PROMPT = "Add a rate limiter to the webhook handler";

export const PERMISSION_REQUEST: PermissionRequestEventParams = {
  requestId: "demo-req-1",
  summary: "Write internal/webhook/limiter.go",
  options: [
    { id: "allow", label: "Allow once", kind: "allow_once" },
    { id: "allow_always", label: "Allow always", kind: "allow_always" },
    { id: "deny", label: "Deny", kind: "reject_once" },
  ],
  detail: {
    kind: "write",
    path: "internal/webhook/limiter.go",
    newBytes: 1046,
    newLines: 41,
    replacesExisting: false,
    diff: [
      "@@ -0,0 +1,12 @@",
      "+package webhook",
      "+",
      "+import (",
      '+\t"sync"',
      '+\t"time"',
      "+)",
      "+",
      "+// Limiter is a token bucket: rate tokens per second, burst at most.",
      "+type Limiter struct {",
      "+\tmu     sync.Mutex",
      "+\ttokens float64",
      "+}",
    ].join("\n"),
  },
};

const ALLOW_BRANCH: Step[] = [
  ...sequence(400, 600, write("t-write", "internal/webhook/limiter.go")),
  ...sequence(1600, 600, edit("t-wire", "internal/webhook/handler.go", 'mux.Handle("/webhook", h)', 'limiter := ratelimit.New(5, 10)\n\tmux.Handle("/webhook", limiter.Middleware(h))')),
  ...sequence(2800, 1300, bash("t-test", "go test ./internal/webhook/...", "ok  \tcheckout/internal/webhook\t0.288s")),
  { at: 5300, event: text("Added a token-bucket limiter in `limiter.go` and wired it into the handler. Tests pass.") },
  { at: 5600, event: usage(48_200, 5_100, 0.05, 54_000) },
  { at: 5800, event: done },
];

const DENY_BRANCH: Step[] = [
  { at: 500, event: text("Understood — I won't create `limiter.go`. Here is what I would do instead:\n\n") },
  { at: 900, event: text("1. a small token-bucket `Limiter` behind an interface, so you can pick the storage\n2. call it at the top of the handler and answer `429` with `Retry-After`\n3. cover the burst and refill paths with tests\n\nTell me which part to start with. (This is a scripted demo reply.)") },
  { at: 1500, event: usage(41_000, 2_400, 0.03, 46_000) },
  { at: 1700, event: done },
];

/** The run in progress on first paint: it reads the handler, says it needs a new file, then asks. */
export const LIVE_SCRIPT: Script = {
  steps: [
    { at: 400, event: { type: "thinking", text: "The handler needs a per-client limit before it does any work. Let me read it first." } },
    ...sequence(1200, 900, read("t-read", "internal/webhook/handler.go", 'func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {\n\tevt, err := h.decode(r)\n\t...\n}')),
    { at: 3400, event: text("I need to add the limiter file.") },
    { at: 4300, ask: PERMISSION_REQUEST, allow: ALLOW_BRANCH, deny: DENY_BRANCH },
  ],
};

/** The reply to any prompt the visitor types: short, honest about being a demo, and it points at installing smind. */
export function cannedReply(prompt: string, n: number): Script {
  const short = prompt.trim().replace(/\s+/g, " ").slice(0, 80);
  const opening = n === 0 ? "Thanks — here's how this would go in a real session." : "Got it. Same note as before:";
  return {
    steps: [
      { at: 300, event: { type: "thinking", text: `The visitor asked: "${short}". This is the scripted demo, so I will answer with a canned reply.` } },
      { at: 1000, event: text(`${opening}\n\n`) },
      { at: 1400, event: text("This is a **scripted demo**: no model is running and nothing you type leaves your browser. ") },
      { at: 1800, event: text("Install smind to run this against your own repositories — it starts an agent in an isolated git worktree, streams its tool calls here, and asks before anything risky.") },
      { at: 2300, event: usage(18_200, 1_100, 0.2, 20_000) },
      { at: 2500, event: done },
    ],
  };
}

// ---- the finished tasks (each is the landing hero's story for that level) --------------------------------------------------

/** Debug: "Why does TestRetry fail on CI only?" */
export const RETRY_EVENTS: RunLogEvent[] = [
  ...read("r-1", "ci/logs/retry.txt", "--- FAIL: TestRetry (0.00s)\n    retry_test.go:41: attempt order [3 1 2], want [1 2 3]"),
  ...grep("r-2", "rand.Seed", "internal/", "internal/retry/backoff.go:15:\trng := rand.New(rand.NewSource(time.Now().UnixNano()))"),
  ...edit("r-3", "internal/retry/backoff.go", "rng := rand.New(rand.NewSource(time.Now().UnixNano()))", "seed := testSeed()\n\trng := rand.New(rand.NewSource(seed))"),
  ...bash("r-4", "go test -race -count=20 ./internal/retry/...", "ok  \tcheckout/internal/retry\t3.904s"),
  text("The jitter is seeded from the wall clock, so CI's faster runner hits a different backoff order. I pinned the seed in the test; 20 repeated runs now pass."),
  usage(30_000, 2_600, 0.03, 33_000),
  done,
];

/** First task: "Add a unit test for parseDate" */
export const PARSE_DATE_EVENTS: RunLogEvent[] = [
  ...read("p-1", "internal/dates/parse.go", "func parseDate(s string) (time.Time, error) { ... }"),
  ...write("p-2", "internal/dates/parse_test.go"),
  ...bash("p-3", "go test ./internal/dates/...", "ok  \tcheckout/internal/dates\t0.021s"),
  text("Added a table test with 5 cases: ISO, RFC 3339, an empty string, a bad month and a leap day. `go test` is green."),
  usage(22_000, 1_900, 0.02, 24_000),
  done,
];

/** Review: "Review PR #42 for race conditions" (no code changed) */
export const REVIEW_EVENTS: RunLogEvent[] = [
  ...bash("v-1", "gh pr diff 42", "diff --git a/internal/cache/store.go b/internal/cache/store.go\n..."),
  ...read("v-2", "internal/cache/store.go", "func (s *Store) Get(k string) (V, bool) { ... s.hits++ ... }"),
  ...grep("v-3", "sync\\.", "internal/cache/", "internal/cache/store.go:12:\tmu sync.Mutex"),
  text("Three problems, no code changed:\n\n"),
  text(
    "1. `store.go:58` `hits++` runs outside the mutex: a data race under concurrent `Get`.\n2. `store.go:91` `Len()` reads the map without `RLock`.\n3. `pool.go:34` `wg.Add` after the `go` statement can race with `Wait`.\n\nRun the tests with `-race` to confirm 1 and 2.",
  ),
  usage(38_000, 3_200, 0.04, 41_000),
  done,
];

/** Analysis: "Why did p99 latency double since Tuesday?" */
export const ANALYSIS_EVENTS: RunLogEvent[] = [
  ...read("a-1", "metrics/p99-by-route.csv", "route,p99_ms_mon,p99_ms_wed\nPOST /charge,38,240"),
  ...read("a-2", "deploys.log", "Tue 14:02 checkout-api 4.12.0 (db pool 40 -> 10)"),
  ...bash("a-3", "go tool pprof -top cpu.prof", "      flat  flat%   sum%        cum   cum%\n     1.20s 31.4% 31.4%      1.90s 49.7%  database/sql.(*DB).conn"),
  text("Ranked by evidence:\n\n"),
  text(
    "1. **Pool shrunk 40 → 10** in Tuesday's deploy. Pool wait on `POST /charge`: p99 38 ms → 240 ms.\n2. **N+1 query** in the new order summary: about 9% of the increase.\n3. **GC pauses**: unlikely, the heap is flat.\n\nNext: restore the pool size behind a flag and compare p99.",
  ),
  usage(70_000, 6_100, 0.09, 76_000),
  done,
];

/** Onboarding (web-app): "Explain this repo to me" */
export const ONBOARDING_EVENTS: RunLogEvent[] = [
  ...read("o-1", "README.md", "# web-app\n\nThe customer dashboard."),
  ...read("o-2", "package.json", '{ "name": "web-app", "dependencies": { "next": "15" } }'),
  ...bash("o-3", "tree -L 2 src", "src\n├── app\n├── components\n└── lib"),
  text("A Next.js app with three parts:\n\n"),
  text("- `src/app`: the routes and layouts\n- `src/components`: UI, one folder per feature\n- `src/lib`: API clients and helpers\n\nStart with `src/app/page.tsx`, then follow its imports."),
  usage(9_000, 900, 0.01, 11_000),
  done,
];

/** Design (platform): the Lead. It starts two Peers, waits for them and writes the summary. */
export const DESIGN_EVENTS: RunLogEvent[] = [
  { type: "thinking", text: "Two independent questions: how jobs are stored and leased, and what each failure costs. Two Peers, then I merge their notes." },
  ...agent("d-1", "task_new", '"queue schema and leases", parent: #12', { title: "Queue schema and leases", parentTaskId: 12 }, "task 13 started (run 14)"),
  ...agent("d-2", "task_new", '"failover modes", parent: #12', { title: "Failover modes", parentTaskId: 12 }, "task 14 started (run 15)"),
  ...agent("d-3", "task_wait", "run 14, run 15", { runs: [14, 15] }, "both runs done"),
  text("Both Peers are done. Summary (ADR draft):\n\n"),
  text(
    "| Option | Failover | Ops cost |\n| --- | --- | --- |\n| One global queue | Single point of failure | Low |\n| Queue per region | Drain from the replica | Medium |\n| Event log | Replay by offset | High |\n\n",
  ),
  text("**Recommendation:** a queue per region, jobs pinned to their home region, the dead-letter table replicated."),
  usage(120_000, 9_800, 0.31, 130_000),
  done,
];

/** Design Peer 1: "Queue schema and leases" */
export const PEER_SCHEMA_EVENTS: RunLogEvent[] = [
  ...read("s-1", "docs/adr/storage.md", "# Storage\n\nPostgres per region."),
  ...edit("s-2", "docs/adr/job-queue.md", "## Schema\n", "## Schema\n\n`jobs(id, region, state, lease_until, attempts)`\n"),
  text("Drafted the `jobs` table and a 30 s lease with heartbeat renewal."),
  usage(18_000, 1_500, 0.05, 20_000),
  done,
];

/** Design Peer 2: "Failover modes" */
export const PEER_FAILOVER_EVENTS: RunLogEvent[] = [
  ...read("f-1", "docs/adr/regions.md", "# Regions\n\nus-east, eu-west, ap-south."),
  ...edit("f-2", "docs/adr/job-queue.md", "## Failure modes\n", "## Failure modes\n\nRegion loss, replica lag, split brain.\n"),
  text("Three modes: region loss, replica lag, split brain. Region loss is the only one that needs a human."),
  usage(20_000, 1_700, 0.06, 22_000),
  done,
];
