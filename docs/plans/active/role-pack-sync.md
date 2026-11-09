# Role-pack sync for existing installs (ADR-0058)

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

## Progress

Engine, history, store/registry/RPC guard and the human-only gates are built and tested; the CLI is next.

## Validation
