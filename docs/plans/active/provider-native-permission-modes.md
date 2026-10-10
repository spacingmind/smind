# Provider-native permission modes (remove auto-safe)

Design: [ADR-0019](../../decisions/0019-provider-native-permission-modes.md)
(Accepted 2026-09-28). Branch: `refactor/provider-native-permission-modes`.

## Acceptance Criteria

1. `internal/taskrunner/policy.go` is deleted, along with `ApprovalPolicy`,
   `AllowlistedCommand`, `safeCommandPrefixes`, `SafeBashRules`, and
   `isBareCd`/`splitShellChainSegments`. `autoAllowACPFileEdit`,
   `fileEditPathFromTitle`, `acpCommand`, `bashCommand`, and
   `LivePolicyDecider` are removed from `permission.go`.
   `grep -rn "auto-safe\|AutoSafe\|AllowlistedCommand\|SafeBashRules" --exclude-dir=docs`
   returns only the legacy `PermissionResolvedByAutoSafe` constant, its
   timeline label, and migration code/tests.
2. `PermissionDecider.Decide` no longer takes a `command` argument.
   `runPermissionDecider.Decide` never auto-resolves to allow. Its only
   outcomes are a human answer, timeout-deny, provider cancellation, or run
   stop.
3. `provider.list` returns, for every provider, `modes: [{id, label,
   description, autoApproves}]` and `defaultMode`:
   - claude-native: `default`, `acceptEdits`, `plan`, `auto`,
     `bypassPermissions`. `auto` is omitted when `CLAUDE_CODE_USE_BEDROCK`
     or `CLAUDE_CODE_USE_VERTEX` is set in the daemon env.
   - codex-native: `auto`, `full-access`.
   - glm/kimi: the agent's advertised modes from a cached probe
     `session/new`, or a static fallback (`[default]`) if the probe fails.
     These providers also report `supportsAutoAccept: true`.
4. `task.prompt` and `run.start` accept `permissionMode` (and `autoAccept`
   for ACP providers). An unknown mode for that provider returns an error
   listing the valid ids. An empty mode means the provider's `defaultMode`.
   A request carrying the legacy `approvalPolicy` field is rejected with an
   error that names `permissionMode`.
5. Runner applies the mode natively:
   - claude: `--permission-mode <id>` plus `--allow-dangerously-skip-permissions`.
     The decider is installed for every mode, including `bypassPermissions`
     (in bypass the CLI never asks, but a mid-run switch to an asking mode
     must still reach a human; see Decisions). No `--allowedTools` Bash
     rules.
   - codex: `thread/start` carries that mode's `approvalPolicy`/`sandbox`
     preset.
   - ACP: `session/set_mode` (or a mode config option) after `session/new`
     when the mode differs from the agent's current one. `autoAccept=true`
     installs `acp.AutoApprovePolicy`.
6. `run.setPermissionMode {runId, modeId}` (and, for ACP, `autoAccept`)
   switches a running run: Claude via `SetPermissionMode`, ACP via
   `session/set_mode`. Codex returns a clear "not supported mid-run" error.
   A finished run returns an error. `run.setApprovalPolicy` is removed.
7. Runs and agent profiles persist `permission_mode` (and `auto_accept`).
   The migration backfills existing rows with ADR-0019's table. After
   migration, a legacy `auto-safe` run or profile never maps to a mode with
   `autoApproves=true`.
8. The web composer's mode picker, the agent-profile form, and the task
   header pill render the catalog from `provider.list`, using provider
   labels verbatim. The mid-run control offers the running provider's
   switchable modes. `lib/approval-policies.ts`, `approval-policy-cycle.ts`
   (becomes a mode cycle), and `approval-policy-control.tsx` are
   replaced. A stored composer state holding legacy `approvalPolicy` loads
   as the provider's `defaultMode`. The Shift+Tab cycle walks the catalog.
9. CLI: `smind task send ... --mode <id> [--auto-accept]` and `smind
   profile add ... --mode=<id>`. `--approval-policy` exits non-zero with a
   message pointing to `--mode`. `profile ls` shows a MODE column.
10. `smind task permissions`, streaming, and logs print the copy-pasteable
    `-> smind task approve <runId> <requestId>` hint (salvaged from e5017dd).
11. ADR-0017's MCP `task_send` (PR #223 or its follow-up) rejects a
    `permissionMode` whose `autoApproves` is true, and rejects
    `autoAccept=true`, unless the mode comes from a human-authored profile
    passed by id.
