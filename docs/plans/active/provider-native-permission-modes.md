# Provider-native permission modes (remove auto-safe)

Design: [ADR-0019](../../decisions/0019-provider-native-permission-modes.md)
(Proposed). Branch: `refactor/provider-native-permission-modes`.
**Phase 1 (this doc + ADR) is design only. Implementation starts after the
user resolves the Open questions below.**

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
     The decider is installed for every mode except `bypassPermissions`.
     No `--allowedTools` Bash rules.
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
    `cmd/smind/main.go` usage text drop auto-safe. ADR-0019 moves to
    Accepted.
13. `task test` and `task lint` pass. The web UI gets a light+dark
    screenshot pass of the composer mode picker, the profile form, and the
    mid-run control.

## Test Scenarios

Go (`internal/taskrunner`, `internal/runs`, `internal/wsapi`, `internal/store`, `cmd/smind`):

- **S1 claude mode passthrough**: for each claude mode, the fake CLI
  receives `--permission-mode <id>` and `--allow-dangerously-skip-permissions`,
  and no `Bash(...)` `--allowedTools`.
- **S2 claude bypass installs no decider**: `bypassPermissions` produces no
  can_use_tool round-trip to the decider. `acceptEdits` routes a Bash
  can_use_tool to the decider and then to a human.
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
- **S14 legacy event rendering**: a stored `permission_resolved` event
  with resolution `auto_safe` still decodes, and `run.logs` returns it.
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

## Open questions (need user answer before implementation)

1. **Claude default mode: `acceptEdits` or `default`?** Paseo defaults to
   `default`. smind's `manual` has used `acceptEdits` since 2026-09-11,
   because under `default` the CLI blocked edits without emitting
   can_use_tool (`runner.go:521-527`). *Recommendation:* make `acceptEdits`
   the default. Add a spike (step 3) to check whether `default` now emits
   can_use_tool for edits via `--permission-prompt-tool stdio`. If not,
   list `default` with a description noting that edits are blocked
   headless, or drop it.
2. **Offer Claude `auto` (classifier) mode?** It's the closest native
   replacement for auto-safe's goal of unattended verification, but it
   costs classifier calls and requires the Anthropic API.
   *Recommendation:* offer it, with `autoApproves=true` so it's blocked
   from MCP `task_send` unless it comes from a profile.
3. **ACP `autoAccept` toggle: keep it (Paseo parity) or rely only on the
   agent's own bypass mode?** *Recommendation:* keep it, as a separate
   boolean, because Kimi and other agents may advertise no bypass mode. It
   is never auto-enabled.
4. **ACP mode discovery: probe `session/new` or a static catalog?**
   *Recommendation:* a lazy probe per provider, cached for the daemon's
   lifetime, with a timeout and a static fallback, plus refresh on
   `provider.test`. Needs a live capture of glm-acp-agent's and Kimi's
   `session/new` `modes` (user-reported GLM modes: `default`,
   `accept_edits`, `bypass_permissions`; not yet verified here).
5. **Codex `auto-review` and `read-only`?** Paseo version-gates
   auto-review. *Recommendation:* ship `auto` + `full-access` only, and
   add the others later.
6. **Orchestrator (MCP `task_send`) and auto-approving modes.** Should an
   orchestrating agent be allowed to pick `bypassPermissions`/`auto`/
   `autoAccept`? *Recommendation:* no, unless the mode comes from a
   human-authored profile referenced by id (ADR-0019 point 6).
7. **Legacy wire field: hard error or silent ignore?** *Recommendation:*
   hard error, since smind's clients ship together. Confirm the mobile app
   never sends `approvalPolicy` (grep shows it only in comments).
8. **Old `approval_policy` columns: keep them unused or rebuild the tables
   to drop them?** *Recommendation:* keep them unused.

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
11. **MCP guard** on the ADR-0017 `task_send` (S17). Rebase onto #223 once
    it lands on develop.
12. **Docs**: ADR-0014/0018 notes, README, ADR-0019 → Accepted. Move this
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

- 2026-09-28: Phase 1 design done. ADR-0019 (Proposed) and this plan are
  written. Awaiting answers to the Open questions. No code changed.

## Validation

(Filled in during implementation: map each Acceptance Criterion to the
test/scenario or manual check that confirmed it.)
