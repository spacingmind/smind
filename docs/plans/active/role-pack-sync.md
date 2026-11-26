# Role-pack sync for existing installs (ADR-0058)

Open: PR 2 (daemon RPCs, web banner and review dialog, doctor line per ADR-0058 D4) is specified below and being built; nothing of it is validated yet.

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

### Named tests

Go: `TestProfileRolePack_CheckShape`, `TestProfileRolePack_CheckIsReadOnly`, `TestProfileRolePack_NotRunScoped`, `TestProfileRolePack_RPCHumanOnly` (the table and the plugin refusal; `TestHumanOnlyRPCTable_MatchesHandlers` extended), `TestProfileRolePack_ApplyUpdatesStalePromptFieldsOnly`, `TestProfileRolePack_ApplyTakeChangesACapabilityField`, `TestProfileRolePack_ApplyBackupWrittenBeforeTheUpdate`, `TestProfileRolePack_ApplyDryRunWritesNothing`, `TestProfileRolePack_ApplyChangedMeanwhile`, `TestProfileRolePack_ApplyIsIdempotent`, `TestProfileRolePack_ApplyRejectsAMalformedTake`, `TestLineDiff`, `TestDoctorShowsRolePack` (text, singular, all current, no daemon, `--json`).
Web (vitest): `rolepack_banner_hidden_when_every_seat_is_current`, `rolepack_banner_shows_the_count_and_a_review_button`, `rolepack_check_runs_once_when_the_section_opens_and_never_polls`, `rolepack_review_lists_stale_and_customised_fields_with_their_diff`, `rolepack_capability_fields_are_marked`, `rolepack_apply_sends_no_take`, `rolepack_take_sends_the_field`, `rolepack_capability_take_asks_first`, `rolepack_nothing_is_applied_without_a_click`, `rolepack_backup_path_and_outcomes_are_shown`, `rolepack_changed_meanwhile_is_explained`, `rolepack_check_error_offers_retry`.
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
