# Claude silently starts in Manual mode: detect it from the init frame (E1.2)

Status: active (spec, 2026-11-15). Size: S-M (4): scope 2 (taskrunner, doctor), PRs 2, risk 1 (a notice, no permission change), decisions 1, verification 1 (fake CLI plus one measured probe). Code starts from a fresh `develop`.

## Problem

smind passes the run's permission mode to the Claude CLI (`claudecode.WithPermissionMode(mode)`, `internal/taskrunner/runner.go`). When the CLI cannot honour a mode (auto mode disabled by a settings key, a plan or model that lacks it) it does not fail: it starts in `default` (Manual: asks before each tool) and says nothing. The run then stops at every tool call while smind's UI still shows the requested "Auto" mode, so a human sees an agent that "hangs" and a chip that lies. Nothing in smind reads the mode the CLI really chose.

## Measured on the real CLI (HAIKU ONLY probe, 2026-11-15)

`probe.sh` (kept locally, not committed): the real CLI 2.1.325, model pinned to the Haiku id, one turn, no tools, no session persistence, `--max-budget-usd 0.02` per call, cwd a fresh empty directory, the user's existing login (an isolated HOME has no credentials, so the HOME was not isolated; the project and the user's config were only read). Five calls, about USD 0.038 in total.

| requested `--permission-mode` | settings | `system`/`init` frame `permissionMode` | exit | stderr |
|---|---|---|---|---|
| `default` | none | `default` | 0 | none |
| `acceptEdits` | none | `acceptEdits` | 0 | none |
| `plan` | none | `plan` | 0 | none |
| `auto` | none (this account allows it) | `auto` | 0 | none |
| `auto` | `--settings '{"permissions":{"disableAutoMode":"disable"}}'` | **`default`** | 0 | none |

So: the init frame carries the real mode in a top-level `permissionMode` string, it matches the request when honoured, and the silent downgrade is reproducible and invisible apart from that field. `bypassPermissions` was not probed (it needs the dangerous-skip flag; the downgrade rule is the same, tested with the fake CLI only).

## Acceptance criteria

1. On the Claude `system`/`init` frame the runner reads `permissionMode` and compares it with the mode it requested for this run.
2. A request for an auto-approving mode (`AutoApproves` in the catalog: `auto`, `bypassPermissions`) that the CLI reports as anything else produces ONE session note on the run ("Claude started in <real> instead of <requested>; ...") naming the likely cause and the fix, and a `permission_mode_changed` event carrying the real mode, so the mode chip shows the truth. A mismatch on a non-auto mode produces the event and a shorter note.
3. A reported mode MORE permissive than requested (for example `bypassPermissions` when `default` was asked) produces the same note with a warning level and the event; smind never lowers or raises the mode itself.
4. A match, a missing field (an older CLI) and a malformed field produce nothing and never fail the run. Resumed turns (a second init on the same chat) note once per run, not per frame.
5. `smind doctor` gains a read-only check `claude.auto_mode` that reads the Claude settings files smind already inspects for retention (user, project of the working directory is not read; managed and `CLAUDE_CONFIG_DIR` roots) for `permissions.disableAutoMode` and `permissions.disableBypassPermissionsMode`, and reports each as a warning with the fix. It makes no model call and writes nothing.
6. The run's stored `permission_mode` is NOT rewritten by the note (the run requested what it requested); only the live event and the display change.
7. No secret, path or settings content is put in the note: it names the key (`permissions.disableAutoMode`), never the file's contents.

## Named tests

- `internal/taskrunner`: `TestInitFrameModeMismatchNotesOnce` (auto requested, fake CLI reports default: one note, one `permission_mode_changed` with `default`), `TestInitFrameModeMatchIsSilent`, `TestInitFrameModeMissingOrMalformedIsSilent`, `TestInitFrameMorePermissiveWarns`, `TestInitFrameModeNoteNeverRewritesStoredMode`.
- `internal/taskrunner/fakeclaude`: reports `permissionMode` on init (the requested one, or a downgraded one when `FAKECLAUDE_DOWNGRADE_MODE` is set); this is how the tests above drive it. A fake never replaces the probe: the probe table above is the evidence for the real field name and value.
- `internal/doctor`: `TestClaudeAutoModeDisabledWarns`, `TestClaudeAutoModeAbsentIsOK`, `TestClaudeAutoModeReadsManagedAndConfigDirRoots`, `TestClaudeAutoModeNeverWrites`.
- Web (only if the chip needs it): the existing `permission_mode_changed` handling is reused; a test that the chip shows the real mode after the event.
- Live check at the end: the same probe row (auto plus `disableAutoMode`) against a real run through an isolated daemon, Haiku pinned, expecting the note once.

## Decisions

- D1 (recommended): detect at init from `permissionMode`, never by inferring from behaviour (a stalled permission). It is exact, free and available before any tool call.
- D2 (recommended): notice, not repair. smind does not switch modes for the user; ADR-0019 keeps the mode the human's or the provider's.
- D3: doctor reads settings files only (the existing retention check's roots); it does not launch the CLI.
- Open for the Human: whether the note should also raise an inbox item (an unattended Auto run that is really Manual just waits). Recommendation: no, the existing permission inbox item already appears at the first tool call; the note explains why.

## Progress

- [x] Probe on the real CLI (Haiku only, five calls, about USD 0.038): the field and the downgrade are real.
- [ ] PR 1: runner detection, fake CLI, tests. PR 2: `smind doctor` check.

## Validation

(filled in per PR)
