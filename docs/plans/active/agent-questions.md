# Agent questions reach the human (ADR-0055)

Status: **PR1 in review** (decisions D1-D10 accepted as recommended on 2026-11-09; ADR-0055 stays Proposed until the first implementation PRs land).
See [ADR-0055](../../decisions/0055-agent-questions-reach-the-human.md). Size: L (wsapi, inbox, events, UI on three surfaces, a table): each PR below with a
validation contract (ADR-0045).

## Acceptance Criteria

- AC1 **Claude answer shape is pinned by a probe**: a Haiku run against the real CLI in an isolated home answers an `AskUserQuestion` through `can_use_tool`
  with `updatedInput`+`answers`; the recorded request/response pair is a committed fixture; a record of what the CLI does for the tool under each permission mode.
- AC2 A Claude `AskUserQuestion` becomes a `question_request` with the question text, options and descriptions; the generic "run AskUserQuestion" card is gone.
- AC3 A human answer (single, multi, "Other" text) reaches the model in the tool result; a decline reaches it as a deny with the note.
- AC4 Only a human answers: no MCP tool, no orchestrator path, no seat (`purpose != ""`) parks or answers a question; `task_wait` reports `waiting_question`.
- AC5 Unattended: after 5 minutes the agent receives a decline with the note, never an answer; `questions: decline` declines at once; `disabled` removes the capability.
- AC6 Secrets: `isSecret`, `format: password` and vocabulary matches are refused with a note; no input rendered, nothing stored.
- AC7 Untrusted text: plain text only, caps enforced in the daemon, no default selection, no links.
- AC8 Inbox `question` item, web card, mobile sheet and `smind task answer` work and agree; push carries a title only.
- AC9 Codex: with `questions: ask` the client advertises `experimentalApi`, answers `item/tool/requestUserInput` in the documented shape; the decline encoding is verified
  against the real app-server (or the fallback is documented).
- AC10 ACP: with `questions: ask` the client advertises `elicitation.form` only, maps the form subset, declines the rest; `accept|decline|cancel` mapped.
- AC11 "Always allow in this project": a `permission_grants` row from a human `scope: project` answer, evaluated after floor and policy, never offered for the
  never-offered list, audited on every use, revocable in the UI and CLI, expiring.
- AC12 Additive wire: old clients ignore the new events and states; a run never hangs silently.

## Test Scenarios

PR1 (`internal/taskrunner`, `internal/runs`, `internal/wsapi`, `internal/inbox`, `cmd/smind`, fake CLI):
`TestParseClaudeAskUserQuestion_*`, `TestClaudeAnswersInput_GoldenAgainstRecordedShape` (golden against the AC1 fixture),
`TestClaudeAdapter_AskUserQuestionDeclineDeniesWithNote`, `TestQuestion_TimeoutDeclinesWithNote`, `TestQuestion_NeverAutoAnswers`, `TestQuestion_SeatDeclinesImmediately`,
`TestQuestion_ProfileDeclineAndDisabled`, `TestQuestion_RepeatGuard`, `TestQuestion_CapsAndTruncationFlag`, `TestQuestion_SecretVocabularyRefused`,
`TestQuestion_AnswerValidation` (unknown option, over cap, double answer), `TestQuestion_RunStopAbandons`, `TestWsapi_AnswerQuestionIsHumanOnly` (walks the handler map and the
MCP tool list), `TestMCP_TaskWaitReportsWaitingQuestion`, `TestInbox_QuestionItem`, `TestCLI_TaskAnswer*`, `TestEvents_UnknownQuestionTypesIgnoredByOldDecoder`.
PR2 (`internal/codex`, `internal/acp`, fake agents): `TestCodex_InitializeAdvertisesExperimentalOnlyWhenAsk`, `TestCodex_RequestUserInputAnswerShape`,
`TestCodex_IsSecretRefused`, `TestCodex_NonBlockingAnswersEmpty`, `TestCodex_McpElicitationFormMapped_UrlDeclined`, `TestACP_InitializeAdvertisesFormNotUrl`,
`TestACP_ElicitationFormMapping` (table over property types), `TestACP_ElicitationUnsupportedDeclined`, `TestACP_ElicitationAcceptDeclineCancel`.
PR3 (UI): `question-card.test.tsx` (single, multi, text, truncated, refused, timeout-resolved, no markdown/links), mobile sheet unit tests, light+dark frames for every
state and the transition frames (open, select, submit, resolve); `TestPush_QuestionTitleOnly`.
PR4: `TestGrants_OnlyHumanScopeProject`, `TestGrants_EvaluatedAfterFloorAndPolicy`, `TestGrants_NeverOfferedForFloorDestructiveNetworkUnparsedQuestion`,
`TestGrants_ExpireAndRevoke`, `TestGrants_UseIsAudited`, `TestGrants_PolicyDenyBeatsGrant`.

Mutations (committed code, one at a time; the named test must then fail): the answer sent without `answers` → the golden answer test; the timeout returns the first option →
`TestQuestion_NeverAutoAnswers`; the seat guard removed → `TestQuestion_SeatDeclinesImmediately`; the secret check removed → `TestQuestion_SecretVocabularyRefused`; a grant
evaluated before the floor → `TestGrants_EvaluatedAfterFloorAndPolicy`; an MCP tool added that answers → `TestWsapi_AnswerQuestionIsHumanOnly`.

## PR split and write scopes

