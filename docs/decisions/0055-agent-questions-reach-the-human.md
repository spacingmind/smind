# 0055: Agent questions reach the human — one question model, one card, one inbox item, for Claude, Codex and ACP

## Status

**Proposed — 2026-11-08.** Extends [ADR-0019](0019-provider-native-permission-modes.md) (provider-native permissions; smind never decides a request is safe) and
[ADR-0017](0017-mcp-server.md) (an orchestrating agent can never answer). Uses the inbox of [ADR-0045](0045-validator-and-inbox.md) and the policy engine of
[ADR-0047](0047-steward-seat.md) without changing either. Every choice under [Decisions for the Human](#decisions-for-the-human) carries a recommendation; a veto changes only
the PR named next to it. Spec, acceptance criteria and PR split: `docs/plans/active/agent-questions.md`. No code is written before the Human answers.

Inputs: ADR-0008 (structured run events), ADR-0019, ADR-0017, ADR-0021 (queue), ADR-0037 (learning probe; secretdeny paths), ADR-0040 (push kinds), ADR-0041 (seats), ADR-0045 (inbox),
ADR-0047 (policy engine, the 5-minute timeout), `docs/plans/completed/ui-redesign-parity.md` (the "question-form variant" note that parked this for want of a verified answer
shape).

## Context

An agent that needs a decision from the person asks through a tool: Claude Code's `AskUserQuestion`, Codex's `item/tool/requestUserInput`, an MCP server's elicitation, and
ACP's `elicitation/create`. smind runs these agents unattended most of the time, so a question nobody can answer is a stalled run. Today **no question reaches a human as a
question** (evidence from the code):

| Provider | What the agent sends | What smind does today | Evidence |
|---|---|---|---|
| Claude native | a `can_use_tool` request with `ToolName: "AskUserQuestion"`, `Input.questions[]` (`question`, `header`, `multiSelect`, `options[]`) | the generic path: the card reads **"run AskUserQuestion"** with Allow / Deny and no question text. **Allow returns `req.Input` unchanged** (`claudeDeciderAdapter.Decide`, `internal/taskrunner/permission.go`), so the tool runs with **no `answers`**; the model sees an empty answer. Deny returns the fixed "denied by human reviewer". | `permission.go` (`claudeDeciderAdapter`), SDK `engine.go` (`updatedInput`, zero name-based filtering) |
| Codex native | `item/tool/requestUserInput` (experimental) with `questions[]` (`id`, `header`, `question`, `isOther`, `isSecret`, `options[]`), `isBlocking` | **never arrives**: `Initialize` sends only `clientInfo`, no `capabilities.experimentalApi`, and `internal/codex/client.go` registers handlers for the two approval requests only. If it did arrive, `rpc.go` answers any unregistered method with a JSON-RPC internal error. MCP elicitation (`mcpServer/elicitation/request`) is the same. | `internal/codex/client.go` (`Initialize`, `handleRequest`), `rpc.go:303` |
| ACP (GLM, OpenCode, Cursor, …) | `elicitation/create` (form or url mode), only if the client declared the `elicitation` capability | **never arrives**: smind declares no elicitation capability, so a compliant agent does not send it. `internal/acp/rpc.go` would answer `-32601`. | `internal/acp/client.go`, `rpc.go:337` |

(The brief called this "auto-allow". Precisely: a **human** pressing Allow on the bare card auto-answers with nothing, and nothing in the product tells the human a question
was asked. Under a provider mode that auto-approves, Claude does not even call `can_use_tool`. Either way nobody answers.)

The request side of the existing flow is solid and is reused: `runPermissionDecider.decide` (`internal/runs/registry.go`) registers a pending channel keyed by request id,
publishes `permission.pending`, records `permission_request`/`permission_resolved`, and resolves on a human answer (`run.respondPermission` → `RespondPermission`), on the
**5-minute timeout** (deny, resolution `timeout`), on provider cancellation or on run stop. System runs and seats resolve to a reject immediately; a remote run (ADR-0048)
never auto-accepts. `smind task approve|deny` and the inbox `permission` item (`internal/inbox`, `KindPermission`) sit on top. The MCP server exposes no tool that answers
(`cmd/smind/mcp_instructions.go`).

Why a permission card cannot carry a question: its answer is one option id of a list the provider sent (`PermissionOption{ID, Label, Kind}`), with no free text, no multi
choice and no per-question structure; the `Kind` vocabulary (allow/reject once/always) is wrong for an answer.

## Decision

**Add a first-class *question request*: a daemon-side model, two additive event types, one RPC, an inbox kind and a card on web, mobile and the CLI. Only a human answers.
Secrets are refused, not collected. An unattended run times out into a decline with a note, never into an invented answer.**

### 1. The question model (provider-agnostic)

```
QuestionRequest {
  requestId, runId, source: "claude.ask_user_question" | "codex.request_user_input" | "mcp.elicitation" | "acp.elicitation",
  blocking: bool,                    // Codex isBlocking; true for the others
  questions: Question[]              // 1..4
}
Question {
  id,                                // provider id, or "q<n>" when the provider has none (Claude keys answers by question text)
  header,                            // <= 32 chars, plain text
  text,                              // <= 1,000 chars, plain text
  kind: "single" | "multi" | "text",
  options: Option[],                 // <= 8; empty for "text"
  allowOther: bool,                  // single/multi: also accept free text (Claude: always; Codex isOther)
  required: true,
  valueHint: "string" | "number" | "integer" | "boolean"   // ACP forms; card validates before submit
}
Option { id, label (<= 80), description (<= 200), preview?: not rendered in v1 }
QuestionAnswer {
  action: "answer" | "decline",
  answers: [{questionId, optionIds: string[], text?: string}],   // text <= 2,000, only if allowOther or kind==text
  note?: string                                                   // decline reason, <= 500, goes back to the agent
}
```

All caps are enforced in the daemon when the request is built (truncate with an ellipsis, drop options past the cap and record `truncated: true` so the card says so). A
request over the caps in count (more than 4 questions, a form with unsupported property types) is **declined whole** with a note, not partially answered.

### 2. Wire and storage (additive)

- Events (ADR-0008 style, new types, unknown to old clients and ignored by them per ADR-0010): `question_request` {requestId, source, blocking, questions, truncated} and
  `question_resolved` {requestId, resolution: `human` | `declined` | `timeout` | `refused_secret` | `refused_unsupported` | `provider_cancellation` | `run_ended`, answers
  summary}. The stored `question_resolved` keeps the **chosen option ids and free text** for a non-secret question (the history is the audit; the model also has the text in
  its own transcript) and never for a refused one.
- RPC `run.answerQuestion {runId, requestId, action, answers[], note?}` and read-only `run.pendingQuestions` (or the question requests folded into the existing pending
  read, `internal/runs/pending.go`). Topics `question.pending` / `question.resolved` mirror `permission.pending|resolved`. All are in the human-only set: **no MCP tool and no
  orchestrator path answers** (a test walks the handler map and the MCP tool list, as `pluginMutating` does).
- The parked request reuses the permission machinery: the same pending map keyed by request id, the same ctx/run-stop/abandon cases, the same notifier; only the resolution
  payload differs. A second answer to the same id is "already resolved", like a permission.
- No SQL migration: events live in the existing event log. The "always" rule of §8 adds one table, scoped to that PR.

### 3. Who may answer

Only a human connection (web, mobile, desktop, CLI over the daemon socket, relay with a paired device). An orchestrating agent never answers (ADR-0017): `task_wait`
returns `waiting_permission` today; it returns **`waiting_question`** (new, additive state; clients that do not know it treat it as "not done, a human is needed", the same
as `waiting_permission`) with the question text so the orchestrator can tell the human. A seat (`purpose != ""`, the Steward, the validator) never parks a question: the
daemon declines immediately with note "no human is attached to this seat", exactly like the permission reject for system runs.

### 4. Per-provider mapping (what each CLI expects back)

**Claude native** (`internal/taskrunner/permission.go`, new branch before the Bash/generic path, keyed on `ToolName == "AskUserQuestion"`):
- Request → `QuestionRequest`: each `questions[i]` → a `Question` (`multiSelect` → `multi`, else `single`; `allowOther: true`; the tool has no id so the id is the index and the
  answer key is the **question text**).
- Answer → `allow` with `updatedInput = original input + answers`, where `answers` maps **question text → chosen label** (several labels of a multi-select joined with
  `", "`; free text is sent as typed). This shape comes from the SDK's documented user-input handling (the Agent SDK's "handle approvals and user input" guide; the CLI's
  own dialog resolves the same way), **not from the vendored Go SDK or the reference clones in this repo, which carry no description of it**. The earlier plan parked this for
  exactly that reason, and a wrong `updatedInput` silently runs the tool with empty answers. **So PR1 begins with a live Haiku probe against the real CLI in an isolated
  home (AC1) and the shape is pinned by a recorded fixture, not by memory.**
