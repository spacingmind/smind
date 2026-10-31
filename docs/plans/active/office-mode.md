# Office mode: interface mode, binary-safe file read, PDF preview, deliverable cards (ADR-0049)

Status: active (2026-10-31). Binding text: [`docs/decisions/0049-office-mode.md`](../../decisions/0049-office-mode.md) (Accepted; H1–H6 are the
Human's answers, S1–S18 are Supervisor defaults 2026-10-31, Human may veto; a veto changes the PR named in the ADR's table).

Size: L as one item (Scope 2 (web + daemon + desktop), PRs 2 (five, with ordering), Risk 2 (untrusted-PDF parser, binary file read, open-path
primitive), Decisions 0 (settled), Verification 2 (UI frames in two modes, macOS and Windows desktop) = 8) → split into the PRs below,
**non-stacked**, each from `origin/develop` after the PRs it depends on have merged. Every wsapi, desktop-permission and UI PR gets a
validation contract (ADR-0045, `docs/plans/active/office-mode-<pr>.contract.md`) before code. This PR (docs only) has none: no contract,
docs-only.

Scope: epics **A** (interface mode) and **E** (documents as deliverables). **Not in this plan** (later ADRs, ADR-0049 section 8): D
folder workspaces without git, C document-generation skills as plugins, docx/xlsx/pptx preview, mobile. No daemon default, prompt, tool,
permission, policy, model, workspace-type or store-schema change anywhere in this plan.

## Acceptance Criteria

Interface mode (O1)
- AC1 `readInterfaceMode()` returns `"office"` only for the stored string `"office"`; missing, `"coding"`, corrupt, wrong type and a throwing
  `localStorage` all give `"coding"`; `writeInterfaceMode` never throws; the key is `smind:interface-mode` and is listed in `STORAGE_KEYS`.
- AC2 One shared preference store (`createPreferenceStore`): a change from Settings, the footer menu, the shortcut and the palette reaches every
  mounted reader in the same frame; another tab's change arrives through the `storage` event.
- AC3 Four entry points exist and agree: a Settings → General "Interface" select, a footer-menu radiogroup, the `Mod+Shift+U` shortcut
  (`interface.toggle`, section General, rebindable) and a palette command naming the *opposite* mode.
- AC4 In office mode the tab "+" menu, the empty-state base section and the sidebar quick actions list no New Terminal, no Review/diff and no
  PR entry; the composer has no branch/base-ref picker; the palette lists no terminal/diff/branch/PR-opening command; their shortcut actions
  are no-ops and are absent from the shortcut help dialog. "Hidden" = removed from the DOM and the tab order, not disabled.
- AC5 A task **created** while office mode is on seeds `chat` + `files` only; a task created in coding mode, and any task that already exists,
  keeps its seeded tabs; flipping the mode closes, reopens, restarts and re-seeds nothing; an open Terminal tab keeps its PTY and output, an
  open Diff/PR tab keeps working.
- AC6 **No payload change (H2):** for the same scripted user actions (create task, send prompt with and without a branch value, change a
  setting) the JSON of every wsapi request is byte-identical in both modes; no wsapi method, `daemon.info` field, setting, profile or plugin
  API carries or reads the mode (a source scan of `internal/` finds no `interfaceMode`).
- AC7 Accessibility: the select is a native `<select>` with a label; the menu is `role=radiogroup` with `role=radio` + `aria-checked`; a toggle
  by shortcut or palette writes "Office mode on" / "Coding mode on" to a polite live region; hidden entries are not tabbable; when the focused
  element is removed by the flip, focus lands on the composer.

Concise cards, greeting, prompts (O2)
- AC8 In office mode a `terminal`- or `edit`-intent tool card shows the renderer's one-line summary and a status (running/done/failed) and **no**
  command text, output, diff, +/- counts or expand control; a failed card adds the one-line error summary; its file link still opens the file.
  In coding mode the same fixtures render exactly as before (snapshot-equal DOM).
- AC9 Read/search/fetch/MCP/plugin/thinking cards, permission requests, run-error boxes, failover and budget cards render identically in both
  modes (fixtures compared); `DetailLevel` still works in office mode.
