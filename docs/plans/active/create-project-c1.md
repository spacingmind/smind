# C1: the "Add project" dialog, restructured (first PR of the create-project flow)

Size M, **web UI only, NO new RPC** (`fs.listDir`, `workspace.create`, `workspace.list`, `fleet.host.list` and the desktop's saved Connections, nothing else). Contract: `docs/plans/active/create-project-c1.contract.md`. Desktop is the target (centred modal; the full-bleed sheet at 800x520, as Settings does since B3); mobile is out of scope.

## Human answers (2026-11-27), recorded

1. **Host scope, phase 1:** the current connection plus the saved Connections; **fleet hosts are shown DISABLED with the reason** ("Not available yet"). Fleet create/clone comes later, after an ADR-0048 amendment: not now.
2. **New folder (C2, later):** mkdir + `git init` + an empty initial commit, ticked and locked with the reason; an existing non-git folder only gets an explicit "Set it up with git init" click, never automatic. (C1 has no git-init button.)
3. **Clone destination (C3, later):** ask each time, no remembered default (at most suggest the folder being browsed); credentials are the daemon host's own git/gh login only, never tokens through the UI.
4. **Templates** need an ADR first (not planned). After create: select the project and offer "Start a task". A duplicate path does **find-or-switch**. **No native OS folder dialog** (the in-app chooser only). Mobile out of scope.

## Facts verified in the code (before any design)

- One dialog, `CreateWorkspaceDialog` (`components/crud-dialogs.tsx`), owned by the sidebar (`app-sidebar.tsx`: `setCrud({kind:"workspace"})`). Every entry point ends there: the sidebar footer "Add workspace" button (the header "+" was removed by UI cleanup B1), the sidebar empty state (header "+" and "Add project"), home "Add project", the first-run cards, the Get-started card, the palette "New workspace" and the `smind:new-workspace` window event.
- `fs.listDir {path?}` lists **sub-directories only**, hidden (dot) entries excluded, each `{name, path, isGitRepo}`; an empty path means the daemon user's home; the answer is `{path, parent, entries}`. It does not say whether the listed directory itself is a repo, so a typed path P is judged from its **parent's** listing (the entry whose path is P). A path under a hidden directory cannot be judged: no hint, the daemon decides at create.
- `workspace.create {path, title?}` validates on the daemon (absolute, exists, directory, has `.git`) and **does not reject a duplicate path** (a plain INSERT). `~` is not expanded by the daemon.
- The browse chooser's "Recent:" chips are the parent folders of existing workspaces (max 5), not real recents.
- The current connection: on desktop `desktop.getCurrentConnection()` and `listConnections()` (`{id, kind, label, baseUrl}`); on the web build there is no connection object, so the chip shows the page's own host. The desktop switches the WHOLE app to one daemon at a time (ADR-0013) with `desktop.selectConnection(id)`; that is not something to do from inside a dialog, so saved connections are listed, the current one marked, the others shown as not switchable here ("Switch the connection in Settings -> Connections").
- Fleet hosts: `useFleetHosts` (`fleet.host.list`). A peer cannot browse or create (ADR-0048), so they are rows with a disabled state.

## Decisions

- **D1 One modal, stacked steps** (not pages): "Add project", centred about 640px; at 800x520 and below the full-bleed sheet pattern (reuse the Settings dialog's small-size classes). Step 1 "Where", step 2 "How", step 3 the details of the chosen method. Step 1 is **skipped when there is one host**: it shows as a chip "On: <name>" (desktop: the connection label; web: the page host). With other saved connections or approved fleet hosts it is a short list of rows: the current host (selected), saved connections (shown, not switchable, with the reason), fleet hosts (disabled, "Not available yet").
- **D2 Method cards:** "Open a folder" is live and preselected. "Clone from Git", "New project" and "From a past session" are shown **disabled with a "Coming soon" badge** (not hidden: they show the shape of the flow; `aria-disabled`, no click handler, muted style, so nothing that looks live is dead).
- **D3 Open a folder:** a path input and "Browse..." (the existing chooser, now also with a "Home" place; the hidden-files toggle is **not** added: `fs.listDir` has no parameter for it), real recents per host, trim and `~` expansion (`~` and `~/x` expand using the daemon's home from `fs.listDir {}`; any other relative path gets the hint "Use an absolute path, or start with ~"), validation on blur and on submit, and an inline status from the parent listing: "Not a git repository" (with the plain instruction to run `git init` in that folder and add it again; **no git-init button**, that is C2), "This folder does not exist on <host>", or nothing. The submit is disabled while the path is known to be missing or not a repo.
- **D4 Recents:** client-side, persisted per host (`localStorage`, key `smind:project-recents:<host key>`; the host key is the desktop connection id, or the page origin on the web), at most 8, newest first. A project created here is recorded; on each open the host's current workspaces are merged in (so a project that is removed later **stays** in recents); "remove from recents" (a small x on the entry) deletes only the recents entry and never calls `workspace.delete`. Projects that exist right now are not listed (they are in the sidebar); removed ones are.
- **D5 Duplicate path = find-or-switch:** the normalised path (home expanded, trailing slashes trimmed) is compared with the current workspaces' paths (exact and case-sensitive; symlinks are not resolved: stated). A match shows "Already a project: <title>" with "Open it", and the primary button becomes "Open project": it selects the existing project (expands it in the sidebar and scrolls it into view) and **sends no `workspace.create`**: no second workspace on one path.
- **D6 After create (or open):** the modal switches to a done state: "<title> is ready." with "Start a task" (opens the existing New task dialog for that project) and "Done"; the project is expanded and scrolled into view in the sidebar (there is no "selected project" state in the app today, only a selected task; said plainly). A daemon error is a sentence (shared `daemonSentence`), the dialog stays open, the path and the method stay.
- **D7 Entry points:** every entry point keeps calling the sidebar's one `setCrud({kind:"workspace"})`; the component behind it is replaced. The old `CreateWorkspaceDialog` is removed with its tests (rewritten). No new global event.
- **D8** The optional "Title" field is kept (collapsed under "Name (optional)").

## Acceptance criteria

- AC1 The "Add project" modal replaces the single form; all six entry points open it.
- AC2 Step 1: one host gives a compact chip "<friendly name> <address>" only; several give rows: the current one selected, saved connections listed and not switchable, fleet hosts disabled with "Adding projects on fleet hosts isn't supported yet".
- AC3 Step 2: only "Open a folder" is live; the other three are disabled with "Coming soon".
- AC4 Recents are per host, persisted, keep removed projects, and "remove from recents" never deletes a project.
- AC5 A known non-git path shows "Not a git repository" before submit, with no git-init action and a clear next step; a missing folder says so.
- AC6 A duplicate path finds and opens the existing project; `workspace.create` is not called.
- AC7 `~` is expanded client-side; other relative paths get a hint.
- AC8 After create the project is expanded and in view, and "Start a task" is offered (it opens the New task dialog for that project).
- AC9 Daemon errors are sentences and the dialog stays open.
- AC10 At 800x520 the modal is the full-bleed sheet.
- AC11 The Browse chooser still works and gains a "Home" place.
- AC12 No daemon, wire or `mobile/` change; hygiene green.
- AC13 Frames: REAL-WINDOW (the desktop audit build) before ACCEPT, light and dark, 1280x800 and 800x520.

## Named tests

`components/add-project/add-project-dialog.test.tsx`: `add_project_host_chip_shows_the_current_connection`, `add_project_one_host_shows_only_the_chip`, `add_project_fleet_rows_are_disabled_with_the_reason`, `add_project_saved_connections_are_listed_not_switchable`, `add_project_only_open_a_folder_is_live`, `add_project_non_git_hint_before_submit_and_no_git_init_action`, `add_project_missing_folder_is_said_before_submit`, `add_project_duplicate_path_switches_instead_of_creating`, `add_project_tilde_is_expanded`, `add_project_relative_path_gets_a_hint`, `add_project_after_create_selects_and_offers_start_a_task`, `add_project_errors_are_sentences_and_the_dialog_stays_open`, `add_project_small_window_is_a_full_bleed_sheet`.
`lib/project-recents.test.ts`: `project_recents_are_kept_per_host`, `project_recents_keep_removed_projects`, `project_recents_remove_does_not_delete_the_project`.
`components/add-project/entry-points.test.tsx`: `add_project_all_six_entry_points_open_the_new_modal`.
`web/e2e/specs/create-project-c1.spec.ts`: `UC-C1-01` opens from the sidebar with the host chip and the live method, `UC-C1-02` a real git repo is added and the done state offers Start a task, `UC-C1-03` recents show after creating and a removed project stays, `UC-C1-04` the non-git hint before submit, `UC-C1-05` a duplicate path opens the existing project (one workspace), `UC-C1-06` `~` expansion, `UC-C1-07` a daemon error is a sentence and the dialog stays, `UC-C1-08` the small-window sheet.

## Mutations (committed code; each must fail a named test)

M1 a duplicate path calls `workspace.create` (find-or-switch removed); M2 the non-git hint is never shown; M3 recents are not keyed per host; M4 a fleet row is enabled.

## Frames

States: the modal with one host; the modal with fleet rows disabled (a fleet stub, labelled); Open a folder empty; with recents; the non-git hint; the duplicate "Already a project"; the Browse chooser; creating; an error; after-create with Start a task. Light and dark, 1280x800 and 800x520. REAL-WINDOW required before ACCEPT; web-harness frames while building are fine and labelled.

## Staffing

GLM peers write the code from step briefs (A the modal shell and the host and method steps; B recents, the non-git hint, duplicate and `~`; C after-create, the entry-point test and e2e). Peer-sonnet only if GLM still refuses a send (the run record quoted exactly in the PR body). The Lead writes the spec and contract, reviews, runs the mutations, takes the frames and writes the PR.

## Progress

- [x] spec and contract
- [x] peers A, B, B2, C1, C2 (all GLM)
- [x] mutations M1 to M4 (each killed)
- [ ] validator, frames (web-harness first; REAL-WINDOW before ACCEPT, the screen is locked at the time of writing)

## Validation

(filled at PR time)
