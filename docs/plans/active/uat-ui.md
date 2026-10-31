# UAT UI — user-journey sweep of the web and desktop apps

Epic (Human, via Supervisor, 2026-10-31): find the bugs a user really hits by running full user journeys, fix them, and lock them with a Playwright regression suite.
Size: L (7) — scope 2 (web, desktop, harness), PRs 2 (three phases), risk 0 (phase 1 changes no product code), decisions 1 (CI suite placement), verification 2 (screenshots in two themes and two widths, real daemon).

Phase 1 (this file): prep, catalogue, harness, first ranked bug list. Phases 2 (fixes) and 3 (CI Playwright suite) are separate PRs after the Steward reviews the phase-1 report. Nothing in phase 1 touches product code.

## Acceptance criteria (phase 1)

- AC1 This catalogue lists 40–60 journeys, one row each with id, steps, expected result, surfaces and priority, covering every area in the brief (see the table).
- AC2 An isolated harness under `~/.smind/dogfood/uat/` (own `SMIND_HOME`, own port, fake agents only, no provider secret, no model call) with a README of exact commands. Every script refuses to run unless `SMIND_HOME` is under `~/.smind/dogfood/uat/` and the port is not 4648/4870.
- AC3 A seed creates workspaces, tasks (finished, busy, waiting a permission, errored, long, archived), queue items, a second permission request and a Lead tree, from the fake agents.
- AC4 Every journey runs on the web app in light and dark at 1280 and 390; a screenshot per journey × theme × width lands in `/tmp/steward4/uat/<journey-id>-<theme>-<width>.png`; console errors and failed requests are logged beside it.
- AC5 Every screenshot is looked at; each defect is recorded below with id, journey, severity, repro, screenshot path and suspected file.
- AC6 The report says plainly what could not be covered (desktop shell, blocked journeys, anything stubbed).

## Test scenarios (named)

The harness scripts are the scenarios; the names are the journey ids in the table (`J01`..). A scenario passes when its steps complete and its expected result is visible; a failure or a visual defect becomes a bug row. These scripts become the phase-3 Playwright suite.

- `harness_guard_refuses_foreign_home`: running any harness script with `SMIND_HOME=/tmp/x` exits non-zero with a message (checked once, evidence in Validation).
- `harness_seed_idempotent`: `uat-reset.sh` twice gives the same seed ids.
- `journeys_capture_all`: every J row yields its screenshots in the four theme × width combinations.

Priority: P0 = the product is unusable without it, P1 = a core flow, P2 = secondary, P3 = edge.
Surfaces: W = web at 1280, N = web at 390, D = desktop (see "not coverable").

## Journey catalogue