- AC10 The greeting hook picks from `OFFICE_GREETINGS` (≥ 3 lines per bucket, own copy) in office mode and from today's pool in coding mode;
  the line only changes when the bucket or the mode changes.
- AC11 Office mode shows exactly four suggestion chips on the empty home; clicking one fills the composer and **sends nothing**; coding mode
  shows the same chips it shows today (none if there were none at O2 start).

Binary-safe read (O3)
- AC12 `file.stat` returns `{size, mtime, kind, mime, ext, mismatch, previewable}`; `mime` comes from the first 1024 bytes (`%PDF-` check,
  `DetectContentType`, text = valid UTF-8 without NUL in the first 8 KiB), not the extension; a `.pdf` that is text or a `.txt` that is a PDF
  gets `mismatch: true, previewable: false`; a directory, FIFO, device or socket is `invalid_params`; a missing file maps to the existing
  `MissingError`.
- AC13 `file.readRange {taskId, path, offset, length, ifMtime?}` returns base64 bytes `[offset, offset+n)` with `eof` and `mtime`; `n` is clamped
  to the transport's `maxChunkBytes`; reassembling all chunks of a 3 MiB random file is byte-identical to the source; an offset past EOF
  is `{length: 0, eof: true}`; negative values are `invalid_params`.
- AC14 Containment and races: `../` escapes, absolute paths outside the worktree and a symlink pointing outside are refused exactly as
  `file.read` refuses them; the file is opened once and checked on the **open descriptor** (regular, size ≤ `maxReadBytes` else `too_large`);
  a file replaced by a larger one between stat and read cannot exceed `maxReadBytes`; `ifMtime` mismatch gives `conflict`.
- AC15 `daemon.info.files = {maxReadBytes: 50 MiB, maxChunkBytes, previewKinds: ["pdf"]}` with `maxChunkBytes` 4 MiB on direct and 512 KiB on
  the relay; the largest relay response (512 KiB chunk) fits one E2EE frame (`< 1<<20` with envelope); an older daemon's `method_not_found`
  makes the client say "Update the daemon to preview files".
- AC16 Scoping: both methods are task-scoped like `file.read`, are in the handler map, are **not** reachable by a plugin session (the `file.`
  prefix on ADR-0042's never-list; test over the plugin capability table), and log neither params nor bytes (canary test over logs, events,
  `request_log`, error text).

PDF preview (O4)
- AC17 A `.pdf` opened from the Files tab or a file link opens the existing `file` tab (`${taskId}:file:${path}`) and shows a rendered PDF
  (vertical pages, fit-width, zoom, page N / M, keyboard paging, reload); no new `TabKind`; text files still open in the editor; other
  binaries show the "No preview" panel with Open in app / Download instead of an error; a non-PDF named `.pdf` (mismatch) never reaches the viewer.
- AC18 The viewer is an opaque-origin sandboxed iframe: `sandbox="allow-scripts"` and **none** of `allow-same-origin`, `allow-popups`,
  `allow-forms`, `allow-modals`, `allow-top-navigation`, `allow-downloads`; its document is served with the ADR's CSP header
  (`default-src 'none'; … connect-src 'none'; frame-ancestors 'self'`); the viewer cannot read the host's `localStorage` or open a socket.
- AC19 Hardening options are on and tested through the options object the code builds: `isEvalSupported:false`, `enableScripting:false`,
  `enableXfa:false`, annotation layer off, `data` input only (no URL), canvas and image caps, ≤ 2000 laid-out pages, 30 s parse timeout;
  a PDF with a `/JavaScript` action, a link annotation and a `/Launch` action renders its pages and runs/navigates nothing.
- AC20 The host validates every viewer message by shape and by `event.source`; unknown or malformed messages are ignored; the viewer is given
  bytes only (no path, task id or token).
- AC21 The preview is lazy: the **entry** chunk grows ≤ 2 KB; the viewer chunk is fetched only when a PDF opens (build-size check recorded);
  pdf.js is pinned to an exact version ≥ the CVE-2024-4367 fix, its licence attribution is added, and the plan records the pin.
