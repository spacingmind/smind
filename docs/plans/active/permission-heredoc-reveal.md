# Permission cards: elide heredoc bodies by default, reveal on demand

Status: SPEC, awaiting Steward review. Size: L (security, wire shape, 3 clients). Owner: Lead (profile 7).

## Decision record

**Human decision 2026-11-01 (relayed by the Supervisor):** "elide by default, reveal on demand". The daemon always computes the elided form of a command for `detail.command` on every permission detail and every provider path, policy engine on or off. The body is read on demand through a human-only RPC, redacted, and only while the permission is pending. The raw body stays only in the run's own tool-call log. No compat shim (no users): every fixture that asserted the raw `detail.command` is updated.

Builds on #803 (separator fix) and #804 (nested heredoc elision in `permpolicy.Parsed.Normalized`): this plan needs `Normalized` to be body-free at any nesting depth.

## Why

Heredoc bodies are where pasted secrets live. Today `detail.command` is the raw command (capped) on the wire: `taskrunner.BuildPermissionDetail` (ACP execute, `permission_detail.go:95`), `claudeBashDetail` (`permission.go:241`), and the codex adapter (`permission.go:268`, via `CommandExecutionApprovalRequest`). The web card hides it only when `policy.normalized` exists (`permission-detail.tsx:196`); the CLI viewers print it always (`cmd/smind/permission_detail.go:60`, `run_viewer.go:168`). The elided form must not depend on the policy engine being on.

## Design

1. **One choke point.** `runPermissionDecider.decide` (`internal/runs/registry.go`) is where every provider's request converges before it is recorded, parked or notified. A new `elideDetail(detail, summary)` runs there first: it computes `permpolicy.SplitCommand(cmd)` for an `execute` detail, keeps the raw body spans out of band, and rewrites `detail.Command` to `Normalized`. `summary` is elided the same way when it embeds the command (providers put the command in the title). `gitRuleInfo` keeps classifying the raw command (decisions unchanged); `GitRule.Command` is argv-only, so it carries no body.
2. **Out-of-band store.** The raw bodies live in the run's `pendingPermissions` entry (server memory, never in `history`, never persisted, never emitted). Each elided body gets a stable `index` (operator order across nesting, as the scanner records them) and its byte count.
3. **Wire shape.** `PermissionDetail` gains `heredocs: [{index, bytes}]` (omitted when none). `command` contains `<<TAG [heredoc N bytes]` markers; the Nth marker is `index N`. No body text, no preview.
4. **Reveal RPC.** `permission.revealHeredoc {permissionId, index}` -> `{index, bytes, text, redacted: bool, truncated: bool}`. `text` is the body passed through `internal/secretdeny`/`redact` (same redactor the audit uses), capped at 64 KiB with `truncated`. Errors: `not_found` when the permission is not pending (resolved, expired, unknown id) or the index is out of range. Never writes the body to the event stream, audit or logs; writes one audit row `permission.heredoc_revealed` with `{permissionId, index, bytes}` and no content. Access: human/relay callers only (see scope below).
5. **Scope.** The handler is registered in the human-only set; run-scoped agents and Peers get `forbidden`. The handler-map walk test (like `steward.review`) is extended so adding `permission.revealHeredoc` to a run-scoped map fails it.
6. **Other paths carrying the raw command (to be audited in step 1 of implementation, report exactly which and what changed):** run/tool_call events (`runToolCallParams.Input`, `run.logs`), relay frames, inbox rows (`internal/wsapi/inbox*`), task logs the UI loads, MCP `task_permissions`, `PolicyInfo`. The agent's own tool-call log (`ToolInput`/`RawInput`) is deliberately left raw: it is the run's record of what the agent sent. Anything that reaches a client or relay beyond that gets the elided form.
7. **Clients.** Web and mobile approval cards render `detail.command` (already elided) and, per `heredocs[i]`, a "Show heredoc (N bytes)" toggle that calls the RPC and renders the redacted body inline (collapsible, wrapped, never clipped, same card styles); redacted spans show the redaction marker. CLI: `smind permission show --reveal [--index N]` prints the redacted body to the terminal (sanitised for control codes).
8. **CLI viewers** read the already-elided `detail.command`; no client-side elision.

## Acceptance criteria

- AC1 Every provider path (ACP execute, claude-native Bash, codex command execution) produces a detail whose `command` and `summary` contain no heredoc body, with the policy engine on and off.
- AC2 `detail.heredocs` lists `{index, bytes}` in marker order, including heredocs nested in subshell / `$()` / `bash -c`.
- AC3 `permission.revealHeredoc` returns the body with token-shaped secrets redacted while the permission is pending; refuses (not_found) after resolution; refuses out-of-range index.
- AC4 Run-scoped and Peer callers cannot call it (walk test).
- AC5 The reveal is never written to the audit or event stream; the audit gets a content-free `permission.heredoc_revealed` row.
- AC6 The marker `SECRET_TOKEN_123` in a heredoc never appears in: `detail.command`, `summary`, the permission.pending event, inbox rows, relay frames, CLI output, audit rows. It does appear only in the reveal response (redacted if token-shaped) and the run's own tool-call log.
- AC7 Allow/deny/ask decisions unchanged: full permpolicy, runs and wsapi policy suites pass.
- AC8 Web and mobile cards: collapsed, expanded, redacted-body and long-body frames, light and dark, 1280 and 390, theme pairs differ, nothing wider than the viewport.
- AC9 CLI `smind permission show --reveal` works against a pending permission and refuses otherwise.

## Named tests (planned)

- `TestElideDetail_AllProviders` (runs): table over ACP, claude, codex fixtures x policy on/off.
- `TestElideDetail_NestedHeredocIndexes` (runs/permpolicy).
- `TestPermissionRevealHeredoc_RedactsAndRefusesWhenResolved` (wsapi).
- `TestPermissionRevealHeredoc_NotCallableFromRunScope` + extended handler-map walk.
- `TestPermissionRevealHeredoc_AuditRowHasNoContent`.
- `TestSecretNeverOnWire` (wsapi): the SECRET_TOKEN_123 sweep over event stream, inbox rows, relay frame encoder, audit.
- `permission-detail.test.tsx` (web) and mobile card tests: toggle fetches once, renders redacted body, collapses.
- CLI: `TestPermissionShowReveal`.
- One mutation per load-bearing test, recorded under Validation.

## PR split

1. This spec (docs only).
2. Daemon: `elideDetail` + wire shape + path audit + tests (AC1, 2, 6, 7).
3. Daemon: `permission.revealHeredoc` + scope + audit + tests (AC3, 4, 5) and CLI `--reveal` (AC9).
4. Web card (AC8 web) with frames.
5. Mobile card (AC8 mobile) with frames.

## Open points for the Steward

- `index` is the marker order across nesting; a `bash -c` body whose quoting hides it falls back to whole-command redaction (#804): in that case `heredocs` is empty and the command shows the redaction text (no reveal possible). Recommendation: accept; revisit only if it shows up in practice.
- Reveal cap 64 KiB, matching the 64 KiB command cap.

## Progress

- 2026-11-01 spec written.

## Validation

(filled in as the PRs land)
