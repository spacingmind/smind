# Role-pack sync for existing installs (ADR-0058)

Open: PR 3 (the optional `--restore <backup>`, ADR-0058 section 8) is not built; PR 1 and PR 2 are validated.

Status: active (2026-11-09). ADR-0058 D1-D6 accepted as recommended (Supervisor, under the Human's delegation). Size: M, two PRs.

## PR 1: engine, history, `--check` / `--diff` / `--apply`, the human-only gates

### Acceptance criteria

- AC1: `rolepack_history.go` holds the SHA-256 of every released value of each synced field of each seat (generated from the git history of the pack); `TestRolePackHistory_ContainsTheCurrentText` fails when the current text is not in it.
- AC2: `CheckRoles` reports, per seat and field, `current` / `stale` (equals an earlier release's value) / `customised`; a missing seat is `missing`, a plugin-owned one `owned`; whitespace-only and line-ending differences never read as customised.
- AC3: `PlanApply` writes only the stale PROMPT fields (`instructions`, `notes`, `skills`) of an untouched seat (no customised field); a customised field, a capability field (`model`, `permissionMode`, `orchestrationTools`, `childPolicy`, `onRateLimit`) or any field of a seat with a customised one changes only with `--take seat[.field]`; the Human's own settings (`provider`, `thinkingLevel`, `autoAccept`, `fallbackProfileId`, `childProfileIds`, `questions`, name, id) are never written.
- AC4: `ApplyRoles` saves every profile it will change, whole, to `<backup dir>/<UTC timestamp>-pack-v<N>/<seat>-<id>.json` (files 0600, directory 0700) BEFORE the first write; no backup, no write; `--dry-run` writes nothing; a profile edited since the check is skipped (`changed_meanwhile`) and the rest proceed; running it twice changes nothing the second time.
- AC5: `profile.update` takes an optional `expectedUpdatedAt`: the write is one conditional UPDATE and a profile changed since is refused with code `conflict`, nothing written; without it the update is unchanged.
- AC6 (CLI): `smind profile seed roles` keeps creating missing seats; `--check` prints one line per field state and exits 0, or 10 when any field is stale; `--diff [seat[.field]]` prints a unified diff of stored against current for each stale or customised field, capability fields marked; `--apply [--backup-dir D] [--take seat[.field]]... [--dry-run]` as AC3-AC4 (default backup directory `<smind home>/backups/role-pack/`), printing each profile's outcome and the backup path; text from the daemon is made terminal-safe.
- AC7 (D6): `smind profile add`, `smind profile edit` and `smind profile seed ... --apply` refuse in an agent run (CLI guard in `run()`) and are denied by the `no-self-approve` floor rule however wrapped (the table gains flag matching); `profile.create` and `profile.update` join `HumanOnlyRPCs`; the drift test covers them.

### Named tests

Engine: `TestRolePackHistory_ContainsTheCurrentText`, `TestRolePackSync_ClassifiesCurrentStaleCustomised`, `..._WhitespaceOnlyDifferenceIsNotCustomised`, `..._MissingAndOwnedSeatsAreReported`, `..._ApplyUpdatesOnlyStalePromptFieldsOfAnUntouchedSeat`, `..._CustomisedSeatIsNeverWrittenWithoutATake`, `..._CapabilityFieldNeedsAnExplicitTake`, `..._UserSettingsAreNeverWritten`, `..._BackupIsWrittenBeforeTheFirstUpdate`, `..._NoBackupNoWrite`, `..._ChangedMeanwhileIsSkippedAndTheRestProceed`, `..._ApplyIsIdempotent`, `..._DryRunWritesNothing`, `TestParseTake`. Guard: `TestStore_UpdateAgentProfileIf`, `TestProfile_UpdateExpectedUpdatedAt`. Gates: the `TestFloor_SelfApprove*` rows for `profile add|edit|seed --apply`, `TestHumanOnlyCommandsRefuseInAgentRun`, `TestHumanOnlyRPCTable_MatchesHandlers`. CLI tests are named with the CLI commit.

### Decisions in this PR

- `questions` (the agent-questions setting) is a Human setting like `thinkingLevel`: not synced.
- A seat is untouched when none of its synced fields is customised; the default apply then writes its stale prompt fields.
- `smind profile rm` is not gated here (D6 names add and edit); `profile.delete` is not in the RPC table.
- The history table covers the five seats that still exist; seats an earlier pack seeded and later dropped are not synced.

## PR 2: daemon RPCs, the Settings -> Profiles banner and review dialog, the `smind doctor` line

Size M. UI change: validation contract `role-pack-sync.contract.md` (ADR-0045), light and dark frames at 1280 and 390 for every state. Mobile: nothing.

### Wire (the contract between the daemon and the web; both sides are built against this text)

`profile.rolePack.check` (no params; read-only; not run-scoped) returns
`{ "packVersion": 9, "seatsWithUpdates": 2, "seats": [ { "name": "lead", "id": 12, "status": "ok|missing|owned", "updatedAt": "<RFC3339>", "untouched": true, "hasUpdates": true, "fields": [ { "name": "instructions", "kind": "prompt|capability", "state": "current|stale|customised", "diff": "<unified diff of stored against the pack; empty when current>", "stored": "", "want": "" } ] } ] }`.
`stored` and `want` are filled for capability fields only (short values); a prompt field carries its `diff` only. `seats` always has one entry per pack seat in pack order, `fields` is empty unless `status` is `ok`, `untouched` and `hasUpdates` are the engine's `Untouched()` and `HasUpdates()`, and `seatsWithUpdates` counts seats with `hasUpdates`.

`profile.rolePack.apply` (Human-only) takes `{ "take": ["seat" | "seat.field", ...], "dryRun": false, "backupDir": "" }` (all optional; an empty `backupDir` is `<smind home>/backups/role-pack`) and returns `{ "backupDir": "<the timestamped directory made, or empty>", "dryRun": false, "results": [ { "seat": "lead", "id": 12, "fields": ["instructions"], "outcome": "updated|changed_meanwhile|dry_run|failed", "error": "" } ] }`. A malformed `take` is an invalid-params error naming it; nothing is written. It is the CLI's `--apply` over the same engine (`PlanApply` then `ApplyRoles`, written through `Registry.UpdateIf` with the `updatedAt` the check read): no logic is copied.

### Acceptance criteria

- AC8 `profile.rolePack.check` returns the shape above from the engine (`CheckRoles` plus `LineDiff`), changes nothing, and is not in `RunScopedMethods` (a run-scoped connection cannot call it).
- AC9 `profile.rolePack.apply` is in `permpolicy.HumanOnlyRPCs` (drift test green), is not run-scoped, is on the plugin session's refused list, and behaves as the CLI: stale prompt fields of untouched seats by default; a customised or capability field only through `take`; a backup of every profile to be changed is written BEFORE the first update; `dryRun` writes nothing; a profile edited since the check is `changed_meanwhile`; running it twice changes nothing the second time; a malformed `take` is refused before any write.
- AC10 `lineDiff` moves from `cmd/smind` into `internal/profiles` as `LineDiff` (one implementation); the CLI's `--diff` output is unchanged.
- AC11 `smind doctor` prints one line from the daemon's check: `[info] role pack: N seats have updates`, or `[ok] role pack: every seat is current` (`N` is one seat: `1 seat has updates`); nothing when the daemon is not running or the call fails; the line is in the `--json` findings; the exit status is unchanged.
- AC12 Settings -> Profiles shows the banner "Role prompts have updates for N seats" (singular for one) with a Review button when the check finds updates; no banner and no dialog entry when every seat is current (a quiet "Role prompts match pack vN" line says so); nothing when the check fails except an inline error line with Retry.
- AC13 The check runs when the Profiles section opens and nowhere else: no timer, no polling, no refetch on window focus, nothing in the background; Retry and the refresh after an apply are the only other calls.
- AC14 Review opens a dialog listing, per seat with updates, each stale or customised field with its state, a unified diff (lines marked by `+`, `-` and a space as well as colour; long diffs scroll inside the dialog), and capability fields marked as such with the stored and pack values. Nothing in the dialog changes a profile by itself.
- AC15 "Apply N updates" sends `profile.rolePack.apply` with no `take` (the default scope: stale prompt fields of untouched seats); each other field has its own Take button that sends `take: ["seat.field"]`; taking a capability field asks for a confirmation that names the old and new value first; after an apply the dialog shows each seat's outcome and the backup directory, and the banner and the profile list refresh.
- AC16 A daemon refusal or a failed seat is shown as received (not swallowed); `changed_meanwhile` says the profile changed since the check and refreshes the check; the dialog stays open on error.
- AC17 No change under `mobile/`; the product is "sMind" in user text, "your workspace", no other product names (`go test ./internal/hygiene/` passes); icons from `lib/icons.ts`; data through TanStack Query on the shared client.

- AC18 (frame review, round 2) The review dialog never shows a disabled-looking primary button: when nothing is applied by default (`defaultCount` is 0) there is no Apply button, only Close. With nothing applied yet the hint "Nothing to apply by default. Use Take on a field to change it." sits ABOVE the footer; after an apply the hint is gone.
- AC19 Errors read as sentences: the daemon's `<rpc method>: ` label (and a package label after it) is dropped, the first letter is capitalised and a full stop added, for the check error in the banner, the apply error in the dialog and a `failed` result line; the words in between stay the daemon's. One shared helper (`src/lib/daemon-sentence.ts`, used by the Browser section too: no copy).
- AC20 State badges (`Update available`, `Edited by you`, `capability`) never wrap inside themselves (no-wrap); the row wraps its badges as a group at 390.

### Named tests

Go: `TestProfileRolePack_CheckShape`, `TestProfileRolePack_CheckIsReadOnly`, `TestProfileRolePack_NotRunScoped`, `TestProfileRolePack_RPCHumanOnly` (the table and the plugin refusal; `TestHumanOnlyRPCTable_MatchesHandlers` extended), `TestProfileRolePack_ApplyUpdatesStalePromptFieldsOnly`, `TestProfileRolePack_ApplyTakeChangesACapabilityField`, `TestProfileRolePack_ApplyBackupWrittenBeforeTheUpdate`, `TestProfileRolePack_ApplyDryRunWritesNothing`, `TestProfileRolePack_ApplyChangedMeanwhile`, `TestProfileRolePack_ApplyIsIdempotent`, `TestProfileRolePack_ApplyRejectsAMalformedTake`, `TestLineDiff`, `TestDoctorShowsRolePack` (text, singular, all current, no daemon, `--json`).
Web (vitest): `rolepack_banner_hidden_when_every_seat_is_current`, `rolepack_banner_shows_the_count_and_a_review_button`, `rolepack_check_runs_once_when_the_section_opens_and_never_polls`, `rolepack_review_lists_stale_and_customised_fields_with_their_diff`, `rolepack_capability_fields_are_marked`, `rolepack_apply_sends_no_take`, `rolepack_take_sends_the_field`, `rolepack_capability_take_asks_first`, `rolepack_nothing_is_applied_without_a_click`, `rolepack_backup_path_and_outcomes_are_shown`, `rolepack_changed_meanwhile_is_explained`, `rolepack_check_error_offers_retry`, and for AC18 to AC20: `rolepack_no_apply_button_when_nothing_is_applied_by_default`, `rolepack_hint_is_above_the_footer_and_gone_after_an_apply`, `rolepack_errors_drop_the_method_label` (the two real strings `profile.rolePack.apply: backup directory /root/x: permission denied` and `profile.rolePack.check: database is locked`), `rolepack_state_badges_do_not_wrap`; and in `src/lib/daemon-sentence.test.ts`: `daemon_sentence_drops_the_method_and_package_labels`.
e2e (`web/e2e/specs/rolepack.spec.ts`, frames only with `ROLEPACK_FRAMES_DIR`): the real daemon's check and apply (a seeded stale seat, a real backup file), and the states a spec cannot reach on a real daemon by answering the RPCs through `page.routeWebSocket`.

### Frames (light and dark at 1280 and 390, every pair opened by the Lead)

`01-banner` (updates, section open), `02-dialog-diff` (stale prompt and a customised field, capability field marked), `03-after-apply` (outcomes and the backup path), `04-conflict` (`changed_meanwhile` and a daemon error), `05-all-current` (no banner, the quiet line), `06-check-error` (inline error with Retry), `07-capability-confirm` (the confirmation). Contact sheet and README under `~/.smind/dogfood/rolepack-pr2/`.

### Mutations (committed code; each must fail a named test)

- `profile.rolePack.apply` added to `RunScopedMethods` or dropped from `HumanOnlyRPCs` -> `TestProfileRolePack_RPCHumanOnly` / `TestProfileRolePack_NotRunScoped`.
- The handler updating without the backup (backup skipped or written after the update) -> `TestProfileRolePack_ApplyBackupWrittenBeforeTheUpdate`.
- The banner applying on its own (an apply call when the banner renders) -> `rolepack_nothing_is_applied_without_a_click`.
- A refetch interval or a timer on the check -> `rolepack_check_runs_once_when_the_section_opens_and_never_polls`.
- The handler writing a capability field without a take -> `TestProfileRolePack_ApplyUpdatesStalePromptFieldsOnly`.
- Capability take without the confirmation -> `rolepack_capability_take_asks_first`.
- The doctor line printing when the daemon is unreachable (an empty line or a fake count) -> `TestDoctorShowsRolePack`.

### Decisions in PR 2

- The diff is computed by the daemon (`LineDiff`), so the web renders text and the engine stays the one place that decides.
- Capability fields get a confirmation in the web even though a Take is already an explicit click: the change widens or narrows what a seat may do.
- `profile.rolePack.check` is a read, but it carries role prompt text, so it is kept off `RunScopedMethods` like the apply.
- No mobile surface, no daemon event for pack drift (the check is read on demand, ADR-0058 D4).

## Progress

Engine, history, store/registry/RPC guard and the human-only gates are built and tested; the CLI (`--check`, `--diff`, `--apply`) was written by a GLM peer and reviewed by the Lead.

## Validation

AC1-AC7 are covered by the named tests in `internal/profiles/rolesync_test.go`, `cmd/smind/profile_rolesync_test.go`, `internal/permpolicy/humanonly_test.go` and `internal/wsapi/profile_test.go`.
Mutation checks on committed code killed every engine, guard and CLI mutation except two, which survive on purpose:
the `updated_at` clause of the store's conditional UPDATE (the pre-check already rejects every reproducible stale write, so the clause only covers the race between check and UPDATE),
and the CLI's `expectedUpdatedAt` param (no test can interleave a write between the CLI's list and update).
Known decision: an empty stored `peer-sonnet.instructions` is the first release's value, so it classifies as stale; a Human who deliberately clears it is overwritten by `--apply` (after a backup, and `--check` shows it first).

## PR 2: progress and validation

Built: the daemon RPCs (`profiles.LineDiff`, `profile.rolePack.check` and `.apply`, the human-only and plugin gates, the method golden), the `smind doctor` line, the demo-daemon rules, the web banner, review dialog, hooks and test client, and `web/e2e/specs/rolepack.spec.ts`. Staffing: the two code peers were launched on GLM first; both runs ended on the GLM 5-hour window (`plan_window_exhausted`) after about five minutes. The peers' branch reflogs show two commits made during those GLM runs, before any peer-sonnet chat existed (the `LineDiff` refactor at 20:25 and the web hooks, wire types and test client at 20:27, local time); peer-sonnet, started at 20:29 in new chats on the same worktrees, wrote every other peer commit (the daemon RPCs, the gates, the doctor line, the banner and the review dialog with their tests), and the e2e spec and frames peer was peer-sonnet from its start. The Lead wrote the integration fixes listed below and reviewed and integrated every diff (the split is in the PR body).

- Validator (ADR-0045): validation 30 on commit `71dd13056`, contract 37, verdict `pass`, `mergeReady`: all 26 named tests, all 8 mutations (M1 to M8 each fail their named test and were restored), commands C1 to C4. One P3 finding: `profile.rolePack.check` is also on the plugin sessions' refused list (extra, intended, untested).
- AC8 to AC11 and AC17: the Go tests named above, `go test ./internal/profiles ./internal/wsapi ./internal/permpolicy ./cmd/smind`, `sh scripts/verify.sh` PASS (build, `task test`, `task lint`). The demo coverage test (`src/demo/demo-coverage.test.ts`) needed two rules (check: nothing; apply: refused) found by `verify.sh`.
- AC12 to AC16: the 13 vitest cases in `role-pack-review.test.tsx`, the e2e spec (4 projects, 28 cases), and the full e2e matrix (`E2E_FULL=1 --grep-invert @flaky`, 325 passed, 43 skipped).
- Frames: light and dark at 1280 and 390 for every state (banner, dialog with diff, after apply with the backup path, conflict and a refused apply, all current, check error, capability confirm) from the e2e harness, plus real-daemon frames from a dogfood daemon with a really stale seeded `lead` (an older released text) and an edited `validator`; every pair opened by the Lead. Findings from the frames, fixed: the apply result named no seat, long diff lines broke mid-word, and the apply error and result sat inside the scrolling body (clipped at 390).
- Mutations run by the Lead on committed code (before the validator): run-scoped apply, apply missing from the human-only table, apply without backup, a capability field written without a take, the banner applying on its own, a refetch interval on the check, a capability take without its confirmation, the doctor line printing after a failed call.
- Integration fixes by the Lead (not peers): the apply handler reports a profile list error, the result line names the seat and diff lines wrap at words, the result and error sit above the dialog footer, the demo rules, the e2e assertion for the seat-named result line.
- Not verified: the native desktop shell and a real phone (mobile has no surface by decision); the review dialog against a daemon with a stale CAPABILITY field (only stubbed: no released capability value differs from the current one, so a real daemon cannot show one).

### PR 2 round 2 (Supervisor frame review): AC18 to AC20

- Fixes (web only): no Apply button when nothing is applied by default (the hint sits above the footer and is gone after an apply), error text as sentences through one shared `daemonSentence` (`src/lib/daemon-sentence.ts`, the Browser section re-exports it), state badges that never wrap (the row wraps the badge group). Written by a GLM peer (peer-glm, task 685, started after the GLM window reopened at 22:17; branch reflog: three commits at 22:20 to 22:21); reviewed and integrated by the Lead, who also updated the e2e assertions to the sentence form and added the `08-nothing-by-default` frame.
- Validator: validation 31 on commit `2997d97b0`, contract 38 (version 2: AC18 to AC20 and mutation M9), verdict `pass`, `mergeReady`: all named tests (17 vitest, 11 wsapi, profiles, cmd/smind, hygiene), all 9 mutations (M1 to M9) each failing its named test and restored, commands C1 to C4. Two P3 notes: `profile.rolePack.apply` takes a caller-supplied `backupDir` (a human-only RPC; its trust boundary is untested), and mutation M4 is reached through the engine's `PlanApply` (the handler has no switch of its own).
- Mutation I ran on committed code: the check error showing the raw daemon message fails `rolepack_errors_drop_the_method_label`; an always-rendered Apply button fails `rolepack_no_apply_button_when_nothing_is_applied_by_default`; removing `whitespace-nowrap` fails `rolepack_state_badges_do_not_wrap`.
- `sh scripts/verify.sh` PASS on the head before the validator; e2e `rolepack.spec.ts` 32 passed (4 projects). Frames re-shot (02, 03, 04 refused, 06 at light and dark, 1280 and 390, plus the new 08 and the real-daemon set); every pair opened.
