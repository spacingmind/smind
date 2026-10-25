# `smind profile edit` + web profile editor contract/failover fields (ADR-0041 S1w)

**Status 2026-10-25 (plans hygiene): PARTIAL.** PR-CLI #574 (AC-C1..C6 confirmed with a real isolated-daemon run) and PR-WEB #576 (AC-W1..W3, W5 confirmed by tests) are merged. Open: AC-W4, the light/dark screenshot and transition-frame pass of the editor, which has no recorded evidence.

Size: L (6) — scope 2, PRs 2, risk 1, decisions 0, verify 1

Two PRs from `origin/develop`, not stacked: **PR-CLI** (`smind profile edit`), **PR-WEB** (profile editor fields).

## Background

`profile.update` is a full replace on the wire (`internal/wsapi/handlers.go` `handleProfileUpdate`).
The web editor today sends only name/provider/mode/autoAccept/thinking/model/notes, so saving
any profile from Settings **silently clears** its `onRateLimit`, `fallbackProfileId`, seat
contract fields. PR-WEB fixes that data-loss (AC-W1) as well as adding fields.
No daemon RPC lists bundled skills, so the web uses a free-text comma list validated by the daemon.
Validation lives in `internal/profiles/registry.go` (`validateContract`, `validateFailover`); both
clients surface its error text verbatim.

## Acceptance criteria

### CLI
- AC-C1 `smind profile edit <id> [flags]` accepts every `profile add` flag except name/provider positionals; adds `--name`/`--provider` to rename/retarget.
- AC-C2 It does `profile.list` (find by id), applies only flags that were given, then `profile.update` with the full merged profile. Unknown id → error, exit 1.
- AC-C3 Clearing: `--model=`, `--thinking-level=`, `--notes=`, `--mode=`, `--instructions=`, `--skills=`, `--child-profiles=` clear the field; `--fallback-profile=0` clears the fallback; `--orchestration-tools=unmanaged` returns it to unmanaged (nil); `--on-rate-limit=none`.
- AC-C4 Daemon validation errors are printed (`profile edit: <daemon text>`), exit 1, and the stored profile is unchanged.
- AC-C5 No flags → usage error, exit 2. `--instructions` with `--instructions-file` → exit 2.
- AC-C6 Prints the same row as `profile add`.

### Web
- AC-W1 Saving the editor never drops fields it does not show: update payload is built from the existing profile (onRateLimit, fallbackProfileId, contract fields) overlaid with form values.
- AC-W2 Fields: orchestration tools (Unmanaged/On/Off), instructions textarea (16 KiB counter/cap), skills (comma list), child policy (None/Leaf/Profiles) + child profiles multi-select (only when policy = Profiles), on-rate-limit (None/Wait/Fallback) + fallback profile select (only when Fallback).
- AC-W3 Daemon error from create/update shows inline in the form (FormError) and the form stays open with edits kept.
- AC-W4 Light and dark screenshots incl. transition frames: editor opening, fallback field appearing, child-profiles appearing, inline error appearing. Compact, clean.
- AC-W5 `types.ts` additions minimal and additive (`OnRateLimit?`, `FallbackProfileID?`).

## Test scenarios (names)

CLI (`cmd/smind/profile_edit_test.go`):
- TestProfileEditChangesOnlyNamedFields
- TestProfileEditClearsModelAndFallback
- TestProfileEditOrchestrationToolsUnmanaged
- TestProfileEditSurfacesDaemonValidationError
- TestProfileEditUnknownID
- TestProfileEditNoFlagsUsage
- TestProfileEditInstructionsFile

Web (`profiles-section.test.tsx`, `use-profiles.test.ts`):
- keeps failover and contract fields when saving an unrelated edit
- sends orchestrationTools null/true/false per the select
- shows child profiles only for childPolicy profiles
- shows the fallback profile select only for onRateLimit fallback
- renders a daemon validation error inline and keeps the form open
- instructions over 16 KiB blocks save with a message

## Decisions
- Skills: free-text comma list (no skills RPC exists). Daemon validates.
- Edit semantics: get → modify → full update (no new RPC; no daemon change).

## Progress
- CLI (task 48, peer-glm): 3 commits, ACCEPTED (PR-CLI).
- Web (task 49, peer-sonnet): see PR-WEB.

## Validation — CLI
- AC-C1..C6 confirmed. `go test ./cmd/smind -run Profile -count=1` ok; mutation (drop the `--fallback-profile=0` clear) fails TestProfileEditClearsModelAndFallback.
- Real run against an isolated daemon (throwaway SMIND_HOME, port 47391): notes cleared, fallback set → `fallback→2`; `--child-policy=profiles` on a no-tools profile → daemon text `a profile without orchestration tools cannot launch children`, exit 1, profile unchanged; clear model/fallback/skills + `--orchestration-tools=unmanaged` → ROLE `-`; unknown id exit 1; no flags exit 2.

## Validation — Web
Never filled in by PR-WEB. Hygiene pass 2026-10-25: PR-WEB merged as #576 (PR-CLI as #574). AC-W1, AC-W2, AC-W3 and AC-W5 are covered by tests present on `origin/develop` in
`web/packages/ui/src/components/settings/profiles-section.test.tsx` (`keeps failover and contract fields when saving an unrelated edit`, `sends orchestrationTools null/true/false per the select`,
`shows child profiles only for childPolicy profiles`, `shows the fallback profile select only for onRateLimit fallback`, `renders a daemon validation error inline and keeps the form open`,
`instructions over 16 KiB blocks save with a message`). AC-W4 (light and dark screenshots with transition frames) has no recorded evidence: open.