| id | journey | steps | expected | surfaces | pri |
|---|---|---|---|---|---|
| J01 | First run: welcome with no workspaces | open app on an empty home | welcome/home landing shows, a clear first action, no console errors | W N D | P0 |
| J02 | First-run connect methods | open connect screen, switch local / URL / SSH forms, submit an empty and a bad URL | validation message per form, no crash | W N D | P1 |
| J03 | Login screen with password set | daemon with a password, open app, wrong then right password | wrong shows an error, right lands on the home | W N | P1 |
| J04 | Add a workspace | sidebar add, folder picker, choose a repo, name it | workspace appears in the sidebar, selected | W N | P0 |
| J05 | Add a workspace: bad path | give a path that is not a git repo | readable error, dialog stays usable | W | P1 |
| J06 | Create a task | new task dialog, title, provider `uat`, create | task opens, setup state resolves, composer focused | W N | P0 |
| J07 | Send a prompt and read the timeline | send a prompt to the rich fake agent | thought, plan, tool calls, markdown table and code render, run ends done | W N | P0 |
| J08 | Long timeline scroll | open the 400-line finished task, scroll, jump to the end | scroll is smooth, composer stays pinned, no layout jump | W N | P1 |
| J09 | Tool call details | expand each tool call, including the failed one | details readable, failed call marked, diff block renders | W N | P1 |
| J10 | Permission request: approve | task waits on a permission, open it, approve | card shows the path/diff, run continues and finishes | W N | P0 |
| J11 | Permission request: deny | second request, deny | run reflects the denial, no stuck spinner | W N | P0 |
| J12 | Inbox | open the inbox with two pending requests | items listed, badge count right, click opens the task, count drops after answering | W N | P0 |
| J13 | Queue a prompt while busy | busy task, send two prompts | queue chips show order, they run in order when the first ends | W N | P0 |
| J14 | Cancel a queued prompt | cancel one queue chip | it disappears, the other still runs | W | P1 |
| J15 | Stop a run | stop the hanging run | state becomes stopped, composer usable, no ghost "running" | W N | P0 |
| J16 | Errored run | task whose agent returns an error | error shown with details, retry or resend possible | W N | P1 |
| J17 | Task tree: Lead with Peers | open the Lead task, view tree and lineage | children listed with tier and status, navigating to a child works | W N | P1 |
| J18 | Task lineage breadcrumbs | open a child, use the lineage link back | parent reached, selection consistent | W N | P2 |
| J19 | Terminal: open and type | open a terminal tab, run `echo hi` | output appears, prompt returns | W N | P0 |
| J20 | Terminal: Ctrl+C | run `sleep 100`, press Ctrl+C | process interrupted, prompt back | W | P1 |
| J21 | Terminal: resize | resize the viewport 1280→390→1280 while a terminal is open | no clipped columns, content reflows, no blank pane | W N | P1 |
| J22 | Files: explorer and open | open the files pane, open `README.md` and a source file | tree lists files, editor shows content with highlighting | W N | P1 |
| J23 | Files: edit and save | change a file, save | dirty marker, saved state, the git panel shows it changed | W | P1 |
| J24 | Diff view | open the diff pane on the task with uncommitted changes | files listed, unified diff renders, untracked file shown | W N | P0 |
| J25 | Git panel states | task with dirty tree, clean tree (beta), empty repo (gamma) | branch line and counts correct in each state | W N | P1 |
| J26 | PR panel: no forge | open the PR panel where `gh` is absent | a clear "not connected" state with the way to fix it, no raw error | W N | P1 |
| J27 | Create PR dialog | open the create-PR dialog with no remote | dialog explains the blocker, does not crash | W | P2 |
| J28 | Sync menu | open the sync menu on a repo without a remote | disabled or explained actions | W | P2 |
| J29 | Settings: general and appearance | open settings, switch theme light/dark/system | theme applies instantly, persists on reload | W N | P0 |
| J30 | Settings: providers | list providers, open one, the custom `uat` row | rows readable, status per provider, no secret shown | W N | P1 |
| J31 | Settings: accounts | open the accounts dialog, add-account form, cancel | form validates, nothing saved on cancel | W N | P1 |
| J32 | Settings: usage | open usage and the sidebar usage summary with no accounts | empty state not a broken chart | W N | P2 |
| J33 | Settings: profiles | open profiles | list and form fit the width, no overflow | W N | P2 |
| J34 | Settings: shortcuts | open shortcuts, search "new" | filtered list, the dialog opens from the keyboard too | W | P2 |
| J35 | Settings: notifications, MCP servers, plugins, daemon | visit each section | each renders, none blank or errored | W N | P2 |
| J36 | Command palette | open with the shortcut, type, run a command, Esc | results filter, command runs, focus returns | W N | P0 |
| J37 | Quick open | open quick-open, type a file name, Enter | file opens | W | P2 |
| J38 | Sidebar search and filter | type a task title, clear, filter by status | list narrows and restores, empty result state | W N | P1 |
| J39 | Sidebar archive and restore | archive a task, open archived tasks, restore | task leaves and returns, selection moves sanely | W N | P1 |
| J40 | Rename and delete a workspace or task | rename a task inline, delete a workspace dialog (cancel) | rename persists, delete asks and cancel keeps it | W | P2 |
| J41 | Hosts page | open Settings → Hosts (fleet stubbed via the WS injector, isolated) | list with link states, empty state, no crash with fleet off | W N | P1 |
| J42 | Pairing dialog | invite and add-host dialogs, approve dialog | invite code shown, SAS readable, dialogs fit 390 | W N | P1 |
| J43 | Automations page (off state) | open Settings → Automations with the feature off | the off-state card explains how to enable it | W N | P1 |
| J44 | Automations page (full) | list, create form, history | BLOCKED until Lead 283's page merges; re-run then | W N | P1 |
| J45 | Reconnect after daemon restart | restart the isolated daemon with the app open | banner/indicator shows, app reconnects, timeline and queue intact, no manual reload | W N | P0 |
| J46 | Reload mid-run | reload the browser during a hanging run | run state restored, same task selected via the hash | W N | P0 |
| J47 | Deep links and back/forward | open `#/` task URLs, browser back and forward | selection follows the hash, no loop | W | P1 |
| J48 | 390px layout sweep | at 390 open the sidebar, a task, settings, the palette, dialogs | nothing clipped or overflowing horizontally, tap targets reachable, sidebar opens and closes | N | P0 |
| J49 | Keyboard-only main flow | Tab/Shift+Tab/Enter/Esc: add task, send, answer permission, open palette | visible focus ring at every step, no trap, Esc closes layers in order | W | P0 |
| J50 | Light/dark contrast pass | every J screenshot in both themes | no unreadable text, no stuck light panels in dark | W N | P1 |
| J51 | Empty and error states | task with no runs, a workspace with no tasks, an unreachable daemon | each state has copy and an action | W N | P1 |
| J52 | Composer: attachments and @file | attach a small image, type `@` for the file picker | chip appears, picker lists files, send keeps both | W N | P2 |
| J53 | Run configuration options | open the mode/model options, switch mode | options list, change applies and shows | W N | P2 |
| J54 | Window resize across the 768 breakpoint | drag 1280 → 800 → 390 on a task | layout switches without losing state | W N | P1 |
| J55 | Open a PDF in Preview (office mode O4) | open a task with a PDF in its files, open it in a Preview tab (sandboxed pdf.js classic worker, PR #752) | pages render, zoom and page controls work, no console or CSP errors, on the 390 layout too | W N D | P1 |
| J56 | PDF viewer inside the desktop WKWebView | open the same PDF in the built macOS app's WKWebView (not Chromium) | same render as J55; proves the worker and sandbox work in WebKit | D | P1 |

56 journeys. J44 is blocked until the Automations page merges; J55 and J56 are blocked until #755 is on develop (after #752 and #753; #755 exempts GET /pdf-viewer.html and /pdf-viewer.js from the desktop proxy cookie gate, without which the viewer never started in the native app). J56 is then run by building the macOS app from develop into the dogfood dir, with no Human daemon, then run on the merged develop build.

## Not coverable here (fills in as the run proceeds)

- Desktop shell: the Tauri app (`/Applications/sMind.app` is the Human's live install and is off limits; a dev build needs a Rust toolchain and a native window that Playwright cannot drive). The web UI built with `VITE_SMIND_DESKTOP=1` and a stubbed Tauri bridge covers desktop-mode layout only (earlier `p5` approach); native chrome, traffic lights, updater and the sidecar daemon are not covered.
- J56 (PDF in the desktop app's WKWebView): cannot be driven headless here. Playwright in this harness has only Chromium (no WebKit build, and installing one is out of scope), `safaridriver` cannot attach to a Tauri webview, and `/Applications/sMind.app` is the Human's live install. A dev build is possible (cargo is installed) but it would start its own sidecar daemon and needs a native window, so it needs the Steward's go-ahead and an isolated `SMIND_HOME`. Until then WebKit rendering of the viewer is unverified; the fallback is a Safari (WebKit) check by hand on the isolated daemon URL.
- Anything needing a real provider, `gh` or GitHub (PR panel with a real PR), push notifications, mobile app.

## Decisions

- Fake agent only: a node ACP stub with scenario files (`hang`, `permission`, `error`, `long`, `slow`) so no model call or secret is ever involved.
- One isolated daemon on port 4961 serving the embedded UI; the Hosts and Automations pages use the Playwright WebSocket injector, since fleet and automations are off in the isolated daemon.
- Screenshots are viewport captures at 1280×800 and 390×844 named `<id>-<theme>-<width>.png`.

## Progress

- [x] Catalogue (56 journeys; J44 blocked on the Automations page, J55/J56 blocked on #755).
- [x] Harness built and smoke-tested (`~/.smind/dogfood/uat/`, README there).
- [x] Journey scripts J01-J54 written (`~/.smind/dogfood/uat/journeys/`).
- [x] Full run on the web app, light and dark at 1280 and 390, screenshots reviewed (contact sheets for triage, full size for every suspicious one).
- [x] Ranked bug list (below).
- [ ] J44, J55, J56 and the two Steward entries S-01/S-02 verified on the merged builds.
- [x] Phase 2 PR1: A-02, A-03, B-01 fixed with regression tests (App.test.tsx), before/after frames in `/tmp/steward4/zcode-audit/after/uat-pr1/`.
- [ ] Phase 2 later PRs (A-01, B-03, the P2s) and phase 3 (CI suite): not started.

## Validation

- AC1: 56 journeys in the table (target 40-60).
- AC2: harness under `~/.smind/dogfood/uat/` with `lib/guard.sh` and `lib/guard.cjs`. Guard proven: `SMIND_HOME=/tmp/x bin/uat-up.sh` and `node lib/guard.cjs` exit 1 ("REFUSING: SMIND_HOME=/tmp/x resolves to /private/tmp/x, not under ~/.smind/dogfood/uat/"); `UAT_PORT=4648` and an unset `SMIND_HOME` are refused too. No model call: agent is `agent/uat-agent.cjs`; every send script asserts the provider chip is the fake agent first.
- AC3: seed gives 3 workspaces and 11 tasks (idle, busy with 2 queued, 2 permission requests, errored, long, archived, a Lead with 2 Peers).
- AC4: 54 scripted journeys x 4 combos = 216 runs; 27 in group A and 27 in group B. Screenshots in `/tmp/steward4/uat/<id>-<theme>-<width>.png` (plus `-b`, `-open` style extra states), console logs beside them (only J03's expected 403/401, J45's restart and J53 narrow were non-empty).
- AC5: all shots reviewed; I opened the top findings myself at full size (A-01, A-02, B-01, B-03 confirmed in the PNGs).
- AC6: see "Not coverable".
- Results: group A 21/27 journeys pass in all four combos (J06, J12, J17, J19, J22, J24 fail on app findings); group B passes everywhere except J39 and J51 (app findings), J49/J52/J53 failed only on script issues and pass after fixes.
- Peers: 3 peer-sonnet (harness; group A; group B, B restarted 5 times: early run end x2, session limit, CLI killed reading an oversized image). 0 peer-glm: GLM was rate limited until about 23:28 and the work was exploratory UI driving, so it went to Sonnet. Not verifiable: whether a GLM run carries `--max-turns 500` (no GLM run happened).
- Sources: `/tmp/steward4/uat/bugs-A.md`, `bugs-B.md`, `bugs-S.md` hold the full repro text.

## Ranked bug list

No P0 found. Severity: P0 data loss/crash/blocked flow, P1 broken feature or wrong state, P2 visual/UX, P3 nit. Screenshot paths are under `/tmp/steward4/uat/`.

| id | journey | sev | repro (short) | screenshot | suspected file |
|---|---|---|---|---|---|
| A-01 (FIXED in phase-2 PR2: palette New task/New workspace open at every width, dialogs mount outside the mobile Sheet) | J06 | P1 | 390px: Cmd+K, "New task in alpha", Enter does nothing; opening the drawer then shows a stale New-task dialog (crud dialogs live in the unmounted sidebar Sheet). Also likely "New workspace". 2/4 combos (both 390) | `j06-palette-noop-light-390.png`, `j06-stale-light-390.png` | `components/app-sidebar.tsx`, `ui/sidebar.tsx` |
| B-01 (FIXED in phase-2 PR1: selection moves off an archived task; daemon already refused run.start on it, guard proven by mutation) | J39 | P1 | Archive the selected task: row leaves the list but hash and pane still show it, composer enabled (a prompt could be sent into an archived task). 4/4 | `j39-archived-light-1280.png` | `App.tsx` (task.archived selection), `app-sidebar.tsx` |
| A-03 (FIXED in phase-2 PR1: the chip opens the Diff tab) | J24 | P1 | Click the "+5 -0" diff chip above the composer: nothing opens. 4/4 | `j24-chip-noop-light-1280.png` | `composer.tsx` onOpenDiff -> `App.tsx` revealInDiff |
| A-02 (FIXED in phase-2 PR1: one Terminal tab) | J19 | P1 | One click on the header terminal button opens two tabs ("Terminal 2" with activity dot and "Terminal"). 4/4 | `j19-open-light-1280.png` | `App.tsx` focusOrOpenTerminal/openTerminalTab, `terminal-pane.tsx` |
| B-03 (FIXED in phase-2 PR3: Can't reach the daemon + Retry) | J51 | P2 | WS refused at load: sidebar "Loading workspaces..." forever, only a small "Connecting..." chip; no unreachable copy or retry (mid-session reconnect J45 is fine). 4/4 | `j51u-dark-1280.png` | `app-sidebar.tsx`, connection status |
| A-07 (NOT a bug: by design, plan stalled-tree-alert L1 has no finished-tree exemption; needs a Human decision to change) | J17/J12 | P2 | A tree whose tasks are all done is flagged "stalled" and listed in the Inbox ~20 min after the runs ended. 2/4 (time dependent) | `j17-err-light-1280.png`, `j12-list-light-1280.png` | stalled-tree detection in `task.tree` / `task-lineage.tsx` |
| A-06 (NOT fixed in PR2: by design, see Phase 2 PR2) | J17 | P2 | 390px: a Lead's child pills are hidden (strip shows only "2 done"), children reachable only via the sidebar. 2/4 | `j17-narrow-strip-light-390.png` | `task-lineage.tsx` |
| A-08 (FIXED in phase-2 PR3: permission rows get Open task) | J12 | P2 | Inbox permission rows offer only "Review request"/"Mark seen", no "Open task". 4/4 | `j12-list-light-1280.png` | `inbox/inbox-screen.tsx` |
| A-09 (FIXED in phase-2 PR2: picker content stays inside the dialog at 390) | J04 | P2 | 390px folder picker: path, list and buttons run past the dialog edge. 2/4 | `j04-picker-light-390.png` | `folder-picker-dialog.tsx` |
| A-10 (FIXED in phase-2 PR3: ACP diff content renders as an inline diff) | J09 | P2 | Edit tool call with ACP diff content renders as raw JSON, not a diff (renderer reads tool input only). 4/4 | `j09-failed-light-1280.png` | `timeline/tool-call-detail.tsx` |
| A-04 (NOT fixed: the editor has no @codemirror/lang-* package at all; needs a new dependency, Human decision) | J22 | P2 | Go file opens with no syntax highlighting. 4/4 | `j22-light-1280.png` | `code-mirror-editor.tsx` |
| B-02 (FIXED in phase-2 PR3: Task not found state, hash cleared) | J47 | P2 | Hash with an unknown task id: warm = previous task stays shown while URL says 99999; cold = landing page with stale hash; no "not found". | `probe-x-light-1280.png` | `App.tsx` route handling |
| S-01 (FIXED in phase-2 PR2: automation chip is icon-only below 18rem; long titles still ellipsise) | sidebar | P2 | (Steward, PR #759) Automation chip squeezes task titles to "Nightly d..."; go icon-only at narrow widths or give the title priority. Not yet reproduced here | - | `app-sidebar.tsx` |
| S-02 (FIXED in phase-2 PR2: off card names `smind service restart` for launchd/systemd (daemon.info managedBy), both while unknown) | J43 | P2 | (Steward, PR #759) off-state card says "smind stop && smind serve"; a service-managed daemon needs "smind service restart" (use daemon.info or list both). Not yet reproduced here | - | `settings/automations-section.tsx` |
| A-11 (FIXED in phase-2 PR3: composer focused after create from the dialog) | J06 | P2 | Composer not focused after creating a task (maybe intentional). 4/4 | `j06-light-1280.png` | `composer.tsx` |
| B-05 (FIXED in phase-2 PR5: tokens, see below) | J50 | P3 | Low contrast: sidebar timestamps ~2.2:1; orange status text on light ("Unreachable", "Automations are off", "Timed out") 2.7-3.0:1. 2/4 measured | `j50-*` | tokens / `index.css` |
| B-06 (FIXED in phase-2 PR4: short reasons Not installed / No account / Update needed, raw text in the title) | J30/J52 | P3 | Provider picker shows raw errors (`exec: "codex": executable file not found ... codex home ...`), clipped text at 1280 | `probe-b-light-1280.png` | `components/pickers` |
| A-13 (FIXED in phase-2 PR2: diff counts stay on one line, PR action stacks below sm) | J24 | P3 | 390px diff header wraps "-0" and floats the Create-PR reason. 2/4 | `j24-light-390.png` | `diff-viewer-pane.tsx` |
| A-12 (FIXED in phase-2 PR4: the replayed resume note is its own paragraph (daemon run.logs)) | J16 | P3 | Resume-fallback notice glued into the assistant text. 3/4 | `j16-dark-1280.png` | daemon resume notice |
| B-04 (FIXED in phase-2 PR4: palette has Open settings) | J36 | P3 | Palette: "open settings" matches nothing though shortcuts list it; odd grouping and branch-path subsequence matches. 4/4 | `j36-typed-light-1280.png` | `command-palette.tsx` |
| B-09 (NOT changed: auto-starting the browser login on Connect is deliberate (providers-section test: an oauth provider's Connect starts its login at once); UX question for the Human) | J31 | P3 | "Connect account" starts the browser login at once and disables the method radios until Cancel. 4/4 | - | `accounts-dialog.tsx` |
| B-07 (FIXED in phase-2 PR4: 36px hit areas below sm for the sidebar toggle, quick actions, group-mode radios) | J48 | P3 | 390px tap targets under 32px (sidebar toggle 28, quick actions 28, usage controls 28). | `j48-*` | sidebar, usage |
| B-08 (FIXED in phase-2 PR4 at 1280 (390 collapse toggle ring not verified)) | J49 | P3 | No focus ring on the "Permission mode" button and `task-collapse-toggle`. | `j49-*` | `ui` button focus styles |
| B-10 (FIXED in phase-2 PR2: invite value ellipsized, Copy visible, full value in title) | J42 | P3 | 390px invite code clipped without an ellipsis. | `j42-invite-dark-390.png` | `hosts/invite-host-dialog.tsx` |
| A-05 (FIXED in phase-2 PR4: folders first, .git hidden) | J22 | P3 | File explorer lists `.git` and puts folders after files. 4/4 | `j22-tree-light-1280.png` | `file-explorer-pane.tsx` / `files.list` |
| A-14 | J06 | P3 | "New task" with nothing selected opens the generic palette. | - | `app-sidebar.tsx` |
| A-15 | J05 | P3 | Bad-path error shows raw server wording. | `j05-b-light-1280.png` | daemon error text |

Counts: 0 P0, 4 P1, 11 P2, 12 P3 (27 entries; B-02 and S-01/S-02 included in P2).

## Not coverable (final)

- Desktop shell (native chrome, WKWebView, updater, sidecar). J56 stays blocked until #755 is on develop; it then needs a dev build in the dogfood dir.
- J02 connect forms (local/URL/SSH): desktop-only, not in the web build; only asserted the web settings have no Connections section.
- J44 (Automations page) and J55 (PDF preview): blocked until the pages merge. S-01/S-02 likewise.
- J26/J27 PR panel and create-PR: no forge in the isolated harness; only the disabled/explained states were asserted.
- J53: the fake agent has one mode, so switching among several modes was not driven; no real provider was ever driven.
- J40: there is no task-rename UI to test.
- Contrast numbers (B-05) come from a script; the first scanner version was wrong and discarded.
- Not every one of the 216 images was opened full size; the rest were judged on contact sheets and assertions. J50/J51 were re-run on 2 of 4 combos after script fixes.

## Phase 2 PR1 validation (A-02, A-03, B-01)

- Tests (`web/packages/ui/src/App.test.tsx`): `clicking the diff chip opens the Diff tab when it is not open (UAT A-03)`, `the header terminal button opens exactly one Terminal tab and a second click opens no more (UAT A-02)`, `archiving the selected task moves the selection off it (UAT B-01)`, `archiving the only task lands on the landing, not on the archived task (UAT B-01)`. App, App.responsive, tab-registry and app-header suites: 131 pass.
- Mutations, each on committed work, test fails: revert `revealInDiff` to a bare `activate` (A-03 test fails); back to `nextTerminalTab` for the first terminal (A-02 test fails); early `return` in the `task.archived` handler (both B-01 tests fail); `if false && task.ArchivedAt != nil` in `prepareStart` (`TestRunStart_RefusesArchivedTask` fails: the daemon guard for a send into an archived task already existed and is covered).
- A-02 root cause: the header button opened the index-2 tab ("Terminal 2"), and the route restore for the resulting `#/.../terminal` hash opened the base tab as well, so two tabs appeared; the first terminal is now the base `Terminal` tab.
- UAT re-run on the real harness, light and dark at 1280 and 390: J19, J24, J39 failed 12/12 before and pass 12/12 after. Frames and sheets: `/tmp/steward4/zcode-audit/after/uat-pr1/`. Not seen: J39 at 390 shows the drawer open in its last frame, so the post-archive pane there rests on the journey's hash assertion.

## Phase 2 PR2 validation (A-01, A-09, A-13, B-10, S-01, S-02)

- Tests: `app-sidebar-dialog-mount.test.tsx` (palette New task / New workspace with the drawer closed), `folder-picker-dialog.test.tsx` (A-09), `automation-chip.test.tsx` (S-01), `invite-host-dialog.test.tsx` (`TestInviteDialog_LongInviteShrinksAndSignalsClipping`), `diff-viewer-pane.test.tsx` (A-13), `automations-off-card.test.tsx` (S-02, three cases). jsdom has no layout, so the layout items assert the class the fix introduces; the frames are the layout proof. Full web suite: 424 files, 4445 tests pass; `tsc -b` clean.
- Mutations on committed work, each fails its test: A-01 `app-sidebar.tsx` back to develop (both mount tests fail); A-09 drop `[&>*]:min-w-0`; S-01 drop `@max-[18rem]/sb:hidden`; B-10 drop the `title`; A-13 drop `whitespace-nowrap`; S-02 `service = false`.
- Frames, before/after, light and dark: `/tmp/steward4/zcode-audit/after/uat-pr2/` (REPORT.md, sheets/, transition bursts for the dialog and picker openings: no flash, no drawer/dialog fight, no jump). All six items pass on the after build.
- A-06 not changed: `task-lineage.tsx` hides the local child pills below `sm` on purpose (the children are one tap away in the sidebar drawer tree). A-06 stays open as a design question for the Human (show a "2 children" chip that opens the drawer, or accept).
- Not covered: S-01 with a real automation (the isolated daemon has automations off, so links were injected into the task list through the WS injector); the title "Nightly deps sweep" still ellipsises at the default 256px sidebar (the chip is icon-only there; only a shorter label or a wider sidebar fixes the rest).

## Phase 2 PR3 validation (A-08, A-10, B-02, B-03, A-11, Start-from labels, token redaction)

- Tests: `PermissionRowOpenTask` (inbox-screen), `edit with ACP diff content renders a diff, not raw JSON` and the trailing-newline cases (tool-call), two B-02 tests and the A-11 test (App.test.tsx), the B-03 sidebar test (app-sidebar.test.tsx), the Start-from short-label test (new-task-dialog.test.tsx), `never puts the access token in the error message` (ws-client). Full web suite: 424 files, 4458 tests pass at the last full run; `tsc -b` clean.
- Mutations on committed work, each fails its test: inbox-screen back to develop; `diffFromContent` off; `setMissingTaskId` removed; B-03 branch disabled; A-11 `runAction` replaced; segmented-control `sm:hidden` removed; `splitLines` trailing-pop removed; `redactToken` returns the url.
- Frames: `/tmp/steward4/zcode-audit/after/uat-pr3/` (REPORT.md, sheets, 12-frame bursts for B-02 and the A-11 dialog close and B-03 retry). Findings from the frames became two extra fixes: a stray lone - and + line in the ACP diff (trailing newline), and the daemon access token printed in the red "Disconnected: wsclient: failed to connect to ws://...?token=..." banner (now `token=REDACTED`).
- Harness note: `routeWebSocket` + close() opens a mocked socket, so the older j51/j51u variant never exercises a real refusal; `pr3-b03` (socket never opens, error + close 1006) is the faithful B-03 journey.
- Not verified: A-11 shows no visible caret/ring in a headless frame (asserted through `document.activeElement`); S-01 etc. as in PR2.
- Left for the remaining P3 list (stop before B-05 tokens): B-05 contrast, B-06 raw provider errors, A-12, B-04, B-09, B-07, B-08, A-05, A-14, A-15.

## Phase 2 PR4 validation (Jump to latest pill, A-05, A-12, B-04, B-06, B-07, B-08)

- **Jump to latest pill** (Supervisor, seen in a UAT frame): the pill floated over the log and covered the end of the last message. A first fix reserved bottom padding; the frames showed overlaps remained 120-200px above the tail (and at 390 while streaming), so it was replaced by the Steward's first option: the pill is an in-flow row (`jump-to-latest-slot`) between the log and the composer, shown only while not following. The log viewport shrinks by the row, nothing is ever under the pill, and the content does not move (anchor drift 0 px at 1280 and 390 over distances 300..0, no toggling at the 32px threshold). Tests: the slot is a sibling after the scroll container, the button is not absolute, the slot is absent while following. Mutations: pill class gets `absolute`; slot disabled (both fail).
- Other tests: `explorer lists folders first, hides .git`, provider-reason formatter (3 more cases), picker row title, focus-ring classes (Permission mode, task-collapse-toggle), 36px hit-area classes (header toggle, quick actions, group radios), palette `open settings`, `TestToRunLogEvent_SessionNoteIsItsOwnParagraph`. Full web suite 425 files / 4477 tests pass; `tsc -b`, `go vet ./internal/wsapi`, gofmt clean; `go test ./internal/taskrunner ./internal/runs ./internal/wsapi` ok at the A-12 commit.
- Mutations on committed work, each fails its test: explorer `.git` filter, picker `Not installed` mapping, `No account` mapping, collapse-toggle ring, Permission-mode ring, quick-action `min-h-9`, header toggle class, group radio `itemClassName`, palette title renamed, session-note blockquote removed (all pass as failures).
- Frames: `/tmp/steward4/zcode-audit/after/uat-pr4/` (REPORT.md, sheets, 12-frame bursts of the pill appearing while the timeline streams and disappearing after the click).
- Dropped from this PR: B-09 (see the table), A-14 and A-15 (design questions: the sidebar "New task" fallback, raw server wording of a bad workspace path), B-05 (stop and report: contrast tokens).
- Not verified: 390 keyboard focus on the collapse toggle (the harness could not land focus inside the drawer); pixel-level overlap at every scroll position (geometry says the slot never intersects the log).

## Phase 2 PR5 validation (B-05 contrast tokens)

Single source of truth changed (`index.css`, `zcode-tokens.json`, `docs/design.md`), no per-component colour overrides: light `--muted-foreground` oklch(0.556 0 0) to oklch(0.5 0 0) (4.15 to 5.26 on the sidebar); new role token `--warning-text` (light `#a84a00`, dark = `--warning`) for orange TEXT (2.63 to 5.06 on the sidebar, 2.35 to 4.52 on the warning chip over the sidebar); `--warning` stays ZCode's orange for dots, fills and glyphs; the sidebar row time uses `text-muted-foreground` instead of `text-foreground-subtlest` (2.2:1). Text usages of `text-warning` switched to `text-warning-text`. First try `#914e00` read brown on the orange chips; `#a84a00` is the warmest value that still holds 4.5:1 on the tightest surface.

Before/after ratios (script: `web/packages/ui/scripts/contrast-table.ts`):

| pair | surface | light before | light after | dark before | dark after |
|---|---|---|---|---|---|
| --muted-foreground | --background | 4.46 | 5.65 | 6.98 | 6.98 |
| --muted-foreground | --sidebar | 4.15 | 5.26 | 6.98 | 6.98 |
| --muted-foreground | --card | 4.73 | 6.00 | 5.46 | 5.46 |
| --muted-foreground | --panel | 4.73 | 6.00 | 6.28 | 6.28 |
| --muted-foreground | --input-bg | 4.73 | 6.00 | 5.46 | 5.46 |
| --muted-foreground | --surface over --background | 4.19 | 5.31 | 6.16 | 6.16 |
| --muted-foreground | --surface over --sidebar | 3.90 | 4.95 | 6.16 | 6.16 |
| --muted-foreground | --hover over --background | 4.01 | 5.09 | 6.16 | 6.16 |
| --muted-foreground | --hover over --sidebar | 3.74 | 4.75 | 6.16 | 6.16 |
| --muted-foreground | --selected over --background | 4.01 | 5.09 | 5.29 | 5.29 |
| --muted-foreground | --selected over --sidebar | 3.74 | 4.75 | 5.29 | 5.29 |
| --warning-text (was --warning) | --background | 2.82 | 5.43 | 7.70 | 7.70 |
| --warning-text (was --warning) | --sidebar | 2.63 | 5.06 | 7.70 | 7.70 |
| --warning-text (was --warning) | --card | 3.00 | 5.76 | 6.02 | 6.02 |
| --warning-text (was --warning) | --panel | 3.00 | 5.76 | 6.93 | 6.93 |
| --warning-text (was --warning) | --warning-surface over --background | 2.51 | 4.82 | 5.90 | 5.90 |
| --warning-text (was --warning) | --warning-surface over --sidebar | 2.35 | 4.52 | 5.90 | 5.90 |

Regression test: `lib/contrast-tokens.test.ts` (>= 4.5 for muted and warning text on background, sidebar, card, panel, input, and the translucent surfaces over background and sidebar, both themes) with `lib/contrast.test.ts` for the maths. Mutations: light muted back to 0.556 (4 pairs fail), `--warning-text` back to `var(--warning)` (fails), `#b45309` (fails on sidebar and both warning-surface pairs). Full web suite 428 files / 4562 tests pass.
Frames and in-page scans: `/tmp/steward4/zcode-audit/after/uat-pr5/` (REPORT.md, swatches.png): failing text elements summed over the frames, light 1280 195 to 72, light 390 68 to 29, dark 1280 114 to 35, dark 390 34 to 15. Remaining offenders are non-orange (green Connected/On chips ~3.9:1 in light, disabled composer chips, `--foreground-subtle(st)` placeholders by design): candidates for a next round.