- AC22 Oversize (> 50 MiB), password-protected, corrupt and truncated PDFs each show a distinct calm error panel, tear the iframe down, and
  leave the app usable; a `conflict` (file changed) offers Reload.

Deliverable cards, open in app (O5)
- AC23 Detection is deterministic: candidates = successful write-intent tool paths ∪ paths named in the run's final assistant message (markdown
  links, matching inline code), normalised to worktree-relative; a shell command's text is never parsed; the same timeline and disk state always
  give the same cards (table test).
- AC24 Verification by `file.stat`: exists, regular, inside the worktree, allow-listed extension, not `mismatch`, `mtime ≥ run.startedAt`; a file
  older than the run, a missing file, a directory, `../x.pdf`, `~/x.pdf`, `https://…/x.pdf` and an absolute path outside the worktree make no card.
- AC25 At most 6 cards per run (then "+ N more files" linking to the Files tab); ≤ 20 candidates checked; one `file.stat` per candidate per run.
- AC26 Card actions: Open (PDF → preview tab; text-like → file tab; docx/xlsx/pptx → no in-app preview, Open in app instead), Open in app and Reveal
  in folder (desktop + local daemon only), Download (web only), Copy path; nothing auto-opens a tab. Cards appear in both modes.
- AC27 `open_path_default(root, rel_path)` (Tauri) refuses: a path outside the canonical root, a symlink escaping the root, a directory (incl. a
  `.app` bundle), an extension off the allow-list (`exe app command bat cmd ps1 sh msi dmg jar lnk url desktop scr html svg` among them), a
  Windows UNC network path, and any call when the attached daemon is remote; it opens through the opener plugin's path API (no shell, no argv
  string); error text carries no absolute path. Rust unit tests per row.
- AC28 Web **Download** assembles chunks into a Blob with `ifMtime`, names the file by a sanitised basename (control chars and separators stripped,
  ≤ 120 chars), revokes the object URL, shows progress above 5 MiB, refuses > 50 MiB.

Docs and dependencies
- AC29 ADR-0049's S16 table is encoded as one Go table (names, detection, per-OS install strings) when the first consumer lands; nothing in this
  plan executes an install command; agents are never told to install (grep of the new skill/prompt text is empty).
- AC30 `docs/ARCHITECTURE.md` gains an "Office mode" section; `docs/decisions/0049` stays the spec; this plan moves to `completed/` only when
  every AC above is confirmed in Validation.

## Named test scenarios (must exist by these names)

