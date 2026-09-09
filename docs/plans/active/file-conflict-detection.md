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

- **Mechanism: conditional write (the spec's recommended option).**
  `file.read` now returns `{content, mtime}`; `file.write` accepts an
  optional `expectedMtime` and returns `{mtime}` (chainable into the
  next save without a re-read). No polling: drift is detected at the
  exact moment it matters (save time), with editor-grade
  rejected-if-changed semantics. No `file.stat` was added.
- **Typed wire errors.** `internal/files` gained `ConflictError`
  (RPCCode "conflict" / "conflict_deleted" when the file is gone) and
  `MissingError` ("not_found" on `file.read` of a missing file).
  `wsapi`'s `rpcError` gained an optional `code` field, populated via
  a small `rpcCoder` interface unwrapping chain in `codedError` —
  clients branch on error kind without string-matching. The UI's
  `RpcError` now carries `code`.
- **Deleted-file semantics.** `expectedMtime` set + file gone →
  `conflict_deleted`, nothing written (no silent resurrection);
  `expectedMtime` omitted + file gone → today's create-it behavior
  (that's the Overwrite action on the deleted banner).
- **run.status probe = re-read and compare mtimes** (read-and-compare,
  not file.stat — mtime already rides file.read, so no new RPC). A
  terminal `run.status` for the viewed task re-issues `file.read` only
  when the buffer is dirty AND an mtime is held; differing mtime →
  "changed" banner, failed read → "deleted" banner. Purely advisory
  (a lost race just falls back to save-time detection) and never
  touches the buffer — no auto-reload. Reuses the diff pane's optional
  `events` prop pattern (`useDaemonEvents`), wired in App.tsx's tab
  renderer.
- **Focus/tab re-check: omitted.** The run.status probe covers the
  highest-risk moment (agent finished); focus/visibilitychange would
  fire on ordinary tab switches with no agent involvement, and the
  save-time conditional write is the authoritative backstop anyway.
- **Mtime granularity.** `DateTime` (RFC 3339Nano over the wire)
  preserves whatever precision the filesystem gives; on coarse-mtime
  filesystems a rewrite within the same timestamp tick can slip
  through — accepted (that's standard editor behavior), and the
  run.status probe would have re-read fresh content anyway.

## Progress

- [x] Backend mechanism (conditional write) + tests
- [x] Conflict banner UI (changed/deleted states, Reload/Overwrite)
- [x] run.status-triggered probe
- [x] UI tests
- [x] Verification (both chains)

## Validation

- AC1/AC2 (conditional write): Go unit tests
  `internal/files/conflict_test.go` (mtime round-trip, matching
  expectedMtime succeeds + chains, stale → typed `*ConflictError`
  with disk unchanged, deleted + expectedMtime →
  `conflict_deleted` with no resurrection, nil expectedMtime =
  last-write-wins incl. recreate) and wire tests
  `internal/wsapi/files_conflict_test.go` (error `code` reaches the
  wire: "conflict"/"conflict_deleted", content unchanged on disk,
  matching write succeeds with mtime result, omitted expectedMtime
  overwrites a changed file).
- AC3 (editor UI): `file-editor-pane.tsx` tracks read-time mtime
  (also after each clean save, via file.write's mtime result); a
  rejected conditional save maps the RpcError code to the banner's
  changed/deleted state with Reload/Overwrite buttons. Reload re-reads
  and replaces the buffer (deleted+Reload surfaces the read error
  state); Overwrite writes without expectedMtime (recreating a deleted
  file). Tests cover all four paths plus no-data-loss assertions
  (buffer and dirty marker survive the banner).
- AC4 (run.status probe): tests prove non-terminal statuses and other
  tasks' events don't probe, terminal status for the viewed task
  probes a dirty buffer (not a clean one), drift shows the banner
  before any `file.write`, and the probe never mutates the buffer.
- AC5 (no auto-action): banner clears only via explicit
  Reload/Overwrite clicks (or path change); no code path writes or
  reloads on its own — asserted by the probe test's
  zero-file.write/no-buffer-change checks.
- Verify chains: Go `go build ./... && gofmt -l . && go vet ./... &&
  go test -race ./...` + `task test` + `task lint` green; web
  `bunx tsc -b` + `bun run test` (10 files, 104 tests) + `task build`
  green, `internal/server/dist/.gitkeep` restored.
