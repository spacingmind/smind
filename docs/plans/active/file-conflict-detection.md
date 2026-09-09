# File conflict detection: agent rewrote a file the human has open

Open question 8 of `docs/research/dual-mode-ui.md`. Today an agent
overwriting a file that's open with unsaved edits in the editor is
silent — the human's next save clobbers the agent's work (or their own
reload does). Paseo's precedent: `conflict-alert` with
changed/deleted/checkFailed states and Reload/Overwrite actions.

## Acceptance Criteria

- The daemon can tell "what changed on disk since I read it": extend
  the file RPC surface minimally — either a `file.stat {taskId, path}`
  returning `{mtime, size}` (cheap probe the client polls when its
  buffer is dirty) or include mtime in `file.read`'s result and let
  saves detect drift via an `expectedMtime` param on `file.write`
  (conditional write — rejected-if-changed). Decide ONE mechanism in
  Decisions and justify; the conditional-write option is recommended
  (no polling, exact semantics, matches how editors do it) but either
  is acceptable.
- With the conditional-write option: `file.write {taskId, path,
  content, expectedMtime?}` — when expectedMtime is set and the file's
  current mtime differs, return a typed error (e.g. code "conflict"),
  do NOT write; when omitted, current behavior (last-write-wins).
- Editor UI (`file-editor-pane.tsx`): track the read-time mtime; on a
  conflicting save show an inline conflict banner with the two
  states Paseo established — **changed on disk** (actions: Reload
  (discard buffer, re-read), Overwrite (force save without
  expectedMtime)) and **deleted on disk** (actions: Reload shows an
  error state, Overwrite recreates the file). Also re-check on window
  focus or tab activation if cheap (decide; optional).
- A run.status terminal event for the viewed task re-probes open dirty
  buffers (the agent just finished — most likely conflict moment);
  if drift is detected show the banner BEFORE any save attempt
  (decide probe mechanism in Decisions — likely a lightweight
  file.stat or read-and-compare).
- No data loss without an explicit choice: both actions are explicit
  user clicks; nothing auto-reloads.

## Test Scenarios

- Go: conflict-path wire tests — write with stale expectedMtime →
  typed conflict error, file content unchanged; write with matching
  mtime succeeds; write without expectedMtime overwrites (current
  behavior); deleted-file case (decide behavior with expectedMtime set
  and file gone → conflict error).
- Web: file-editor-pane tests — dirty save conflict renders banner
  with Reload/Overwrite; Reload re-reads (buffer replaced); Overwrite
  force-writes and clears banner; deleted case renders its state;
  terminal run.status with drift shows banner pre-save.
- Both verify chains green; `.gitkeep` restored if wiped.

## Decisions

(To be filled: probe vs conditional write choice and why; mtime
 granularity caveats (filesystems with coarse mtime), deleted-file
 semantics, focus/tab re-check inclusion.)

## Progress

- [ ] Backend mechanism (stat or conditional write) + tests
- [ ] Conflict banner UI (changed/deleted states, Reload/Overwrite)
- [ ] run.status-triggered probe
- [ ] UI tests
- [ ] Verification (both chains)

## Validation

(Filled in as each Acceptance Criterion is confirmed.)