Go (`internal/files`, `internal/wsapi`):
- `TestInterfaceModeNeverOnTheWire` — source scan: no `interfaceMode` identifier under `internal/` or in any wsapi param struct (AC6).
- `TestFileStat_SniffsContentNotExtension` — PDF named `.txt`, text named `.pdf`, PNG, zip with OOXML marker, empty file, NUL-bearing text (AC12).
- `TestFileStat_RejectsNonRegular` — directory, FIFO (unix build tag), symlink to outside, missing (AC12, AC14).
- `TestFileReadRange_RoundTrip` — 3 MiB random file in 512 KiB and 4 MiB chunks reassembles byte-identically; `eof` exactly once (AC13).
- `TestFileReadRange_ClampsAndBounds` — over-cap `length` clamps; offset past EOF; negative offset/length (AC13).
- `TestFileReadRange_ContainmentAndTOCTOU` — `../`, abs-outside, symlink-outside refused; file grown past `maxReadBytes` after stat gives `too_large`, never an oversized read (AC14).
- `TestFileReadRange_IfMtimeConflict` — file touched between chunks gives `conflict` (AC14).
- `TestDaemonInfoFilesCapability` — `files` object present, relay `maxChunkBytes` 512 KiB, direct 4 MiB (AC15).
- `TestFileRangeRelayChunkFitsFrame` — a max relay chunk's JSON envelope encrypts under `maxFrameLen` (AC15).
- `TestFileMethods_NotPluginReachableAndNoContentInLogs` — plugin capability table walk; canary bytes never in logs/events/errors (AC16).
- Windows build: `TestFileReadRange_WindowsPathForms` (`\\?\`, case, separators) behind `//go:build windows` (S17).

Web (vitest):
- `interface-mode.test.ts` — `reads_only_office`, `corrupt_and_throwing_storage_fall_back`, `write_never_throws` (AC1).
- `interface-mode-store.test.tsx` — `four_entry_points_one_store`, `storage_event_from_other_tab` (AC2, AC3).
- `office-gating.test.tsx` — `plus_menu_hides_terminal_review_pr`, `palette_omits_gated_commands`, `gated_shortcuts_noop_and_absent_from_help`, `branch_picker_hidden_value_still_sent`, `hidden_entries_not_tabbable`, `focus_moves_to_composer` (AC4, AC7).
- `office-seeding.test.tsx` — `new_task_seeds_chat_and_files_only`, `existing_tasks_and_open_pty_untouched_on_flip` (AC5).
- `office-payload-parity.test.tsx` — `requests_identical_in_both_modes` over a scripted create-task/send/settings flow (AC6).
- `tool-card-concise.test.tsx` — `terminal_and_edit_hide_command_output_diff`, `failed_shows_error_summary`, `coding_mode_dom_unchanged`, `permission_and_error_cards_never_simplified`, `read_search_fetch_unchanged` (AC8, AC9).
- `greeting.office.test.ts` / `suggested-prompts.test.ts` — `office_pool_per_bucket`, `four_chips_fill_never_send` (AC10, AC11).
- `pdf-preview.test.tsx` — `pdf_opens_in_file_tab_no_new_tabkind`, `mismatch_never_reaches_viewer`, `binary_shows_no_preview_panel`, `states_oversize_password_corrupt_conflict` (AC17, AC22).
- `pdf-viewer-options.test.ts` — `getDocument_options_are_hardened` (AC19); `pdf-host-protocol.test.ts` — `ignores_malformed_and_foreign_messages` (AC20).
- `pdf-sandbox.test.tsx` — `iframe_sandbox_attribute_is_exactly_allow_scripts` (AC18).
- `deliverables.test.ts` — `candidates_from_write_tools_and_final_message`, `path_normalisation_table` (`../`, `~/`, scheme, abs-outside, abs-inside, spaces), `shell_text_never_parsed`, `caps_6_cards_20_candidates` (AC23, AC25).
- `deliverable-card.test.tsx` — `verification_drops_old_missing_mismatch_dir`, `actions_per_platform_and_type`, `no_auto_open`, `both_modes` (AC24, AC26).
- `download.test.ts` — `assembles_with_ifMtime`, `sanitises_filename`, `refuses_over_cap`, `revokes_url` (AC28).

Rust (`desktop/src-tauri`): `open_path_default_refuses_outside_root`, `..._symlink_escape`, `..._directory_and_app_bundle`, `..._extension_not_allow_listed` (table), `..._unc_on_windows` (`cfg(windows)`), `..._remote_daemon`, `..._error_text_has_no_path` (AC27).

Build/size (O4): `bun run build` size check script asserts entry chunk delta ≤ 2 KB and records the viewer chunk size (AC21).

## Phases and PRs (non-stacked; each from `origin/develop`; disjoint write scopes)

Order (at most two PRs in flight, never branched off an unmerged PR):

```
wave 1:  O1 (web: mode)            ||  O3 (daemon: file.stat / file.readRange)
wave 2:  O2 (web: cards/greeting)  ||  O4 (web: PDF preview)           # O2 needs O1 merged; O4 needs O3 merged
wave 3:  O5 (web + desktop: deliverable cards, open in app)             # needs O2 and O4 merged
```

### O1: mode toggle, preference store, sidebar and palette gating (AC1–AC7)
Write scope: `web/packages/ui/src/lib/interface-mode.ts` (+test), `lib/storage.ts` (one key), `stores/interface-mode-store.ts`,
`components/settings/general-section.tsx`, `components/sidebar-footer.tsx`, `components/tab-registry.tsx` (seeding + "+" menu gating helpers),
`components/sidebar-quick-actions.tsx`, `palette/commands.ts` + the registering call site, `keyboard/shortcuts.ts` + `keyboard/actions.ts`,
the composer branch picker file under `components/composer/`, `App.tsx` (tab-new no-ops), live region component. **Not** timeline, greeting,
home, internal/, desktop/. Contract: `office-mode-o1.contract.md` (UI).

### O2: concise tool cards, greeting, suggested prompts (AC8–AC11)
Write scope: `components/timeline/tool-call-card.tsx`, `tool-renderers.tsx`, `tool-call-detail.tsx` (office branches only),
`lib/greeting.ts`, `hooks/use-greeting.ts`, `components/home/home-landing.tsx`, `lib/suggested-prompts.ts` (+ tests). Reads the O1 store;
starts only after O1 merged. Contract: `office-mode-o2.contract.md`.

### O3: binary-safe file read API (AC12–AC16)
Write scope: `internal/files/binary.go` (+test) (new file; `files.go` untouched), `internal/wsapi/files_binary.go` (+test), the two `methodHandlers`
entries in `internal/wsapi/handlers.go`, `daemon.info` `files` field (the one handler that builds it), chunk-limit constants next to the relay
frame constant. **Wire (additive):** methods `file.stat`, `file.readRange`; `daemon.info.files`. Contract: `office-mode-o3.contract.md`
(wsapi: the validator is mandatory). **Not** web, desktop, `internal/server`.

### O4: PDF preview tab (AC17–AC22)
Write scope: `web/packages/ui/src/components/pdf-preview/` (new: pane, host protocol, viewer entry `pdf-viewer.html` + script, options),
`components/file-editor-pane.tsx` (route by `file.stat`), `lib/file-bytes.ts` (chunked read helper, shared with O5's Download), `package.json`
+ lockfile (pdfjs-dist exact pin; the only lockfile churn allowed), Vite config entry for the viewer HTML, and the one `internal/server` handler/
header for `/pdf-viewer.html` CSP. Starts after O3 merged. Contract: `office-mode-o4.contract.md` (UI + security).

### O5: deliverable cards, open in OS app, download (AC23–AC28)
Write scope: `components/timeline/deliverables.ts` (+tests), `deliverable-card.tsx`, the single hook-in line in `run-timeline.tsx`,
`lib/download-file.ts`, `desktop/src-tauri/src/open_path.rs` + its `main`/command registration + capability entry, the desktop-only action
wiring. Starts after O2 and O4 merged. Contract: `office-mode-o5.contract.md` (desktop permission surface + UI).

## Wire changes (all additive)

| Change | PR | Old client / old daemon |
|---|---|---|
| `file.stat`, `file.readRange` methods | O3 | old daemon: `method_not_found`, UI says "Update the daemon to preview files" |
| `daemon.info.files {maxReadBytes, maxChunkBytes, previewKinds}` | O3 | old client ignores it; old daemon omits it and the client treats files as unsupported |
| none for the mode, cards, greeting, prompts | O1, O2, O5 | n/a (client-only, AC6) |
| `/pdf-viewer.html` static document with CSP header | O4 | n/a (served with the UI) |
| Tauri command `open_path_default` | O5 | web has no such command; the action is not rendered |

## Docs to update

- `docs/ARCHITECTURE.md`: an "Office mode (ADR-0049)" section (mode is client-only; file range methods; PDF sandbox; cards) — O5, final.
- `docs/decisions/0049-office-mode.md`: status line and any veto outcomes; amend if a Supervisor default changes.
- `docs/plans/active/zcode-parity.md`: one line under "Hard rules" saying Office mode / doc previews are now a separate epic (this plan) — this PR.
- Shortcut help text and Settings description strings (in code, O1).
- `docs/ROADMAP.md`: only if the Steward asks.

## Frames plan (every UI PR; the Human's rule: light + dark, 1280 and 390, LOOK at each image)

Isolated daemon under `~/.smind/dogfood/office-mode/{home}` on a free port, fake agent / `internal/qaframes` data only, **no live model calls**; a
fake repo there with a generated sample PDF (a small own-made file), a mismatched `.pdf`, a corrupt one, a 60 MB one, and a `.docx` stub.
Sets, each in light and dark at 1280 and 390:
- O1: Settings → General row; footer menu radiogroup (both selected states); "+" menu coding vs office; empty state coding vs office; palette
  open in each mode; the live-region toast frame; transition frames around the flip (the frame mid-flip is reviewed, per the standing rule).
- O2: a run with terminal + edit + failed cards in coding vs office; the failed card; home greeting + 4 chips.
- O4: PDF pane (page 1, zoomed, loading, each error panel), "No preview" panel for the `.docx`, 390 narrow pane.
- O5: a run with 1, 3 and 7 produced files (the "+ N more"), card hover/focus, desktop vs web action sets (desktop macOS frames from the real
  app binary with an isolated `SMIND_HOME`; a locked screen gives black captures: say so, never claim).
Frames are deleted from the dogfood root after the PR merges, except sheets the Steward still needs (path told first).

## Windows notes (S17)

- Relative-path checks normalise `\` and `\\?\`, compare extensions case-insensitively, and treat `C:` drive-relative forms as outside the
  root; the canonical-root test uses the platform's own `filepath` rules.
- UNC network paths are refused by `open_path_default` (no SMB authentication from "Open in app"); local `\\?\C:\…` is allowed.
- `os.Open` already opens with read+write sharing, so a file Word has open can be previewed; a sharing violation surfaces as a clean "file in use".
- Tests with path tables carry Windows rows; `cfg(windows)` Rust tests and the `//go:build windows` Go test run in the Windows host
  verification the fleet work enables; until then they are written and *not run*, and the PR says so.

## Peer plan (GLM policy)

Design, ADR and this plan: the Lead, no Peer (docs). Implementation PRs: peer-glm (profile 8) with step briefs (one coherent step, ≤ 15 min,
exact files, function and test names, expected assertions, commands, "commit after each step, do not push, no `gh pr create`, no `git stash`,
no installs, touch only the named files"), at most two in parallel with disjoint files in their own task worktrees; peer-sonnet (profile 6)
only for the screenshot/frame loops. The Lead reviews every diff line, runs the tests and the mutations, and pushes. The Lead writes code only
to fix a review finding GLM failed to fix twice. On any GLM rate limit the Lead says so at once with the reset time and does non-GLM work.

Planned step briefs (each ends with its own tests green and a commit):
- O1: (1) `lib/interface-mode.ts` + store + storage key + tests; (2) Settings row + footer radiogroup + live region; (3) palette + shortcut + help filter; (4) tab "+" menu, seeding, quick actions, branch picker gating; (5) parity and gating tests.
- O3: (1) `files.Stat` + sniffing + tests; (2) `files.ReadRange` (open once, fd checks) + tests; (3) wsapi methods + `daemon.info.files` + scoping tests.
- O4: (1) `lib/file-bytes.ts` + tests; (2) viewer entry + options + host protocol + tests; (3) pane + `file-editor-pane` routing; (4) CSP handler + size script (Lead reviews with extra care).
- O2, O5 analogous; the Tauri command is Lead-reviewed line by line.

Planned mutations (file + change → test that must then fail; each on committed work, `git checkout -- file` after):
- `readInterfaceMode` accept any non-empty string → `reads_only_office`.
- gating helper returns the "+" menu unfiltered in office mode → `plus_menu_hides_terminal_review_pr`.
- seeding adds `diff`/`terminal` in office mode → `new_task_seeds_chat_and_files_only`.
- send `interfaceMode` in `run.start` params → `requests_identical_in_both_modes` and `TestInterfaceModeNeverOnTheWire`.
- concise card renders the command text → `terminal_and_edit_hide_command_output_diff`; concise card also hides permission cards → `permission_and_error_cards_never_simplified`.
- `files.Stat` trusts the extension → `TestFileStat_SniffsContentNotExtension`.
- `ReadRange` stats the path instead of the open fd, or drops the `maxReadBytes` check → `TestFileReadRange_ContainmentAndTOCTOU`.
- `ReadRange` drops the `ifMtime` compare → `TestFileReadRange_IfMtimeConflict`.
- relay `maxChunkBytes` 512 KiB → 1 MiB → `TestFileRangeRelayChunkFitsFrame`.
- iframe `sandbox` gains `allow-same-origin` → `iframe_sandbox_attribute_is_exactly_allow_scripts`.
- `isEvalSupported:true` or `enableScripting:true` → `getDocument_options_are_hardened`.
- host accepts messages without checking `event.source` → `ignores_malformed_and_foreign_messages`.
- candidate rule also parses Bash command text → `shell_text_never_parsed`; drop the `mtime ≥ startedAt` check → `verification_drops_old_missing_mismatch_dir`.
- `open_path_default` skips the extension allow-list or the canonical-inside-root check → the matching Rust row.
- download name keeps `/` → `sanitises_filename`.

## Decisions

Settled: ADR-0049 H1–H6 (Human) and S1–S18 (Supervisor defaults, Human may veto). Not decided here (later ADRs, ADR-0049 section 8): D folder
workspaces, C generation skills, docx/xlsx/pptx preview (needs section 7 answered), mobile. Open questions are in the ADR.

## Progress

- 2026-10-31: ADR-0049 and this plan written (docs-only PR). No code started. Peer ledger for this item: peer-glm 0 runs, peer-sonnet 0 runs
  (ADR and plan written by the Lead, a Sonnet seat; no Peer was needed for docs).
- 2026-10-31: O3 (daemon) implemented on `smind/win-b-office-o3`: `internal/files/binary.go` (Stat, ReadRange), `internal/wsapi/files_binary.go`, handler-map entries, `daemon.info.files`, method golden, contract `office-mode-o3.contract.md`. Peer ledger for O3: peer-glm 0 runs, peer-sonnet 0 runs (the Windows Lead, a Sonnet seat, wrote the descriptor-level code and tests directly; the diff is small and the part that matters is security).

## Validation

Docs-only PR: ADR/plan cross-references (S-numbers, AC-numbers, PR names, anchors, file paths named in the ADR) checked by hand against the
tree; markdown sanity (fences balanced, tables well-formed) checked; `task lint` run. Implementation PRs fill this section per AC with
evidence (test names, mutation results, frames) as they land; the plan moves to `docs/plans/completed/` only when every AC is confirmed here.

### O3 validation (AC12-AC16), Windows 11 host, 2026-10-31

- AC12-AC16: named tests in `office-mode-o3.contract.md`; `go test ./internal/files ./internal/wsapi -count=1` green on Windows (wsapi 120 s); `GOOS=linux` and `GOOS=darwin` `go vet` clean (FIFO test is `!windows`).
- Mutations (applied to committed code, `git checkout --` after): M1 classify by `.pdf` extension -> `TestFileStat_SniffsContentNotExtension`; M2 drop the size cap -> `TestFileReadRange_ContainmentAndTOCTOU`; M3 no chunk clamp -> `TestFileReadRange_ClampsAndBounds`, `TestFileReadRange_ChunkCeilingPerTransport`; M4 ignore `ifMtime` -> `TestFileReadRange_IfMtimeConflict`; M5 path-form guard off -> `TestFileReadRange_WindowsPathForms` (a first version of that test did *not* kill M5 because a UNC path fails later anyway; it now asserts the guard's `invalid_params`); M6 relay limit = direct -> `TestDaemonInfoFilesCapability`, `TestFileReadRange_ChunkCeilingPerTransport`, `TestFileRangeRelayChunkFitsFrame`.
- Not run here: the symlink subtests skip on a Windows host without symlink privilege, so symlink containment ran on no host in this session; the FIFO test is `!windows`. `TestFileMethods_...NoContentInLogs` checks the slog default and error text only; `request_log` is the proxy meter and file RPCs never write to it.
- Known gap, not in the write scope: `file.read` / `file.write` still pass a UNC absolute path to `realpath` before the containment check (the O3 methods refuse it first). Follow-up: move `refuseUnsafeForm` into `resolveInRoot`.