12. Docs: ADR-0014 and ADR-0018 get a one-line "superseded in part by
    ADR-0019" note where they describe approvalPolicy/auto-safe. README and
    `cmd/smind/main.go` usage text drop auto-safe. ADR-0019 is Accepted.
13. `task test` and `task lint` pass. The web UI gets a light+dark
    screenshot pass of the composer mode picker, the profile form, and the
    mid-run control.

## Test Scenarios

Go (`internal/taskrunner`, `internal/runs`, `internal/wsapi`, `internal/store`, `cmd/smind`):

- **S1 claude mode passthrough**: for each claude mode, the fake CLI
  receives `--permission-mode <id>` and `--allow-dangerously-skip-permissions`,
  and no `Bash(...)` `--allowedTools`.
- **S2 claude decider wiring**: `acceptEdits` routes a Bash can_use_tool
  to the decider and then to a human. The decider stays installed in
  bypass too, so a later switch can still ask (whether the CLI actually
  skips asking in bypass is the CLI's own behavior, a manual check).
- **S3 decider never auto-allows**: a Bash request for `go test ./...`
  (previously auto-safe-allowlisted) now goes pending and waits for
  `run.respondPermission`. On timeout it resolves to deny.
- **S4 ACP modes discovered**: fake ACP agent advertises
  `modes.availableModes` in `session/new`. The probe caches them and
  `provider.list` returns them. A second call doesn't respawn.
- **S5 ACP mode via config option**: the agent advertises no `modes` but
  has a `category:"mode"` select config option. Those choices become the
  catalog, and applying one sends `session/set_config_option`.
- **S6 ACP probe failure**: the probe agent errors or times out.
  `provider.list` still returns, with the fallback catalog, and does not
  hang past the probe timeout.
- **S7 ACP set_mode applied**: `run.start` with a non-current mode sends
  `session/set_mode` before `session/prompt`. An agent-rejected mode fails
  the run with a clear error.
- **S8 ACP autoAccept**: with `autoAccept=true`, a `request_permission` is
  answered `allow_*` with no pending state. With false, it goes pending.
  (The old in-worktree-edit fast path is gone: an in-worktree edit request
  goes pending.)
- **S9 codex presets**: `auto` → `thread/start` has
  `approvalPolicy:on-request`, `sandbox:workspace-write`. `full-access` →
  `never`/`danger-full-access`.
- **S10 validation**: unknown mode → error listing valid ids. Legacy
  `approvalPolicy` param → error naming `permissionMode`. `autoAccept` on
  a non-ACP provider → error.
- **S11 mid-run switch**: Claude run `acceptEdits` → `default` sends a
  `set_permission_mode` control request, and the run payload reports the
  new mode. ACP sends `session/set_mode`. Codex → error. Finished run →
  error. Unknown run → ErrNotFound.
- **S12 mid-run autoAccept toggle**: flipping `autoAccept` on for a
  running ACP run makes the next `request_permission` auto-allowed.
  Already-pending requests are unaffected.
- **S13 migration table**: seed runs/profiles with
  `manual|auto-safe|full-access|''` × each provider, migrate, and assert
  the `permission_mode`/`auto_accept` values from ADR-0019's table.
  Migration is idempotent on a second run.
- **S14 legacy event rendering**: an `auto_safe` resolution still renders
  in the timeline, labelled as legacy. (Implementation found that
  permission resolutions are never persisted to `run_events`, since the
  persisted event shape has no resolution field, so no stored `auto_safe`
  data exists. Only a live payload from an older daemon could carry it.)
- **S15 CLI**: `task send --mode acceptEdits` sends `permissionMode`.
  `--approval-policy auto-safe` exits 2 with a message pointing to `--mode`.
  `profile add --mode=plan` round-trips.
- **S16 approve hint**: `renderPermissionRequest` output contains
  `smind task approve <runId> <requestId>` (port e5017dd's test).
- **S17 MCP guard** (on the MCP branch): `task_send` with
  `permissionMode:bypassPermissions` or `autoAccept:true` is rejected.
  With a profile id whose mode is bypass, it is accepted.

Web (`web/packages/ui`, vitest):

- **W1**: the composer mode picker lists exactly the selected provider's
  catalog, and switching provider resets to that provider's `defaultMode`.
- **W2**: a legacy localStorage state with `approvalPolicy:"auto-safe"`
  loads as `defaultMode`, not rejected wholesale (provider/thinking are
  kept).
- **W3**: Shift+Tab cycles through catalog modes and wraps.
- **W4**: the profile form saves `permissionMode`, and the card metadata
  line shows the provider label.
- **W5**: the mid-run control calls `run.setPermissionMode` and is hidden
  for codex-native.
- **W6**: a timeline row for a legacy `auto_safe` resolution renders a
  "legacy auto-safe" label.

## Decisions

- Direction: provider-native modes like Paseo, with no smind allowlist
  (user, 2026-09-28). Recorded in ADR-0019.
- The `fix/auto-safe-read-only-commands` branch is abandoned. Only e5017dd
  (the approve-hint fix) is salvaged, and it is cherry-picked or re-applied
  in step 1.
- Migration fails toward asking a human (ADR-0019 table). Browser drafts
  are discarded rather than mapped.
- The user accepted all eight open-question recommendations on 2026-09-28
  (recorded in ADR-0019's Resolved decisions):
  1. Claude default is `acceptEdits`. `default` stays listed, and its edit
     behavior is a manual live check.
  2. Claude `auto` is offered, with `autoApproves=true`.
  3. The ACP `autoAccept` toggle is kept and never auto-enabled.
  4. ACP modes come from a lazy probe `session/new`, cached, with a timeout
     and a `[default]` fallback. GLM/Kimi's real modes are a manual live
     check.
  5. Codex has `auto` and `full-access` only.
  6. MCP `task_send` can't pick an auto-approving mode or `autoAccept`
     except via a profile id.
  7. The legacy `approvalPolicy` field is a hard error.
  8. The old `approval_policy` columns are kept but unused.
- Implementation decisions (2026-09-28, within the accepted design):
  - The Claude decider is installed for every mode, bypass included, so a
    live switch out of bypass still reaches a human (AC5 updated).
  - ACP `auto-safe` rows migrate to the agent default `""`, not
    `accept_edits`, because no agent runs at migration time to say whether
    it advertises that mode. This is the narrower choice. ACP `full-access`
    becomes `autoAccept=true`, exactly what it did before
    (`acp.AutoApprovePolicy`).
  - With a decider, AutoAccept is applied in `runPermissionDecider` and
    recorded as a new `auto_accept` resolution, so the timeline shows that
    the request was auto-accepted. Without a decider it installs
    `acp.AutoApprovePolicy`.
  - ACP mode probing is opt-in (`WithACPModeProbe`, on in `serve`), so test
    Runners never spawn real agents. Real runs always feed the mode cache.
    While an ACP provider's modes are undiscovered, any mode id is
    accepted and the agent itself rejects a bad one (the run fails with the
    agent's error).
  - Claude Code's installed CLI lists `manual` as the canonical id for
    `default` and accepts `default` as an alias (Claude Code CHANGELOG).
    smind keeps Paseo's `default`.

## Implementation steps (after sign-off)

1. **Salvage e5017dd**: `renderPermissionRequest(runID, requestID, ...)`
   and its test (S16). Small separate commit.
2. **Catalog**: `taskrunner.ModeInfo` and `ProviderInfo.Modes/DefaultMode`,
   static Claude/Codex catalogs, and served in `provider.list`.
3. **Claude spike**: check `default`-mode edit behavior (open question 1).
   Add `--allow-dangerously-skip-permissions` via `WithExtraArgs`.
4. **ACP**: parse `modes` from `session/new` (`internal/acp/client.go:118`),
   add `SetMode` (`session/set_mode`) and a probe/cache (S4-S7).
5. **Codex**: `thread/start` approvalPolicy/sandbox presets (verify param
   names against `refs/codex` app-server protocol) (S9).
6. **Runner**: replace the `approvalPolicy` parameter of
   `RunPrompt`/`runACP`/`runClaudeNative`/`runCodexNative` with
   `PermissionMode` + `AutoAccept`. Delete `policy.go`, the allowlist,
   `SafeBashRules`, the ACP edit fast path, and `LivePolicyDecider`. Drop
   the `command` argument (S1-S3, S8).
7. **runs + store**: `permission_mode`/`auto_accept` columns and migration
   (S13). `Registry.Start` signature, `Run.PermissionMode`,
   `SetPermissionMode` with live client hooks (reuse the
   `SetConfigOption` path, `internal/runs/config_options.go`) (S11, S12).
   Delete `approval_policy.go`.
8. **wsapi**: `permissionMode`/`autoAccept` params, legacy-field
   rejection, `run.setPermissionMode`, profile RPCs (S10). Update
   `internal/profiles/registry.go` validation.
9. **CLI**: `--mode`/`--auto-accept`, the profile MODE column, and usage
   text in `main.go` (S15).
10. **Web**: catalog-driven picker, profile form, header pill, mid-run
    control, localStorage migration, timeline legacy label (W1-W6). Then a
    light+dark screenshot pass.
11. [x] **MCP guard** on the ADR-0017 `task_send` (S17) -- delivered on
    the `feat/mcp-task-send` branch (ADR-0017 step 3):
    `TestMCPTools_TaskSendRejectsAutoApprovingMode`.
12. **Docs**: ADR-0014/0018 notes, README. Move this
    plan to completed.

## Files/areas to delete or change

- **Delete**: `internal/taskrunner/policy.go`, `policy_test.go`,
  `internal/runs/approval_policy.go`, `approval_policy_test.go`,
  `internal/wsapi/run_approval_policy_test.go` (replaced),
  `internal/taskrunner/permission_edit_test.go` (ACP edit fast path),
  `web/packages/ui/src/lib/approval-policies.ts` (+test),
  `components/approval-policy-control.tsx`.
- **Change**: `internal/taskrunner/{permission.go,runner.go,event.go,thinking.go,provider.go,runner_test.go,taskrunner_test.go,fakeagent/main.go}`;
  `internal/acp/client.go`; `internal/codex/client.go`;
  `internal/runs/{registry.go,runs.go,*_test.go}`;
  `internal/store/{schema.sql,migrate.go,runs.go,agent_profiles.go,types.go,*_test.go}`;
  `internal/profiles/registry.go`; `internal/wsapi/handlers.go` (+tests);
  `cmd/smind/{task.go,profile.go,main.go}` (+tests);
  `web/packages/ui/src/{lib/types.ts,hooks/use-run-timeline.ts,components/composer/{composer.tsx,run-config-toolbar.tsx,run-config-preference.ts,approval-policy-cycle.ts},components/task-detail.tsx,components/settings/profiles-section.tsx}` plus the listed tests;
  `mobile/src/permissionRequests.ts` (comments only).
- **Docs**: ADR-0014, ADR-0018 notes; README; `docs/plans/active/run-config-ia.md`
  and `web-ui-*.md` mentions (update if still active).

## Progress

- 2026-09-28: Phase 1 design done. ADR-0019 is Accepted and all open
  questions are resolved.
- 2026-09-28: Phase 2 implemented on `refactor/provider-native-permission-modes`
  (rebased onto develop after #222/#223 merged), one commit per step:
  - `635c499` step 1: salvaged e5017dd (approve hint includes the runId).
  - `3697ead` step 2: static mode catalog in `provider.list`.
  - `5ca2197` step 4: ACP modes, `SetSessionMode`, probe and cache.
  - `c10e16e` step 5: Codex approvalPolicy/sandbox on thread start/resume.
  - `4178fbd` steps 3 and 6-9: the Claude flag (step 3's spike could only
    be done live, see manual checks), runner, runs, store and migration,
    wsapi, CLI.
  - `faba145` step 10: web UI.
  - `b6cceb3` step 12: docs.
- Step 11 (MCP `task_send` guard) was **not done**. PR #223 landed only the
  read-only tools, and `task_send` doesn't exist yet. The requirement and
  its test (`TestMCPTools_TaskSendRejectsAutoApprovingMode`) are now in
  `docs/plans/active/mcp-server.md` step 3, and ADR-0017's `task_send` row
  names `permissionMode`/`autoAccept`/`profileId`.
  `taskrunner.ModeAutoApproves` is the check to call.
- 2026-09-28: Opus review #6 and #7 fixed: `4e07e3b` persists live
  permission-mode/auto-accept switches to the run row
  (`store.UpdateRunPermission`), `2116559` fixes `profile add
  --auto-accept[=bool]` flag parsing.
- This plan stays in `active/` until AC11 lands with `task_send` and the
  manual checks below are done.

## Validation

`task test` and `task lint` pass (Go: all packages; web: 106 files, 1346
tests). `cmd/smind`'s `TestMCPServe_ExitsWhenDaemonConnectionDies` (from
#223, a file this branch doesn't touch) failed once in about 10 runs and
passed on every rerun. That's a pre-existing flake.

| AC | Status | How confirmed |
|---|---|---|
| 1 | ✅ | `policy.go` and the allowlist/fast-path code are deleted. The grep only finds the legacy `PermissionResolvedByAutoSafe` constant and label, migration code/comments, and tests that use `"auto-safe"` as an invalid/legacy input. |
| 2 | ✅ | `PermissionDecider.Decide(ctx, summary, options)`. S3 `TestRunPermissionDecider_Decide_NeverAutoAllows`; `TestRunPermissionDecider_Decide_AutoAccept_AutoAllows` covers the only auto-allow, the human's per-run AutoAccept. |
| 3 | ✅ | `TestSupportedProviders_ModeCatalogs`, `TestClaudeModes_AutoHiddenOnBedrockOrVertex`, S4-S6 `TestProviderCatalog_*`, `TestServer_ProviderList_RoundTrip` (catalog present). |
| 4 | ✅ | S10 `TestValidatePermissionSettings`, `TestRegistry_Start_ValidatesPermissionSettings`, `TestServer_RunStart_PermissionSettings` (unknown mode, autoAccept on non-ACP, legacy field is a hard error naming permissionMode). |
| 5 | ✅ | S1 `TestRunner_RunPrompt_ClaudeNative_PermissionModeFlags`, S9 `TestRunner_RunPrompt_CodexNative_ModePresets`, S7/S5 `TestRunner_RunPrompt_ACP_AppliesPermissionMode`, `TestRunner_RunPrompt_GLM_AutoAcceptWithoutDecider`. |
| 6 | ✅ | S11 `TestRunner_SetPermissionMode`, `TestRegistry_SetPermissionMode`, `TestRegistry_SetPermissionMode_CodexUnsupported`, `TestServer_RunSetPermissionMode_*`. Manual: clicking a mode in the live control of a running fake-GLM run wrote `set_mode:accept_edits` to the agent. |
| 7 | ✅ | S13 `TestMigrate_PermissionModesBackfill` (every policy x provider, runs and profiles, idempotent across reopen), `TestStore_Runs_PermissionMode_RoundTrips`, profile store tests. |
| 8 | ✅ | W1-W6: `composer.test.tsx` (mode list, provider switch reset, Auto-accept, Shift+Tab, legacy localStorage), `permission-mode-cycle.test.ts`, `permission-modes.test.ts`, `profiles-section.test.tsx` W4, `task-detail.test.tsx` (live control, hidden for Codex, pill), `permission-reason`/`run-timeline` tests (auto_accept, legacy auto_safe). |
| 9 | ✅ | S15 `TestTaskSend_ModeFlags`, `TestRunProfileAddPrintsCreatedRow` (`--mode=plan`), `TestRunProfileAddRejectsRemovedApprovalPolicyFlag`. Manual: `task send --approval-policy auto-safe` exits 2 pointing at `--mode`. |
| 10 | ✅ | S16 (e5017dd's test). Manual: `smind task permissions` printed `-> smind task approve <runId> <requestId>`. |
| 11 | ✅ | Delivered on `feat/mcp-task-send` (mcp-server.md step 3): `TestMCPTools_TaskSendRejectsAutoApprovingMode` -- bypass mode and `autoAccept: true` rejected when caller-supplied, the same bypass mode via a human-authored `profileId` accepted, `profileId` + explicit values rejected, `profileId` + mismatched provider rejected. |
| 12 | ✅ | ADR-0014/0018 notes, ADR-0017 `task_send` row, README "Permission modes", `cmd/smind/main.go` usage. |
| 13 | ✅ | `task test`/`task lint` green. Light and dark screenshots (Playwright against a sandbox daemon with the fake ACP agent in `modes:session`): composer mode picker (Claude catalog, GLM discovered catalog), Auto-accept toggle off/on, live mid-run control, Settings → Agents mode field. Local only (`/tmp/smind-shot/out`), not committed. |

### Manual verification still needed (needs a live provider)

1. **GLM's and Kimi's real advertised modes**: run `smind serve`, open
   the composer on GLM/Kimi, and confirm `provider.list` shows the agent's
   own modes (user-reported GLM: `default` / `accept_edits` /
   `bypass_permissions`) and that selecting one sends `session/set_mode`.
   Fallback until then: `[default]`.
2. **Claude `default` mode and edits**: does `--permission-mode default`
   now route file edits through can_use_tool (asking a human), or still
   block them silently in headless mode as seen on 2026-09-11? If it still
   blocks, reword or drop `default` from `claudeModes()`. `acceptEdits`
   stays the default regardless.
3. **Claude live switch**: switch a running claude-native run between
   `acceptEdits`/`plan`/`bypassPermissions` from the mid-run control
   (`set_permission_mode`). This also confirms
   `--allow-dangerously-skip-permissions` lets bypass be entered mid-run.
4. **Claude `auto` mode** on an OAuth (non-Bedrock) account: the
   classifier approves or blocks without prompting.
5. **Codex presets**: `full-access` really runs with no approvals, and
   `auto` escalates as `on-request`/`workspace-write`, against a real
   `codex app-server` (including the `thread/resume` path).
