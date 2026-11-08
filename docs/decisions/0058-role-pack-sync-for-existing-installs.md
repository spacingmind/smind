# 0058: Role-pack sync for existing installs — check, diff, back up, and update only what the Human did not change

## Status

**Proposed — 2026-11-09.** Extends ADR-0041 S4a (the role pack of seat profiles) and the `smind profile seed roles` command. It changes nothing about how a seat runs. Every choice under
[Decisions for the Human](#decisions-for-the-human) carries a recommendation; a veto changes only the PR named next to it. No code is written before the Human (or the Supervisor under the
Human's delegation) answers.

## Context

The role pack is code: `RoleSeats` in `internal/profiles/rolepack.go` builds five seat profiles (`peer-sonnet`, `lead`, `validator`, `steward`, `steward-standards`), each with its model, mode,
orchestration and child settings, notes and a long `Instructions` text that the daemon stores in the profile row. `SeedRoles` creates a seat only when no profile of that name exists, and
"a seat whose name already exists is left completely untouched" (its doc comment, pinned by `TestSeedRoles_EditSurvivesRerun` and `TestSeedRoles_ExistingSeatKeptUntouched`). That protects the
Human's edits, and it means every later improvement of a role prompt (a new rule in the Lead's text, a corrected handoff format) reaches new installs only: an existing install keeps the
text it was seeded with, until the Human copies the new text by hand. Fact summary:

| Fact | Where |
|---|---|
| Seats are built by `Build` closures; `Instructions` are package constants | `internal/profiles/rolepack.go` |
| Seeding is by name; an existing seat is `kept` and never read | `SeedRolesWith` |
| A profile row has no record of where it came from or what text it was seeded with | `agent_profiles` (`internal/store/schema.sql`) has no provenance column |
| An edit is a full-record replace through `Registry.Update` (`profile.update`), validated by the same rules as create | `internal/profiles/registry.go`, `internal/wsapi/handlers.go` |
| The CLI seeds through `profile.list` and `profile.create`, and refuses only when `SMIND_RUN_ID` is set | `cmd/smind/profile.go` (`cmdProfileSeed`) |
| The only profile editor is the web Settings -> Profiles section; mobile has none | `web/packages/ui/src/components/settings/profiles-section.tsx` |
| Plugin-contributed profiles are read-only and never stored | `store.AgentProfile.Owner`, `Registry.refuseOwned` |

So there are three kinds of profile named like a seat: **untouched** (still exactly what some released pack version seeded), **customised** (the Human edited it), and **unknown** (we cannot
tell). Anything that updates stored prompts must be safe for all three, because the cost of overwriting a customised prompt is a person's work and the cost of leaving an untouched one stale is
only a worse prompt.

## Decision

A **sync** operation over the existing seat names, read-only by default, never automatic, with a backup before any write.

### 1. Telling untouched from customised: the pack remembers its own history

The pack ships, next to the current seat text, the **SHA-256 of every released version of each synced field** of each seat (`rolepack_history.go`, a table built from the git history of the
file; one new line per release). A stored field is **stale** when its hash is in the seat's history but is not the current one, **current** when it equals the current text, **customised** when it
matches nothing. This works for installs seeded long ago (no new column, no migration, nothing to back-fill) and survives a restored database. A drift test fails the build when the current text
of a synced field is missing from its history table, so a release cannot ship a text change without recording it. Text is compared after normalising line endings and trailing whitespace only.

### 2. What is synced and what is not

- **Prompt fields** (`Instructions`, `Notes`, `Skills`): synced.
- **Capability fields** (`PermissionMode`, `OrchestrationTools`, `ChildPolicy`, `OnRateLimit`, `Model`): shown in the diff, flagged as capability changes, and **never changed without an explicit
  per-field opt-in**, even on an untouched profile: a pack release must not loosen what a seat may do behind the Human's back.
- **The Human's own settings** (`Provider`, `ThinkingLevel`, `AutoAccept`, `FallbackProfileID`, `ChildProfileIDs`, the name and id): never read as drift and never written.

### 3. Operations (CLI first)

`smind profile seed roles` keeps its meaning (create the missing seats, leave the rest) and gains:

- `--check`: for each seat, one line per synced field with its state (`current`, `stale: pack vN`, `customised`) and an overall verdict; exit 0 when nothing needs the Human, 10 when updates
  are available. Reads only.
- `--diff [seat[.field]]`: a unified diff of stored against current for each stale or customised field, with capability fields marked.
- `--apply [--backup-dir <dir>] [--take seat[.field]]... [--dry-run]`: writes the **stale prompt fields of untouched profiles** only. A customised field or a capability field changes only when
  named with `--take`. Before the first write the command saves every profile it is about to change, in full, to `<backup-dir>/<timestamp>/<seat>-<id>.json` (mode 0600; default
  `<smind home>/backups/role-pack/`); if the backup cannot be written nothing is changed. Each profile is updated with `Registry.Update` from the row the check read, guarded by its
  `updatedAt`: a profile edited since the check is skipped and reported `changed_meanwhile`.
- `--apply` is idempotent: run twice, the second run reports every field `current`.
- Human-only: `--apply` refuses inside an agent run (`SMIND_AGENT_RUN` or `SMIND_RUN_ID`) and is added to the human-only command table of the `no-self-approve` floor rule, because it rewrites
  the instructions and permission mode an agent runs under. The daemon side (below) is not run-scoped and refuses a run-scoped caller.

