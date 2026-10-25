# Tree budget: opt-in spend cap on a task tree (ADR-0041 A5 / T1, open question 9)

**Status 2026-10-25 (plans hygiene): PARTIAL.** Daemon (#602) and web (#604) shipped; AC1-AC11 confirmed by tests. Open: AC12 screenshots (light + dark, 1280 and 390 px, not recorded); AC13 only run with seeded cost rows, never with a real Haiku run crossing a tiny budget (needs a live-model run, a Human go-ahead); the optional mobile read-only budget line is not built.

Status: active (2026-10-24).

Size: L (7) — scope 2 (daemon + web, mobile read-only if cheap), PRs 2, risk 1 (admission path, migration; no data loss), decisions 1 (setter reading, below), verification 1 (UI screenshots + one scripted live smoke).

A human may put a USD cap on a task tree. When the tree's *reported* cost has reached it, agents can no longer launch new runs or tasks in that tree; humans are never refused and running runs are never killed.
Binding text: `docs/decisions/0041-in-smind-orchestration.md` A5 and open question 9. No default number (ADR-0027 D4); null = no budget.

## Rules

1. `tasks.budget_usd REAL NULL`, meaningful on the **root** only (root = top of the `parent_task_id` chain; `treeRootOf` in `internal/runs/tree_guard.go`). A value on a non-root row is ignored and `task.setBudget` on a non-root is refused (`not a root task`).
2. **Known cost** of a tree = sum of `run_usage.cost_usd` over every `purpose='user'` run of the root and all descendants, archived included (the same rows `task.tree`'s `usage.subtree.costUsd` rolls up). Running runs count only once their usage row exists (no estimation of in-flight cost).
3. **Partial**: `budgetPartial` is true when at least one finished run in the tree has no `cost_usd` (no usage row, or NULL). Such runs are **never estimated** (ADR-0020) and never counted; the cap bounds *reported* cost only. The text says so everywhere (error, UI, CLI).
4. **Admission** refuses only `source=agent` launches: `Registry.Start`/`StartWhenBusy` with origin agent (the queue-delivered agent sends too), and `task.create` when the caller is a seat (run identity present) with a parent in a budgeted tree. Condition: `budget != nil && known >= budget` (**at the cap refuses**; strictly under allows). Human origin, `orchestrator`, `child_event` sources are never refused (a child-event wake of a Lead is not a spend decision by an agent; documented).
5. Refusal is a typed `*BudgetExceededError{RootTaskID, SpentUSD, BudgetUSD, Partial}` mapped to wire code `budget_exceeded`; its message is model-readable: `task tree #12 reached its budget ($5.02 of $5.00 reported cost; N runs reported no cost and are not counted): no new agent-launched runs or tasks are allowed in this tree. Ask the human to raise it (smind task budget 12 <usd>, or the budget control in the tree header); do not retry.`
6. **One parent notification per crossing**: when a refusal happens and the root's `budget_notified_at` flag (in-memory per root in the notifier, reset when the budget is raised/cleared or the known cost falls below it) is unset, enqueue one child-event style message to the root's chat via the existing parent notifier (ADR-0041 B3, debounced the same way). Further refusals while over cap do not notify again. Raising the cap above spend re-arms.
7. Running runs are **not** killed (v1).
8. **Who sets it**: a HUMAN only. `task.setBudget {taskId, budgetUsd|null}` calls `refuseRunScoped` for run-scoped/agent callers like the other human-only RPCs. Validation: root task only; positive, finite, rounded to 2 decimals; `null` clears.
   **Decision flagged for the Human (conservative reading of the ADR):** the Human's note mentioned "MCP for the Supervisor seat". An agent-reachable setter lets an agent defeat its own cap, and the ADR says an agent cannot set or raise it. So MCP exposes the budget **read-only** (`budgetUsd, budgetSpentUsd, budgetPartial, overBudget, remaining` on `task_tree`/`task_get`) and has **no setter**. If the Human wants an agent-callable *lower-only* setter, that is a one-line follow-up (a `task_budget_lower` tool calling a RPC that rejects `new >= current`); not built.
9. Wire (additive): `task.setBudget`; `budgetUsd` (number|null), `budgetSpentUsd` (number|null), `budgetPartial` (bool), `budgetUnknownRuns` (int), `overBudget` (bool) on the **root** node of `task.tree` and on the task payload (`task.get`/`task.list` rows, root tasks only; omitted/zero elsewhere). `task.create` accepts `budgetUsd` (human only). Error code `budget_exceeded` on `run.start`, `task.prompt`, `chat.queue*`, `task.create`.

## Cost of the check

Agent admissions only, and only when the root has a budget: one extra read of the root task (folded into the root walk's last `GetTask`, no additional query when the walk already loaded it) + one aggregate query (recursive CTE over `tasks`, join `run_usage`). Human admissions: zero added queries. Count-asserting test pins both.

## Surfaces

- CLI: `smind task new <ws> ... --budget <usd>`; `smind task budget <taskId>` prints `budget $5.00 · spent $4.20+ · partial: 3 runs reported no cost · remaining $0.80`; `smind task budget <taskId> <usd>`; `smind task budget <taskId> --clear`. Run-scoped callers cannot reach it (RPC refuses).
- MCP: read-only fields in `task_tree` output. No setter tool.
- Web (PR 2): root task header "Budget" chip → popover: `$spent / $cap` + progress meter, `partial` badge ("$4.20+ · partial: 3 runs reported no cost"), set/clear, validation (positive number, ≤2 decimals, inline error). States none/under/near (>=80%)/over ("Over budget", quiet but clear). Existing header/popover primitives, ADR-0032 shared stores, light + dark.
- Mobile: read-only budget line in the tree header of #594 if cheap; otherwise left and stated.

## Acceptance criteria

AC1 migration adds `budget_usd` nullable; existing rows null; no default.
AC2 agent admission is refused at and over the cap, allowed strictly under, and never refused for human origin.
AC3 a child-of-a-child resolves to the root's budget.
AC4 unknown-cost runs make `partial` true and are not counted; the check uses the known sum.
AC5 raising or clearing the budget re-allows admission immediately.
AC6 refusal carries `budget_exceeded` and the typed sentence (spent, cap, how to raise, partial note).
AC7 exactly one parent notification per crossing, not per refusal; re-armed by raise/clear.
AC8 `task.setBudget` refuses run-scoped callers and non-root tasks and bad numbers; MCP has no setter.
AC9 CLI flags/output as above.
AC10 `task.tree` root node and task payload carry the additive fields.
AC11 admission adds no extra store query for human callers and at most the documented queries for agents; no query when no budget.
AC12 web control states, validation, set/clear; screenshots light+dark at 1280 and 390.
AC13 live smoke on an isolated daemon (own SMIND_HOME and port) with a tiny budget crossed by one Haiku run: the next agent-launched run is refused with `budget_exceeded` and the parent is notified once.

## Named test scenarios

Daemon (`internal/runs`, `internal/store`, `internal/wsapi`, `cmd/smind`):
- `TestMigrate_TasksBudgetUSDNullable` (AC1)
- `TestTreeKnownCost_SumsSubtreeIncludingArchived`, `TestTreeKnownCost_UnknownRunsPartialNotCounted` (AC4)
- `TestBudgetAdmission_AgentRefusedAtAndOverCap`, `..._AgentAllowedUnderCap`, `..._ExactlyAtCapBoundary`, `..._HumanNeverRefused`, `..._ChildOfChildUsesRootBudget` (AC2, AC3)
- `TestBudgetAdmission_RaiseAndClearReallow` (AC5)
- `TestBudgetExceededError_TypedSentence`, `TestWSAPI_BudgetExceededCode` (AC6)
- `TestBudgetNotify_OnePerCrossing_FakeClock`, `..._RearmedAfterRaise` (AC7)
- `TestSetBudget_RefusesRunScoped`, `..._NonRootRefused`, `..._ValidatesAmount`, `TestMCP_NoBudgetSetter_ReadOnlyFields` (AC8)
- `TestTaskBudgetCLI_NewFlag`, `TestTaskBudgetCLI_ShowSetClear` (AC9)
- `TestTaskTree_RootBudgetFields` (AC10)
- `TestBudgetAdmission_QueryCount` (AC11)
Web: `task-budget-control.test.tsx` states none/under/near/over/partial, validation, set, clear (AC12).
Mutation checks (revert, see red): `>=` to `>` (boundary), drop the `source==agent` condition (human refused), skip root resolution (child-of-child), count unknown as 0 without partial, notify per refusal.

## PRs

PR 1 daemon (column, store query, admission, RPC, CLI, MCP read-only fields, task.tree fields). PR 2 web (+ mobile line if cheap), non-stacked from `origin/develop`; it depends on PR 1's wire fields (stated in its body; seeded types only until PR 1 merges). Coordination: item O (inferred rate-limit reset) also touches `cmd/smind` and web types; the second PR opened rebases onto the first.

## Decisions

- Setter conservative reading (rule 8), flagged to the Human.
- Boundary is `known >= budget` refuses (a budget is "reached").
- Source `child_event`/`orchestrator` never refused.
- No kill, no estimate, no default number.

## Progress

- PR 1 (daemon): store column + `TreeKnownCost`, admission in `Start`/`StartWhenBusy`/queue delivery, one notification per crossing, `task.setBudget`, `budget_exceeded`, CLI, MCP read-only. Built by a peer-sonnet Peer (task #91, candidate d8455b10), reviewed and ACCEPTED by the Lead.
- PR 1 merged as #602 (daemon half).
- PR 2 (web) merged as #604 (`task-budget-control.tsx`, `lib/task-budget.ts`, `hooks/use-set-task-budget.ts`). The mobile read-only budget line ("if cheap") was not built.

## Validation

Hygiene pass 2026-10-25: the named daemon tests (`internal/store/task_budget_test.go`, `internal/runs/budget_test.go`, `internal/wsapi/task_budget_test.go`,
`cmd/smind/task_budget_test.go`) and the web `components/task-budget-control.test.tsx` exist on `origin/develop`. AC1-AC11 confirmed by those tests (#602); AC12 test
coverage is in #604, but its screenshots were never recorded here.

PR 1, confirmed (AC1-AC11): named tests above pass (`go test` on runs, store, wsapi, tasktree, cmd/smind; `-race` on runs, store, wsapi). Mutations (reverted): `>=` to `>` fails `TestBudgetAdmission_ExactlyAtCapBoundary`; dropping the agent-origin condition fails `TestBudgetAdmission_HumanNeverRefused` and `..._QueuedAgentRefusedAtEnqueueAndDelivery`.
AC13 (isolated daemon, own SMIND_HOME, port 48771, transcript `/tmp/tree-budget-shots/daemon-smoke.txt`): root budget $0.05 set by CLI; a seeded $0.06 run (cost seeded in sqlite, NO model run) plus a seeded run with no cost; an agent `task_send` over `smind mcp serve` with fromTaskId/fromChatId was refused twice with the typed sentence; exactly one `child_event` row was queued for the root chat; raise and clear via CLI work. Not verified: a real model run crossing the cap (no model call made).
AC12 (web): shipped in #604; unit tests present; light + dark screenshots at 1280 and 390 px not recorded (open).
Known limits: the check runs before `reg.mu`, so launches racing the same instant can both pass (cost is only known when a run finishes, so v1 accepts this); `child_event` wake runs are never refused, so a woken Lead can still spend after the cap (the cap bounds NEW agent launches, per the ADR).