- Decline → `deny` with message `"The user declined to answer: <note>"` (the model reads it). If `system/init` lists the `ask_user_question_unanswered_v1` capability, send
  the per-question `unanswered` annotation (`skipped` for a decline, `dismissed` for a timeout) instead of a deny, which is the documented clean path; otherwise deny.
- Modes where Claude never calls `can_use_tool` for the tool (bypass/auto, `dontAsk`) are unchanged and stated as a limit (AC1 records what the CLI does for
  `AskUserQuestion` in each mode; if it can run without asking, PR1 adds `AskUserQuestion` to the run's disallowed tools for an unattended profile, see §5).

**Codex native** (`internal/codex`): opt-in. PR2 adds `capabilities: {experimentalApi: true}` to `initialize` **only when the profile sets `questions: ask`** (§5), registers
`item/tool/requestUserInput` and `mcpServer/elicitation/request` handlers and maps `questions[]` (`isOther` → `allowOther`; `options` absent → `text`; `isSecret` → §6).
Response `{answers: {<questionId>: {answers: [string]}}}` (option labels, or the typed text). Decline → an empty `answers` for each question; the exact "declined" encoding for
a blocking request is **verified against the real app-server before merge** (AC9); the fallback is a JSON-RPC error with the note. `isBlocking: false` (the model continues
without waiting) → the card is shown but the request is answered immediately with empty answers and recorded `non_blocking`; a human sees what was asked, nothing hangs. MCP
elicitation (form mode only, §below) maps through the ACP form mapping; url mode is declined.

**ACP** (`internal/acp`): PR2 declares `capabilities.elicitation.form` (never `url`) at `initialize` for providers whose profile sets `questions: ask`, and handles
`elicitation/create`. Form schema → questions: string with `enum`/`oneOf` → `single`; array of enum → `multi`; string → `text` (`minLength`/`maxLength`/`pattern`/`format`
validated on the card and again in the daemon); integer/number → `text` with `valueHint`; boolean → `single` {Yes, No}. Any other property type or a nested schema → decline the
whole request (`refused_unsupported`). Answer → `{action: "accept", content: {<prop>: value}}`; decline → `{action: "decline"}`; the 5-minute timeout and a run stop →
`{action: "cancel"}` (the protocol's own three-way distinction is kept). `elicitation/complete` is ignored. **url mode is never advertised**: a link an agent hands the
human is an injection surface (§7), and smind has no flow to complete one.

### 5. Unattended runs: the policy

A question is the one request an agent can use to stall a run indefinitely, so the policy is explicit and the same everywhere:

1. **Parked for a human** when the run is attended, with the existing **5-minute** window (unchanged `defaultPermissionTimeout`; the question uses the same timer). On
   timeout the resolution is `timeout` and the agent receives a **decline with the note** `"No human answered within 5 minutes; proceed with your best judgement or stop and
   report what you need."` — never an answer, never an allow.
2. **Per-profile setting** `questions: ask | decline | disabled` (profile field, additive, default **`ask`**):
   - `ask`: as above.
   - `decline`: never park; decline at once with the note (for unattended Leads and automations, ADR-0044, which default to `decline`).
   - `disabled`: do not advertise the capability (Codex, ACP) or add `AskUserQuestion` to the run's disallowed tools (Claude), so the agent knows it cannot ask.
3. **Composition with the permission timeout.** One timer per request, never stacked: a question and a permission that are pending together each own their 5 minutes. A run
   with a pending question is still a running run, so the stall detector (`internal/stall`, which looks at running/queued activity) does not flag it; the task tree
   (`internal/runs/tree_view.go`) reports `waiting_question` where it reports `waiting_permission` today.
4. **Escalation, not repetition.** After a `timeout` the inbox item stays visible as resolved-by-timeout with a "ask again" affordance that sends the original question
   text back to the chat as a human prompt (ordinary `task send`, no new mechanism); an agent that re-asks the same question text within the run is declined at once
   (`repeat`, cap 3 questions per run per 10 minutes, configurable) so a loop cannot spam a human.

### 6. Secrets and credentials

smind does not collect secrets through an agent's question. A question is **refused** (`refused_secret`) when any of: Codex `isSecret: true`; an ACP property with
`format: "password"` or a name/title matching a small **new** vocabulary list kept beside the question builder (token, password, secret, api key, credential, private key, passphrase; `internal/secretdeny` is path-based and has no word list to reuse); a
Claude/Codex/ACP question whose header or text matches the same list. The agent receives a decline with the note `"Secrets cannot be provided through a question.
Ask the human to set it in the environment or the provider's own login."`, the human sees an inbox line "an agent asked for a secret and was declined" (so they can set it
properly), and **no input is rendered and nothing is stored**. The vocabulary check is a false-positive risk (a question about "token budget"); the cost is a decline with a
clear note, which the agent can rephrase, and the alternative is a credential typed into a card that lives in event history. A secure entry path into the secrets store
is a separate ADR (D3).

### 7. Security review (prompt-injected question text)

The question text, header, option labels and descriptions are **untrusted agent output**, possibly steered by a prompt injection in a file the agent read.
- Render as **plain text** only: no markdown, no HTML, no links, no autolinking, no images; a URL-looking string is shown as text and is not tappable. The mobile card uses
  `Text`, the web card a text node with `white-space: pre-wrap` (the heredoc-reveal card's rule, `permission-detail.tsx`).
- Length and count caps in the daemon (§1), so a card cannot be made to scroll a destructive option out of view or be a wall of text; options past the cap are dropped and
  flagged, not paged.
- **No auto-answer, ever**: no default selection, no "recommended" marker from the agent (a Claude option text of "(Recommended)" is shown as ordinary text, no emphasis),
  no timer-based pick, no "yes to all". The submit button is disabled until every required question has an answer.
- An answer is **human input**: it reaches the agent verbatim and is stored in history; the daemon never interprets it, and never treats free text as a command or a rule.
- A question cannot widen permissions: answering never changes the run's permission mode, never allows a tool, and the only way an answer becomes a standing rule is the
  separate, explicit scoped-rule flow of §8, which is built by the daemon from the *tool request*, not from the answer text.
- Option and question ids from the provider are never used as paths or shell input; they are opaque keys.
- Relay/E2EE: the question and the answer travel inside the existing encrypted payloads (same as permission), no new plaintext surface.

### 8. "Always allow in this project" (UX audit item 2): a scoped standing allow on the permission card

This is a **permission** feature that rides the same inbox, not a question. Today a card's `allow_always` option is the provider's own (ACP) and for Claude/Codex smind
synthesizes only allow-once. The Human asked for "allow this kind of request in this project from now on".

- **Where it lives.** The policy file `$SMIND_HOME/steward/policy.yaml` is human-authored with no RPC that writes it (ADR-0047 §2) and stays so. A card's "always" is a
  different object: a **grant** in a new table `permission_grants` {id, workspaceId, tool, matcher (a structured, ADR-0047-style match: exact tool name, for Bash the
  argv0+subcommand, for file tools the **directory** under the worktree), createdAt, createdBy device, expiresAt (default 30 days), source requestId}. A grant is written
  only by `run.answerPermission` with `scope: "project"` from a human connection (same human-only rule as §3; a test walks the handler map and the MCP list).
- **Evaluation order.** floor (never overridable) → policy rules → grants → ask. A grant can only turn an `ask` into an `allow`; it never overrides the floor or a policy
  `deny`.
- **Never offered** (the card shows only "allow once") for: any request the floor flags (secret paths, `SMIND_HOME`, outside the worktree), the destructive classes
  (`rm`/`git push --force`/`git reset --hard`/anything the git-rule table marks always-ask, `GitRule != nil`, already stripped of standing options today by
  `stripStandingOptions`), network-fetching Bash, `AskUserQuestion` and every other question (an answer is never a standing decision), and any request whose command the
  splitter could not parse (`Doubt` set).
- **Audit and revoke.** Every use of a grant records `permission_resolved` with resolution `grant` and the grant id; the Settings page lists grants with a revoke button and
  `smind task grants ls|revoke` (CLI). Revoking takes effect for the next request.
- **Provider mapping.** Claude: the card choice maps to an `allow`; the grant is smind's, not Claude's `updatedPermissions` (which would live in the CLI's settings, outside
  smind's audit). Codex and ACP likewise answer a plain accept.

### 9. Surfaces

- **Web** (`web/packages/ui/src/components/permission/`): a `QuestionCard` next to `PermissionCard`: header chip, per-question block (radio group / checkbox group /
  textarea, an "Other" field when `allowOther`), a counter for caps, Submit (disabled until valid) and Decline with an optional note. A secret refusal renders as a closed,
  informational row. Light and dark frames, every state (single, multi, text, truncated, refused, resolved-by-timeout), per the screenshot rule.
- **Mobile** (`mobile/`): the same model as a bottom sheet on the inbox item and the run screen, native pickers per `@expo/ui` (ADR-0036); push kind `question` (ADR-0040)
  with no question text in the push payload (title only: "An agent has a question"), so nothing sensitive is on the lock screen.
- **CLI**: `smind task answer <runId> [requestId]` prints the question(s) and reads the answer interactively on a TTY; non-interactive
  `smind task answer <runId> <requestId> --answer <qid>=<optionLabel|text> ... [--decline --note "..."]`. `smind task permissions` lists pending questions alongside
  permissions (marked `question`) and the existing "-> approve" hint line prints `-> answer: smind task answer <runId> <requestId>`.
- **Inbox** (`internal/inbox`): new `KindQuestion` item: needs-attention state while pending, resolved (with the resolution) afterwards, superseded by the next question of
  the same run; sorted with permissions (both are "a run is waiting on you"). Additive in `inbox.list`.
- **MCP/orchestrators**: `task_wait` / `task_status` report `waiting_question` plus the question text; `task_permissions` stays permissions-only; no answering tool.

### 10. Failure modes

| Failure | Behaviour |
|---|---|
| nobody answers | decline with note after 5 minutes (§5), resolution `timeout` |
| run stopped / daemon restart while parked | `abandon`, `question_resolved{run_ended}`; after restart the run is `interrupted`, the question is not re-asked (the agent re-asks on `continue` if it still needs to) |
| client answers twice / for a resolved id | "already resolved" error (same as permission) |
| answer fails validation (unknown option id, over cap, wrong `valueHint`) | RPC error, request stays parked; the card shows the field error |
| provider cancels the request itself | resolution `provider_cancellation`, card closes |
| malformed question payload (no options for `single`, non-JSON) | decline with `refused_unsupported`, logged once |
| two questions pending in one run | allowed (Codex can parallelise); each is its own card and inbox item |
| old client that does not know `question_request` | the run shows as waiting (`task_wait` `waiting_question` → old CLIs print the unknown state), and the 5-minute timeout declines; the daemon version gate prevents a *silent* hang |

## Options considered

1. **Reuse the permission card with one option per answer.** Rejected: no free text, no multi-select, no per-question structure, wrong semantics ("allow once").
2. **Let the orchestrating Lead answer.** Rejected: ADR-0017, and an injected agent could then steer its own supervisor's answer.
3. **Auto-pick the first/recommended option on timeout.** Rejected: an invented answer is an unreviewed decision; decline-with-note keeps the agent honest.
4. **Route secrets to a secure input.** Deferred (D3): needs the secrets-store entry flow; refusing is the safe v1.
5. **Disable the tools everywhere and keep today's behaviour.** Rejected as the only answer, kept as the `disabled` per-profile setting.

## Consequences

- A Claude `AskUserQuestion` is finally a question card whose answer the model receives; the "empty answers" incident class goes away.
- Codex and ACP questions work only when a profile opts in (`questions: ask`), so no running setup changes behaviour on upgrade.
- `waiting_question` is a new state for orchestrators; the doc string in `mcp_instructions.go` and the CLI hint change.
- The grant table is the first human-written standing decision stored in the DB rather than the policy file; the evaluation order and the never-offered list keep it
  strictly narrower than the policy file's capabilities.
- Honest limit: the Claude answer shape is pinned by a recorded probe of CLI 2.1.x, so a CLI release that changes it breaks the answer silently; a contract test with the
  recorded fixture and the probe script are kept for a re-check on CLI upgrade.

## What we are NOT doing

- Collecting secrets, rendering links or markdown, url-mode elicitation, auto-answering, an agent answering an agent, writing `policy.yaml` from the UI, using Claude's own
  `updatedPermissions` store, or a question state machine across daemon restarts.

## Decisions for the Human

Each has a recommendation; accept all in one message or veto by number. "PR" = the implementation PR it changes (split in the spec).

| # | Decision | Recommendation | PR |
|---|---|---|---|
| D1 | A **separate question request** (own events, RPC, inbox kind, card) rather than extending the permission request | **Separate** (§1, §2; options 1 rejected) | PR1 |
| D2 | **Unattended policy**: per-profile `questions: ask\|decline\|disabled`, default `ask`; automations and orchestrated Leads default `decline`; 5-minute timeout then **decline with a note, never an answer** | **Accept as written** (§5) | PR1 |
| D3 | **Secrets**: refuse and tell the human (no input, nothing stored); a secure entry flow is a later ADR | **Refuse in v1** (§6) | PR1 |
| D4 | **Codex and ACP questions are opt-in per profile** (capability advertised only with `questions: ask`) | **Opt-in**, so no running setup changes on upgrade | PR2 |
| D5 | **Orchestrator visibility**: a new `waiting_question` state in `task_wait/task_status` (additive) instead of reusing `waiting_permission` | **New state** (§3) | PR1 |
| D6 | **"Always allow in this project"**: daemon-owned `permission_grants` (30-day default expiry, revocable, never for floor/destructive/network/unparsed/questions) rather than editing `policy.yaml` or Claude's `updatedPermissions` | **Grants table** (§8) | PR4 |
| D7 | **Mobile push** for a question carries the title only, no question text | **Title only** (§9) | PR3 |
| D8 | **Repeat guard**: decline a re-asked identical question; cap 3 per run per 10 minutes | **Accept** (§5.4) | PR1 |
| D9 | **Spend one Haiku probe** (a few cents, isolated home, cwd in `~/.smind/dogfood/`) to pin the Claude `updatedInput` shape before any code, and one Codex app-server probe for the decline encoding if a Codex login is available | **Yes to the Claude probe now; Codex probe only if the login exists** | PR1/PR2 |