### 4. Web (second PR)

Settings -> Profiles shows a banner when the check finds updates: "Role prompts have updates for N seats" with a Review button that opens the per-field diff, an Apply button for the
untouched profiles' prompt fields, and per-field Take for the rest, each with the backup path shown after. The check runs when the Settings section opens, never in the background; nothing is
applied without a click. `smind doctor` prints one line (`role pack: N seats have updates`). Mobile has no profile editor and gets nothing.

### 5. Versioning

The pack gets an integer `PackVersion`, bumped with any seat text, shown in `--check` ("pack v7") and in the backup file name. It is only a label for humans and for the history table; the
hashes decide staleness. A seat added to the pack is created by the existing seeding, as `steward-standards` was.

### 6. Failure modes

| Failure | Result |
|---|---|
| Backup directory unwritable or full | nothing is changed; error names the directory |
| Profile edited between check and apply | that profile is skipped (`changed_meanwhile`), the rest proceed; re-run completes |
| Validation refuses the new text (a skill name that no longer resolves) | that profile reports the validation error, others proceed, the backup stays |
| Seat renamed by the Human | not found by name: reported `missing`, `seed roles` would create a fresh one, as today |
| Plugin-owned profile with a seat's name | read-only: reported `owned`, never written |
| A dev build's unreleased text is in a profile | no hash match: `customised`, left alone |
| Daemon restarts mid-apply | profiles are updated one by one; the next `--check` shows what is left |
| Stored text differs only by whitespace | normalised, counts as `current` or `stale`, never `customised` |

### 7. Tests (named, written with the PRs)

Engine: `TestRolePackSync_ClassifiesCurrentStaleCustomised`, `TestRolePackSync_HistoryContainsTheCurrentText` (the drift guard), `TestRolePackSync_ApplyUpdatesOnlyStalePromptFields`,
`TestRolePackSync_CustomisedFieldNeverTouchedWithoutTake`, `TestRolePackSync_CapabilityFieldNeedsExplicitTake`, `TestRolePackSync_UserSettingsNeverWritten`, `TestRolePackSync_BackupBeforeFirstWrite`,
`TestRolePackSync_NoBackupNoWrite`, `TestRolePackSync_ChangedMeanwhileIsSkipped`, `TestRolePackSync_ApplyIsIdempotent`, `TestRolePackSync_OwnedProfileSkipped`, `TestRolePackSync_WhitespaceOnlyDifference`.
CLI: `TestProfileSeedRoles_CheckExitCodes`, `TestProfileSeedRoles_ApplyRefusesInAgentRun`, plus the floor table test for `profile seed --apply`. Wire: `TestProfileRolePack_RPCHumanOnly`. Web: the banner,
review dialog and the apply flow, with light and dark frames. Mutations: apply writes a customised field; apply writes a capability field without take; the backup is written after the update; the
history guard removed; the `updatedAt` guard removed.

### 8. PR split

1. Engine, history table and drift guard, `--check`, `--diff`, `--apply` with backup, docs, the floor/CLI human-only entry.
2. Daemon RPCs (`profile.rolePack.check`, `profile.rolePack.apply`), the web banner and review dialog, `smind doctor` line, frames.
3. Optional: `--restore <backup>`.

## Options considered

- **Overwrite on every upgrade.** Rejected: destroys edits.
- **A stored hash of the seeded text per profile** (written at seed time). Works from now on, but existing installs, the ones this record is for, have no hash, so they could not be classified; the history table covers them and needs no migration. A stored hash can be added later for provenance if wanted.
- **A pack version stamp in the profile row.** Cheap, but a stamp says what was seeded, not whether it was edited since.
- **Auto-sync at daemon start.** Rejected: an unattended rewrite of role prompts and a possible change of what seats may do.

## Consequences

- Improvements to role prompts reach existing installs through one explicit, reversible step; edits are never lost.
- Each pack release adds one line per changed field to the history table, enforced by the drift guard.
- Capability changes in a pack release are visible and need a deliberate choice, so a prompt update can never silently widen a seat.
- Nothing runs in the background and no profile changes without the Human's command or click.

## What we are NOT doing

- Not rewriting a customised field by default, or any capability field without an explicit take.
- Not updating on daemon start or on a timer.
- Not syncing the Human's own profiles (those whose names are not the pack's).
- Not adding provenance columns to `agent_profiles` in the first PRs.

## Decisions for the Human

- **D1: how untouched is detected.** Recommendation: the history table of released hashes (no migration, covers existing installs). Alternative: a stored hash per profile from now on (cannot classify today's installs).
  Veto changes PR 1.
- **D2: default scope of `--apply`.** Recommendation: prompt fields (`Instructions`, `Notes`, `Skills`) of untouched profiles only; capability fields and customised fields only with `--take`.
  Alternative: also stale capability fields on untouched profiles.
- **D3: backup.** Recommendation: always, before the first write, in the smind home by default, never skippable. Alternative: opt-in `--backup-dir`.
- **D4: surfaces.** Recommendation: CLI in PR 1, web banner and review dialog plus a `doctor` line in PR 2, nothing on mobile. Alternative: CLI only.
- **D5: customised profiles.** Recommendation: never changed without `--take seat[.field]`; the diff is always available. Alternative: also offer "take all" for customised fields in the web dialog (one confirm, with the backup).
