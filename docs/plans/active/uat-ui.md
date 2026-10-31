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

54 journeys. J44 is blocked until the Automations page merges.

## Not coverable here (fills in as the run proceeds)

- Desktop shell: the Tauri app (`/Applications/sMind.app` is the Human's live install and is off limits; a dev build needs a Rust toolchain and a native window that Playwright cannot drive). The web UI built with `VITE_SMIND_DESKTOP=1` and a stubbed Tauri bridge covers desktop-mode layout only (earlier `p5` approach); native chrome, traffic lights, updater and the sidecar daemon are not covered.
- Anything needing a real provider, `gh` or GitHub (PR panel with a real PR), push notifications, mobile app.

## Decisions

- Fake agent only: a node ACP stub with scenario files (`hang`, `permission`, `error`, `long`, `slow`) so no model call or secret is ever involved.
- One isolated daemon on port 4961 serving the embedded UI; the Hosts and Automations pages use the Playwright WebSocket injector, since fleet and automations are off in the isolated daemon.
- Screenshots are viewport captures at 1280×800 and 390×844 named `<id>-<theme>-<width>.png`.

## Progress

- [x] Catalogue drafted (54 journeys).
- [ ] Harness built and smoke-tested.
- [ ] Journey scripts written, group A (J01–J27) and group B (J28–J54).
- [ ] Full run, screenshots reviewed.
- [ ] Ranked bug list.

## Validation

(filled in at the end of phase 1)

## Ranked bug list

(filled in after the run)
