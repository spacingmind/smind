# Dual-mode UI research: "Agents mode" vs "Editor mode"

Research doc, not a plan. Question: should smind's web UI split into an
agent-orchestration mode and a VS Code-like editor mode, and if so how?
Everything below is grounded in code actually read in this repo, in
`refs/paseo`, or in a cited web source.

Two premise corrections up front, since the rest depends on them:

- The file explorer has **no git status**. `internal/files/files.go:26-30`
  defines `Entry{Name, IsDir, Size}` — nothing else reaches the tree.
- The diff viewer is **not per-hunk**. `diff-viewer-pane.tsx:68-76` hands
  the whole unified diff to `diff2html` in `side-by-side` mode. There is
  no hunk selection, no staging, no per-file "viewed" state.

## Executive recommendation

1. **Do not build a mode switch inside the task pane.** No product that
   works does an in-shell A/B toggle; the two that ship a dual surface
   (VS Code's Agents window, Google Antigravity) ship a *second shell over
   shared session state*, which is a different and strictly later thing.
2. **Build the prerequisite instead: a panel-kind tab model.** Replace
   `App.tsx`'s four hardcoded tabs with a dynamic tab list of
   `agent | file | diff | terminal` panels plus one optional side dock.
   "Agents mode" and "Editor mode" then become focus/preset differences
   over one layout, not two code paths.
3. **Promote the sidebar to an attention rail** — per-task run status,
   pending-permission badge, unreviewed-change count. That *is* the
   orchestration dashboard, and it must stay visible in both modes.
4. **Commit flow: human-triggered `git add -A` + commit** (`task.commit`),
   no agent auto-commit-per-edit, review comments routed back to the agent
   as a prompt, and a **mandatory safety commit before archive** — today
   `git worktree remove --force` silently destroys uncommitted work
   (`internal/workspace/task.go:112`, `git.go:21-23`).
5. Per-hunk staging is out of scope: `task.diff` deliberately collapses
   staged/unstaged/untracked into one base→worktree diff, which is what
   makes review invariant to whether the agent committed.

## Comparison of products

| Product | Agent/editor relationship | Mode switch? | Isolation | Review surface | Commit model |
| --- | --- | --- | --- | --- | --- |
| **Paseo** (`refs/paseo`) | One workspace canvas. Every surface is a tab *kind* (`agent`, `terminal`, `file`, `working_diff`, `changes_tree`, `files`, `pull_request`, `commit_diff`, `browser`, `plugin`) with a declared `supportedHosts: ["main" \| "explorer"]` (`packages/app/src/panels/panel-manifest.ts`) | **No.** Agents, terminals, files and diffs all move between the Explorer dock and main panes (`docs/explorer-sidebar.md:18-21`) | git worktree per workspace (`docs/product.md`, "isolated copies (git worktrees)") | Live diff subscription (`subscribe_checkout_diff_request` → `checkout_diff_update`), GitHub-style **draft line comments** persisted per checkout (`packages/app/src/review/state.ts:6-14`) | `checkout_commit_request { cwd, message?, addAll? }` — coarse, whole-worktree. Plus push / PR create / PR merge / merge-from-base (`packages/app/src/git/policy.ts:9-25`) |
| **VS Code** (Copilot/agents) | Two shells over one session store: normal window (Chat view in sidebar + chat *editors* as tabs) and a chat-primary **Agents window** (Sessions / Customizations / Chat / Changes / Files) | **Two windows, not a toggle.** "Both surfaces share the same sessions, settings, and keybindings" | Worktree per session; "VS Code commits uncommitted changes to the session branch before it removes the worktree folder" | **Changes** view; "select a block of text in the diff to leave range-based feedback for the agent"; session rows carry a file-change stat for unreviewed edits | **Commit Changes** from the session, commit message auto-generated. Agent Merge (preview) drives a PR to mergeable |
| **Cursor 3** | **Agents Window** listing every local+cloud agent across repos; agent tabs side-by-side or in a grid | Separate window; editor stays the editor | git worktree per agent, up to 8 parallel; `/worktree` command | Per-agent worktree diff; results land as a PR | Agent pushes results as a pull request |
| **Google Antigravity** | The one mainstream product with an explicit dual surface: **Editor view** (VS Code-like, agent sidebar) and **Manager view** ("control center for orchestrating multiple agents working in parallel across workspaces") | **Yes** — and notably the switching mechanism is not documented anywhere public | Per-workspace | "Artifacts" — task lists, plans, screenshots, browser recordings — reviewed instead of raw tool calls | Not documented |
| **Windsurf Cascade** | Chat panel + inline diffs in the editor | No | Same checkout | Inline green/red diffs, per-file Accept/Reject | Gates at write time, not commit time |
| **Claude Code IDE ext.** | Chat in the sidebar, diffs in the standard three-way diff editor | No | Same checkout | Accept All / Reject All / **Accept Hunk** on hover; editing the proposal before accepting is reported back to Claude | Gates at write time; no commit UI |
| **Continue** | Agent mode shares the Chat interface | No | Same checkout | Per-change and per-file accept/reject with keybindings | None |
| **Aider** | Terminal, no editor | n/a | Same checkout | `/diff` since last message | **Auto-commits every edit** with a weak-model-generated Conventional Commits message; `/undo`; `--no-auto-commits` to opt out |
| **GitHub PR review** | n/a (the reference model) | n/a | Branch | Per-file diff, **Viewed** checkbox + progress bar, comments stay **pending** and only visible to you until submit | Batch: **Review changes** → Comment / Approve / Request changes → **Submit review** |

The pattern across all of them: **the surfaces multiply, the state model
does not.** Cursor, VS Code and Antigravity all add a second *window*;
none of them adds a mode toggle that swaps one pane's contents.

## Recommended layout model

**One task-scoped tabbed canvas with a persistent attention rail and one
optional side dock. No mode switch.**

```
┌──────────────┬──────────────────────────────────────────────────┐
│ attention    │ [agent: run 3] [src/foo.go •] [diff] [term] [+] │  main host
│ rail         ├───────────────────────────┬──────────────────────┤
│              │                           │                     │
│ ws / space   │      focused panel        │   side dock          │
│  ▸ task ●2   │                           │   (e.g. diff, or     │
│  ▸ task ⚠    │                           │    a second file)    │
│  ▸ task +14  │                           │                      │
└──────────────┴───────────────────────────┴──────────────────────┘
```

- **Attention rail** = today's `app-sidebar.tsx` tree plus per-task
  badges: live run status, pending-permission count, unreviewed changed-file
  count. Both Paseo (`AgentAttentionReason = "finished" | "error" |
  "permission"`, `packages/protocol/src/agent-attention-notification.ts:3`)
  and VS Code (per-session file-change stat for unreviewed edits) put
  exactly this information in a permanently visible list. It is the
  orchestration dashboard, and it cannot live behind a mode.
- **Main host** = a dynamic tab list, not a fixed tab strip. Tab kinds:
  `agent` (a run timeline), `file` (CodeMirror; many open at once),
  `diff`, `terminal`. Each kind declares which hosts it supports, exactly
  as Paseo's `panel-manifest.ts` does.
- **Side dock** = one extra pane host with "Open to Side". That single
  split is what makes "chat left, diff right" possible, which is the
  actual thing "Editor mode" was reaching for.
- **"Modes" become presets**, if wanted later: a preset is "open these
  kinds in these hosts, focus this one". It never unmounts the other host
  and never loses tab state.

### Rationale

1. **A mode switch duplicates every cross-cutting affordance.** Permission
   prompts must reach the human while they are reading a diff; the diff
   must reach them while they are watching the agent. Today permission UI
   is nested inside a run entry (`task-detail.tsx:121-127`), reachable only
   from the Chat tab. Under a mode switch, Editor mode needs its own
   permission surface and a "back to Agents" interrupt. Under a tab model,
   the agent tab is simply already open and its rail badge lights up.
2. **The tab registry costs about what the mode switch costs, now.**
   `App.tsx:109-128` already maps a tab value to a component; the change
   is making that list *data* instead of JSX. `internal/wsapi/conn.go`
   already "dispatches each inbound request to its handler in its own
   goroutine so one connection can have many requests in flight at once",
   so several live panels streaming at once needs no transport work.
3. **It is the reversible choice** (roadmap principle 4). A preset system
   on top of a tab registry can emulate modes later, and a second
   chat-primary shell (VS Code's Agents window, Cursor's Agents Window)
   can be added over the same tab kinds. A mode switch cannot be upgraded
   into a tab model without a rewrite.
4. **Phase 3 mobile needs one panel model, not two.** Paseo runs the same
   panel implementations through a compact shell — a full-screen Explorer
   overlay for Changes/Files that closes after a file opens
   (`docs/explorer-sidebar.md:46-51`). Desktop modes plus a separate mobile
   shell would be two models to keep in sync.
5. **The one product that shipped the literal dual-mode split
   (Antigravity) has no public account of how you move between the
   views** — a bad sign for the affordance, and a strong hint that the
   value is in the parallel-agent manager itself, not in the modality.

## Commit flow recommendation

smind's constraints, read from the code:

- A task is a worktree on a fresh branch: `git worktree add <path> -b
  <branch>` (`internal/workspace/git.go:14-16`).
- `task.diff` is base-commit→working-tree, including untracked files,
  produced by snapshotting into a throwaway `GIT_INDEX_FILE` and running
  `git diff --cached <base>` (`internal/workspace/git.go:98-140`). The base
  is recovered from the branch's oldest reflog entry (`git.go:153-164`).
  **Consequence: the diff is invariant to whether the agent committed.**
- Archiving runs `git worktree remove --force`
  (`internal/workspace/task.go:112`, `git.go:21-23`) — "archiving a task
  worktree is meant to discard it regardless of uncommitted changes".
- There is no git RPC at all. The handler table
  (`internal/wsapi/handlers.go:20-50`) has no `git.*` method.

### Recommendation: diff-review-then-commit, human-triggered, coarse

1. **Add `task.commit { taskId, message }` = `git add -A` + commit inside
   the worktree.** Same shape as Paseo's `checkout_commit_request { cwd,
   message?, addAll? }` (`refs/paseo/packages/protocol/src/messages.ts:2068`).
   Coarse is correct here specifically because `task.diff` already
   collapses staged/unstaged/untracked deliberately — per-file or per-hunk
   staging would require a second diff mode (staged vs unstaged) plus
   staging RPCs, contradicting the design recorded in
   `docs/plans/completed/web-ui-diff-viewer.md`.
2. **Do not auto-commit per edit (the Aider model).** Aider gets away with
   it because it has `/undo` and a weak model generating messages; smind
   has neither, and the branch history is the thing the human will
   eventually merge or open a PR from. Keep the agent's ability to commit
   via the shell (it already has a PTY in the task cwd) as a prompt-level
   act, not a product feature.
3. **Do add a safety commit before archive.** VS Code sets the precedent:
   for worktree sessions it "commits uncommitted changes to the session
   branch before it removes the worktree folder". Today `--force` means
   one archive click destroys unreviewed agent work with no recovery path.
4. **Route review comments to the agent, not to hand edits.** GitHub's
   mechanic — comments stay *pending* and private until one **Submit
   review** — maps directly onto Paseo's implementation, which turns draft
   line comments into a `review` agent attachment
   (`refs/paseo/packages/app/src/review/store.ts:266-279`) and sends it as
   a prompt. This is the highest-leverage piece: without it,
   review-then-commit degrades into the human fixing things by hand, which
   is the workflow smind exists to replace.
5. **Borrow the "Viewed" checkbox, not "Approve".** Per-file viewed state
   plus a progress bar answers "have I looked at all of this yet" across a
   long agent diff. There is no second reviewer here, so Approve /Request
   changes collapse into "commit" and "send review to agent".

Agent-commits-then-review is rejected as the *default*, but note it stays
cheap to allow later precisely because of the base→worktree diff: a task
whose agent committed shows the same review surface as one whose agent did
not. That property is worth protecting.

## Gap inventory

### Have

| Piece | Where | State |
| --- | --- | --- |
| Workspace/space/task tree | `web/packages/ui/src/components/app-sidebar.tsx` | Loads once per connection (`useEffect` keyed on `client`, `app-sidebar.tsx:48-95`); no live updates, no badges beyond `Task.Status` text |
| Agent run timeline, streaming | `components/task-detail.tsx`, `hooks/use-run-timeline.ts` | Per-run `run.start` + `run.attach`; history via `run.logs` |
| Permission prompts | `task-detail.tsx:143-190` | Inline in a run entry; resolves cross-connection via `permission_resolved` |
| File tree + CodeMirror editor | `components/file-explorer-pane.tsx`, `hooks/use-file-explorer.ts`, `components/code-mirror-editor.tsx` | One file open at a time (`selectedPath: string \| null`, `use-file-explorer.ts:24`); save via `file.write` |
| Task diff | `components/diff-viewer-pane.tsx` | Whole-task unified diff, `diff2html` side-by-side, manual Refresh button |
| Terminal | `components/terminal-pane.tsx` | One session per task; `terminal.list` → reattach or create; detach-not-close on unmount |
| App shell | `web/packages/ui/src/App.tsx` | Single `selectedTask`, four hardcoded tabs (`App.tsx:109-128`) |
| Reconnect + resync | `lib/reconnect.ts`, `App.tsx:66-80` | New `WsClient` identity re-runs every client-keyed hook |
| Concurrent streams per connection | `internal/wsapi/conn.go:39-56` | Many in-flight requests per socket already supported |
| Worktree isolation + base-relative diff | `internal/workspace/git.go` | Solid; the review surface's foundation |

### Missing

| Gap | Evidence | Needed for |
| --- | --- | --- |
| Tab-kind model / multiple open files / second pane host | `App.tsx:109-128` is a fixed `<Tabs>`; `App.tsx:106-107` is a `ResizablePanelGroup` containing exactly **one** `ResizablePanel` | The whole recommended layout |
| Cross-task attention (status, pending permissions, unreviewed changes) | No aggregate query exists; permission events only reach a client inside an active `run.attach` (`internal/wsapi/handlers.go:420-437`) | Agents-mode dashboard |
| Any push/subscribe RPC | Handler table has no `*.watch`/`*.subscribe`; events are emitted only within a request's own stream (`handlers.go:420`, `terminal.go:88`) | Live sidebar, live diff, notifications, Phase 3 mobile |
| Git status per file | `internal/files/files.go:26-30` — `Entry{Name, IsDir, Size}` | Explorer decorations, changed-file counts |
| Live diff | `diff-viewer-pane.tsx:53-57` refetches only on task change or button press | Reviewing while the agent works. Paseo's equivalent: `subscribe_checkout_diff_request` / `checkout_diff_update` |
| File-changed-on-disk detection / conflict handling | Nothing watches the worktree; `use-file-explorer.ts` reads once on select | The agent overwriting a file the human has open dirty. Paseo: `file-pane/conflict-alert.tsx` (`changed` / `deleted` / `checkFailed`, with Reload and Overwrite) over `file-pane/live-file/model.ts` |
| Any git write op | No `git.*`/`task.commit` in `internal/wsapi/handlers.go:20-50` | Commit flow. Paseo has commit/pull/push/PR-create/PR-merge/merge/merge-from-base (`git/policy.ts:9-25`) |
| Per-file "viewed" state, per-file collapse | `diff-viewer-pane.tsx` renders one flat `diff2html` blob | Reviewing a large diff |
| Review comments → agent | Nothing | The fix loop. Paseo: `review/state.ts` + `review/store.ts:266-279` |
| Safety commit before archive | `internal/workspace/task.go:112` force-removes | Not losing work |
| Editor preview pane | Already tracked: `docs/plans/active/web-ui-editor-preview.md` | — |
| File search / command palette | Nothing | Navigating a real repo. Paseo: `command-center/workspace-file-search.ts` |

### Smallest coherent increment

Ordered, each step shippable on its own. Steps 1–2 are frontend-only and
decide nothing architectural; step 3 onward needs maintainer answers below.

1. **Tab-kind model (frontend only).** Turn `App.tsx`'s tab strip into a
   `{ kind, key, title }[]` list with a registry mapping kind → component.
   Clicking a file row in the explorer opens a `file` tab (many at once);
   keep the tree as a docked column of the main host. Delivers "chat and
   file are peers" and "many files open" with no daemon change and no
   irreversible decision. Note `App.tsx:33-38`'s existing reasoning —
   unmounting an inactive tab is a deliberate detach-not-close contract —
   and preserve it per kind.
2. **Side dock (frontend only).** Add the second `ResizablePanel` and an
   "Open to Side" action. Delivers chat-|-diff side by side, i.e. the
   thing "Editor mode" wanted, without a mode.
3. **Live task status push (daemon).** A subscribe RPC so the rail shows
   run status and pending-permission badges across tasks without an
   attach. This is the real Agents-mode dashboard, and the first
   architectural decision (open question 2).
4. **`task.commit` + `task.gitStatus` (daemon) and a commit bar on the
   diff tab.** Plus the pre-archive safety commit.
5. **Review comments → prompt.** Per-file viewed state, line comments, one
   "Send review" that starts a run with the comments as context.

## Open questions for the maintainer

Per AGENTS.md rule (d) — these materially affect data model / public API
shape and are not decided in `docs/decisions/`.

1. **Is the tab/pane layout scoped per task or per workspace?** Paseo
   scopes tabs to a workspace (`buildWorkspaceTabPersistenceKey({serverId,
   workspaceId})`, `workspace-tabs/model.ts:58-68`), but in smind the
   worktree — and therefore files, diff and terminal — is *per task*
   (`internal/workspace/task.go:44`). Per-task tabs means N independent tab
   sets and no way to see two tasks at once; per-workspace means tasks
   themselves become tab targets. This sets the persistence key and the
   whole navigation model.
2. **Does the daemon get a subscription/push API, or does the UI poll?**
   Everything in the recommendation above (live rail, live diff, file
   conflicts, notifications) needs one. Roadmap principle 3 says the
   protocol locks in from Phase 1, and Phase 3's relay carries the same
   API to mobile, so this shape is hard to change later. Options: a
   generic `*.watch` subscription request per resource (Paseo's
   `subscribe_*`/`unsubscribe_*` pairs), one long-lived firehose per
   connection, or client polling.
3. **Where does layout state live?** Paseo persists it client-side per
   `(serverId, workspaceId)`. Server-side persistence would mean phone and
   desktop agree and layout survives a browser reset, but adds a UI-state
   table to `internal/store`.
4. **Is coarse `git add -A` commit acceptable as the permanent model, or
   is per-file / per-hunk staging a requirement?** Per-hunk needs a second
   diff mode (staged vs unstaged) and staging RPCs, which contradicts
   `task.diff`'s single-diff design.
5. **Should archive hard-require a commit or stash?** Today it force-removes
   the worktree. Options: auto-commit to the task branch (VS Code's
   behavior), refuse to archive while dirty, or keep the current
   discard-everything semantics behind a confirmation.
6. **Who writes the commit message — the human, or a generated one?**
   Aider and VS Code both auto-generate. Generating one in smind means the
   daemon making a model call through its own routing engine, which is a
   new dependency direction (`internal/wsapi` → `internal/routing`).
7. **Is the agent allowed to commit, and if so must agent commits be
   distinguishable from human ones** (trailer, author, or a marker in
   `internal/store`) so review can filter to "what I haven't seen"?
8. **Conflict policy when the agent rewrites a file the human has open
   with unsaved edits** — reload, overwrite, or block the save? Paseo
   offers Reload / Overwrite plus explicit `deleted` and `checkFailed`
   states (`file-pane/conflict-alert.tsx:11-14`). Depends on question 2.

## Sources

Local code (read directly):

- `web/packages/ui/src/App.tsx`, `components/{app-sidebar,task-detail,file-explorer-pane,diff-viewer-pane,terminal-pane,code-mirror-editor}.tsx`, `hooks/{use-file-explorer,use-run-timeline}.ts`, `lib/types.ts`
- `internal/wsapi/{handlers.go,conn.go,terminal.go,files.go}`, `internal/workspace/{git.go,task.go}`, `internal/files/files.go`
- `docs/ROADMAP.md`, `docs/plans/completed/web-ui-diff-viewer.md`, `docs/plans/active/web-ui-editor-preview.md`, `AGENTS.md`

`refs/paseo` (local read-only clone):

- `packages/app/src/panels/panel-manifest.ts`, `workspace-tabs/model.ts`, `docs/explorer-sidebar.md`, `docs/product.md`, `docs/agent-lifecycle.md`
- `packages/app/src/review/{state.ts,store.ts}`, `git/{policy.ts,diff-pane.tsx}`, `file-pane/{conflict-alert.tsx,live-file/model.ts}`
- `packages/protocol/src/messages.ts` (`checkout_*`, `subscribe_checkout_diff_*`), `packages/protocol/src/agent-attention-notification.ts`

Web:

- [Use the Agents window (Preview) — VS Code](https://code.visualstudio.com/docs/agents/run/agents-window)
- [A Unified Experience for all Coding Agents — VS Code blog](https://code.visualstudio.com/blogs/2025/11/03/unified-agent-experience)
- [Work with chat sessions in VS Code](https://code.visualstudio.com/docs/agents/sessions/chat-sessions)
- [Cursor 3 Agents Window: Parallel Agents and Worktree Isolation — AgentPatterns.ai](https://www.agentpatterns.ai/tools/cursor/agents-window/)
- [What Is Cursor 3? Agents, Worktrees, and What's New — DataCamp](https://www.datacamp.com/blog/cursor-3)
- [Google Antigravity — Wikipedia](https://en.wikipedia.org/wiki/Google_Antigravity)
- [Windsurf — Cascade overview](https://docs.windsurf.com/plugins/cascade/cascade-overview)
- [Use Claude Code in VS Code — Claude Code docs](https://code.claude.com/docs/en/vs-code)
- [How Edit works — Continue docs](https://docs.continue.dev/edit/how-it-works)
- [Git integration — Aider docs](https://aider.chat/docs/git.html)
- [Reviewing proposed changes in a pull request — GitHub Docs](https://docs.github.com/en/pull-requests/collaborating-with-pull-requests/reviewing-changes-in-pull-requests/reviewing-proposed-changes-in-a-pull-request)