1. **PR0 (probe, no product code):** the Claude `AskUserQuestion` Haiku probe + fixture + mode table (AC1). Wait for the Human's D9 answer first.
2. **PR1 (Claude + core):** model, events, RPC, pending machinery, unattended policy, secret refusal, Claude mapping, inbox kind, `waiting_question`, CLI `task answer`.
3. **PR2 (Codex + ACP):** capabilities, handlers, mappings (the profile field `questions` already landed in PR1). **The Codex half is waiting on the Codex probe** (the decline encoding and `isBlocking: false` semantics need the real app-server; Codex probes are deferred, no credits). The ACP half and the Claude path go ahead.
4. **PR3 (UI):** web card, mobile sheet, push, light/dark frames. Parallelisable with PR2 after PR1.
5. **PR4 ("always allow"):** `permission_grants`, card scope choice, settings list, CLI. After PR1.

One PR at a time, each stops for the Human's review.

## Decisions

D1-D10 accepted as recommended (2026-11-09, under the Human's delegation). Conditions: Haiku probes pre-approved; Codex probes deferred.

PR1 as built, and what it leaves for later (stated, not hidden):
- The Claude answer shape is pinned by a recorded probe (fixtures in `internal/taskrunner/testdata/askuserquestion/`). The CLI did not advertise the `unanswered` capability, so a decline is a deny with a message; it also still sends the request under bypass permissions.
- The per-profile `questions` setting is a profile column (default `ask`); seeded validator, steward and peer profiles default to `decline`.
- Not in PR1: the mobile sheet and push (PR3), the grants table (PR4), Codex and ACP handlers (PR2), a `waiting_question` state in the task tree, fleet mirrors and automations (they still read a parked question as a running run; MCP `task_wait` and `task_status` do report `waiting_question`), the "an agent asked for a secret" inbox line and the "ask again" affordance after a timeout (both need a history-derived resolved item), the `decline` default for automations and orchestrated Leads (only the seeded peer, validator and steward profiles default to `decline` in PR1; a Lead stays `ask`), and D10's resolved or timed-out inbox row (a run event carries no timestamp, so listing a timed-out question needs a small event-time addition; it stays a follow-up).

## Progress

- 2026-11-08: ADR drafted (Proposed), spec written; ADR merged.
- 2026-11-09: Claude `AskUserQuestion` probe (Haiku, five scenarios, about USD 0.014); PR1 built: model, events, decider, RPCs, profile field, inbox item, MCP state, CLI, web card.
- Earlier note: Code facts checked against `internal/taskrunner/permission.go`, `internal/runs/registry.go`, `internal/codex/client.go`,
  `internal/acp`, `internal/inbox`.

## Open items (from review, not in PR1)

- **Protect `smind task approve|deny|answer` from agent shells in the policy engine** (a deterministic deny rule on the command for seats and agents). Today `smind task answer` refuses when `SMIND_RUN_ID` is set, but that is a speed bump: an agent can unset the variable, and the daemon treats an unidentified local connection as a human. `task approve` and `task deny` have the same pre-existing hole.
- Repeat guard hashes the question text only (a reworded loop passes until the cap of 3 per 10 minutes); refusals for seat, profile, secret and repeat never touch the guard.
- Text keys of `answers` collide for two questions with identical text (the CLI keys the same way).
- Secret vocabulary is English only and a heuristic; a secure entry path is a later ADR (D3).
- The `ask|decline|disabled` literals are repeated in profiles, store and plugins; one shared constant set is a cleanup.
- Question card nits: an `aria-label` per Other input, a hint saying why Submit is disabled, key the card by request id instead of a reset effect.

## Validation

PR1 (review round 2 applied). Acceptance criteria, with where each is shown:

- AC1 Claude answer shape pinned: recorded fixtures `internal/taskrunner/testdata/askuserquestion/` and `TestClaudeAnswersInput_GoldenAgainstRecordedShape`, `TestClaudeAnswersInput_UsesTheOriginalLabel`. The probe results are kept under the dogfood directory, not in the repo.
- AC2 `TestParseClaudeAskUserQuestion_*`, `TestClaudeAdapter_AskUserQuestionAnswerAllowsWithAnswers`.
- AC3 answer and decline mapping: the same adapter tests plus `TestQuestion_HumanAnswerResolvesAndIsRecorded`.
- AC4 human only: `TestWsapi_AnswerQuestionIsHumanOnly`, `TestWsapi_QuestionMethodsRefusedForRunScoped`, `TestMCP_NoToolAnswersQuestions`, `TestTaskAnswer_RefusedInsideAnAgentRun`; the open item above states the remaining limit.
- AC5 unattended: `TestQuestion_TimeoutDeclinesWithNote`, `TestQuestion_NeverAutoAnswers`, `TestQuestion_ProfileDeclineAndDisabled`, `TestQuestion_UnreadableProfileFailsClosed`, `TestQuestion_DisabledProfileDisallowsAskTool`, `TestQuestionNoteTimeout_UsesTheRealDuration`.
- AC6 secrets: `TestAsksForSecret_Evasions`, `TestQuestion_SecretRefusedNothingRenderedOrStored`, `TestQuestion_StoredAnswerTextIsScrubbed`.
- AC7 untrusted text: `TestParseClaudeAskUserQuestion_StripsEveryDangerousRange`, caps tests, web `question-card.test.tsx` and e2e `Q-04`.
- AC8 inbox, web, CLI, MCP: `internal/inbox` question tests, `TestBuildTaskWaitOutput_WaitingQuestion`, `TestTaskAnswer_*`, e2e `Q-01`..`Q-05`.
- AC12 additive and never silent: `TestEvent_QuestionRoundTripsThroughPersistence`, `TestQuestion_RestartClosesOrphanRequests`, `TestWsapiMethodMapGoldenFleetOff`.
- Not yet shown: AC9, AC10 (PR2), AC11 (PR4), push and mobile card (PR3).
