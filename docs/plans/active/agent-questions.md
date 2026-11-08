# Agent questions reach the human (ADR-0055)

Status: **spec, not started.** Nothing is implemented until the Human answers the decisions in
[ADR-0055](../../decisions/0055-agent-questions-reach-the-human.md). Size: L (wsapi, inbox, events, UI on three surfaces, a table): each PR below with a
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
`TestClaudeAskUserQuestion_BuildsQuestionRequest`, `TestClaudeAskUserQuestion_AnswerSingleMultiOther` (golden against the AC1 fixture),
`TestClaudeAskUserQuestion_DeclineIsDenyWithNote`, `TestQuestion_TimeoutDeclinesWithNote`, `TestQuestion_NeverAutoAnswers`, `TestQuestion_SeatDeclinesImmediately`,
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
3. **PR2 (Codex + ACP):** capabilities, handlers, mappings, profile opt-in field.
4. **PR3 (UI):** web card, mobile sheet, push, light/dark frames. Parallelisable with PR2 after PR1.
5. **PR4 ("always allow"):** `permission_grants`, card scope choice, settings list, CLI. After PR1.

One PR at a time, each stops for the Human's review.

## Decisions

Proposed in ADR-0055 D1..D9; none taken yet.

## Progress

- 2026-11-08: ADR drafted (Proposed), spec written. Code facts checked against `internal/taskrunner/permission.go`, `internal/runs/registry.go`, `internal/codex/client.go`,
  `internal/acp`, `internal/inbox`.

## Validation

Pending implementation.
