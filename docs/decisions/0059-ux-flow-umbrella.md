# 0059: UX flow umbrella — guided first run, turn summaries, a working inbox, phone pairing and push-to-approve, message search, ambient quota

## Status

**Proposed — 2026-11-09.** An umbrella: it fixes the direction, the guard rails and the PR order of six flow improvements, and defers each item's detail to its own plan under
`docs/plans/active/` when that item starts. It changes no accepted decision; where an item meets one, the section says so and asks. Every choice under
[Decisions for the Human](#decisions-for-the-human) carries a recommendation; a veto changes only the item named next to it. Nothing is built for this record.

Inputs: ADR-0019 (provider-native permissions), ADR-0028 (account usage windows), ADR-0040 (push), ADR-0045 (inbox and validator), ADR-0053 (run skeleton storage), ADR-0055
(agent questions, Proposed), ADR-0056 (who is the human on the socket, Proposed), ADR-0057 (chat fork and per-turn file undo, Proposed). The small fixes from the same review
(daemon-down copy, `?` overlay, naming, deny reason, new-file content) ship separately and are not part of this record.

## Context

The flows below work, but each makes the person do the daemon's job: find out by failing that no provider is signed in, read fourteen tool rows to learn what changed, leave the
task they are in to approve one request, type a pairing link into a phone, remember which task held an answer, and hit a quota wall with no warning. They are independent, so each
is its own PR line; they share three guard rails.

## Guard rails (apply to every item)

1. **No new way to approve.** A permission, a question or a merge is answered only by an authenticated human surface through the RPCs that exist; every new surface calls the same
   handler and shows the same detail as the card. Nothing here is callable by a run-scoped connection: each new human-only handler calls `refuseAgent` like the existing ones (ADR-0017) and gets a refusal test.
2. **Derive, do not store.** Readiness, summaries and warnings are computed from state the daemon already holds; the only new persisted state is the search index (item E).
3. **Frames and tests.** Every UI PR carries light and dark frames at 1280 and 390 for each state, and the named tests of its plan.

## A. Guided first run (web and desktop)

**Today.** Home disables the composer and says "Add a project to start a task" (`web/packages/ui/src/components/home/home-landing.tsx`, `placeholderFor`, lines 42-46); it never
says that no provider works. The only guided first run is desktop-only: `components/first-run/first-run-gate.tsx` reads the desktop setting `onboardingCompleted` and the web build
has no equivalent. Provider readiness already exists as data (`useProviderHealth`, `provider.test`, the "N of M healthy" footer row) and Settings > Providers has a
"Connect your first account" card (`components/providers/first-account-card.tsx`). Adding a project is a typed path with a folder browser over `fs.listDir`.

**Proposal.** A "Get started" card on the home landing, shown only while there is no project or no ready provider. Its rows are computed live: *Sign in to a provider* (status word
per provider: Ready, Not installed, Signed out, from `provider.test`) and *Add a project* (opens the existing dialog). Dismissible per browser (local storage), re-openable from
Settings > General. The Providers empty state leads with the one action and moves proxy wording to an advanced section.

**Wire.** None. **Security.** `provider.test` is the existing local check (no network); nothing new is read from disk. **Tests.** checklist reflects real readiness; hides when both
are met; dismiss and re-open; no provider call on render. **PR.** one web PR, size S-M. Desktop keeps its own first-run screen.

## B. Turn summaries with diff stats in the run view

**Today.** The `overview` detail level collapses a run of tool calls into one row reading "N tool calls" plus the tool names (`components/timeline/tool-group-row.tsx`); the
concise tool card is "never the command, never a diff" (`components/timeline/concise-tool-card.tsx`). So no row says what changed. The raw tool input is on the client already (`TimelineToolCallItem.input`).
ADR-0053 specifies a per-run `diffStat {files, +, -}` captured at run end and ADR-0057 a per-turn record of the files a turn wrote; neither is built (`internal/store/migrate.go` has
no diff-stat column).

**Proposal.** Phase 1, client only: the group header becomes a sentence from counts ("edited 3 files, ran 2 commands, read 5 files") and an Edit or Write row shows "+x −y"
derived from the tool input, marked as the tool's own numbers. The Overview/Detailed switch becomes a labelled segmented control. Phase 2, when ADR-0053/0057 land: the turn
footer shows the authoritative "N files changed +a −b" from the recorded diff and links to the Diff tab. Mid-run steering (send while running) is ADR-0021's amendment, not this one.

**Wire.** Phase 1 none; phase 2 reads the field those ADRs add. **Security.** Diff counts only, never content; a sensitive path shows its count and no text (the same redaction as
the permission detail). **Tests.** sentence per mix of tools; counts for Edit, Write and multi-edit; unknown tools fall back to "N tool calls"; narrow layout. **PR.** one web PR
(phase 1), size M; phase 2 rides ADR-0053/0057.

## C. A working inbox (inline approve, one primary action, 390 layout)

**Today.** The inbox is a full-screen view that replaces the workspace (`App.tsx`, `activeView === "inbox"`). A permission row already answers through the same card, but only after a "Review request" expand; the row itself offers
"Open task", and a row can carry up to five text buttons (Review request, Open task, Mark seen, Snooze, Resolve; the last two are hidden for permissions) that wrap badly at 390
(`components/inbox/inbox-screen.tsx`). Methods are `inbox.list`, `inbox.count`, `inbox.seen`,
`inbox.snooze`, `inbox.resolve`; permission items carry the run and request ids. ADR-0045 states that smind never merges and that there is no merge RPC and no merge button.

**Proposal.** (1) The inbox opens as a side panel over the current view; the full page stays for deep work. (2) One primary action per row by kind: a permission row shows Allow and
Deny without the expand, calling `run.respondPermission` with the same detail summary as the card (and the optional deny reason); a question row opens its card (ADR-0055); a verdict or review
row opens the task or its PR link; a failed run offers Retry. Seen, snooze and resolve move to an overflow menu with a one-line definition each. (3) At 390 the actions stack under
the text with 44 px targets. **No merge control is added** (D2).

**Wire.** None. **Security.** Inline approve is the card's call, so ADR-0056's principal rules apply unchanged; no multi-select approve, no default key for Allow. **Tests.** inline
Allow and Deny send exactly the card's params; a row of another kind has no Allow; panel keeps the task mounted; 390 frames. **PR.** web PR size M, then the mobile inbox row (M).

## D. Phone pairing and push-to-approve

**Today.** `smind relay offer` already draws a QR on a terminal and writes a PNG (`cmd/smind/relay.go`, `--qr`, `--png`), so a QR exists on the CLI. What is missing is the rest of the
path: the phone only pastes a link (`mobile/src/screens/PairingScreen.tsx`), `mobile/package.json` has no camera module, no RPC mints an offer (the `relay.*` RPCs are the local-only
`relay.device.list|approve|revoke|rename|window`; `window` sets the approval window and device limit, it does not mint a link), and the web and desktop UI cannot show one; the desktop add flow does show the relay address and key fingerprint for confirmation
(`components/settings/add-relay-link.tsx`). Push is end-to-end sealed (`internal/push/seal.go`): the blob names the task and run and the body reads "Needs approval: <summary>"
(`internal/push/notifier.go`); a tap opens the task screen (`mobile/src/push/open.ts`), where the card is a transcript item (`TaskDetailScreen.tsx`). The phone already suppresses a banner
for the task it has open (`mobile/src/push/foreground.ts`); no focus signal reaches push: the web client reports the focused task for git polling only (`task.gitFocus`, `internal/wsapi/git_handlers.go`), and `internal/push` has no presence code.

**Proposal.** (1) A human-only, local-or-session-only `relay.offer` RPC with the CLI's window and device-limit parameters, and a Settings > "Pair a phone" panel showing the QR, the
copyable link, the expiry and the pending devices (approve and revoke exist). (2) Mobile: scan with the camera, then a confirmation sheet with the relay host and key fingerprint before
anything is saved; paste stays. (3) Push tap goes straight to the pending card (scroll to it) and the inbox row approves inline (item C). **No Allow button on the notification itself**
(D3). Presence suppression for a focused desktop client is deferred (D4).

**Coordination with ADR-0056.** Its option 2 would make the phone the second channel that confirms a human-only call made on an unproven local connection. This item does not decide
that: it only provides the pairing and the deep link that option would use, and the confirmation would be a separate push kind designed in ADR-0056's own PR, never an approve action
on an ordinary permission push.

**Wire.** one new method (`relay.offer`), refused to run-scoped connections and not an MCP tool; the push event shape is unchanged (it already has `taskId` and `runId`). **Security.**
the offer link carries the relay host's admission secret: shown only on explicit request, never logged, expires, and the panel repeats the CLI's "treat this link like a password"
warning; scanning adds a native camera module (dev-client rebuild, permission prompt). **Tests.** `relay.offer` refused for a run-scoped connection and for a non-human principal;
offer parameters and expiry; confirmation sheet shows host and fingerprint and saves nothing on cancel. **PR.** daemon + web (M), mobile scan and sheet (M, native), tap-to-card (S).

## E. Message-content search

**Today.** Search matches task titles, branches and files only (the palette sources in `web/packages/ui/src/palette/`, `hooks/use-task-search-index.ts` for paths). ADR-0053 keeps,
forever, every user prompt (16 KiB cap) and the coalesced agent answers (32 KiB cap) as rows in `run_events`, and `runs.final_text` for pruned runs. `modernc.org/sqlite` is the
driver (`go.mod`).

**Proposal.** A daemon-side SQLite full-text index over prompts and answers, fed when those rows are written and rebuilt at migration, and a human-only `task.searchMessages
{query, limit}` returning task, chat, run, a short snippet and the match position. The palette gets a "Messages" group; a result opens the task at that run. Thinking and tool output
are not indexed (not kept by ADR-0053 either).

**Wire.** one new method. **Security.** the index lives in `smind.db` next to the rows it indexes, adds no new copy outside it, and a result never returns more than the snippet; the
method is refused to run-scoped connections (an agent must not search the human's other conversations) and carries a result and query cap. A task archived or deleted drops its index
rows with it. **Tests.** index follows insert, prune and delete; a skeleton run is findable by prompt and answer; ranking and snippet bounds; run-scoped refusal; migration rebuild on a
database with history. **PR.** daemon (M) then web group (S); mobile later.

## F. Ambient quota with a pre-limit warning

**Today.** Plan usage (`account.usage`, windows with used percent and reset) is read through a cache with a call budget (ADR-0028, `internal/wsapi/account_usage.go`). The Human set it
to show on hover per chat and in Settings, with the footer item off by default (ADR-0028, second round). A run that hits the wall gets the failure class with its reset
(`plan_window_exhausted`, with "Wait until HH:MM" beside Retry); the meter says "Near limit" only when the provider itself reports `allowed_warning`, and there is no proactive notice.

**Proposal.** Keep the footer off by default (ADR-0028 stands) and add one non-blocking, dismissible notice next to the composer when a window the current provider's account uses
reaches 90 % used, naming the window and its reset ("5-hour window 92 % used, resets 20:52"), once per window per level until the window resets. The meter copy says "used" or "left"
and the age of the reading. A rate-limited run lists "Wait until <time> (in 2 h 14 m)" first and Retry second (today they sit side by side), and offers the other account when one exists.

**Wire.** none: computed from the cached `account.usage` reports and its existing event, with no extra provider call (the call-budget tests of ADR-0028 must still pass, counting real
requests). **Security.** reads only fields the wire already carries. **Tests.** threshold fires once per window and level; no notice below threshold or with a stale or missing
reading; dismissal resets with the window; no added provider calls. **PR.** web PR size M; mobile later.

## PR order

1. A (S-M), the first thing a new user sees. 2. C web (M) and B phase 1 (M), independent. 3. F (M). 4. E (M+S). 5. D (M+M+S) last: the largest, the only one with a native module,
and the one that touches ADR-0056's question. Each item gets its plan (acceptance criteria and named tests) when its PR starts; at most one UI PR of this record is open at a time.

## Decisions for the Human

- **D1 (item A): project discovery.** Recommendation: no disk scan in the first run; the folder browser stays, and "recent projects" comes from what smind already knows. Alternative: an
  opt-in scan of the home directory for repositories (privacy and time cost).
- **D2 (item C): merge from the inbox.** Recommendation: no merge control anywhere (ADR-0045 stands); a verdict row opens the PR. Alternative: add a merge RPC and button, which
  reverses ADR-0045's "never merge" line and would need its own ADR.
- **D3 (item D): approve from the notification.** Recommendation: the notification only opens the pending card; approval happens in the app after the phone is unlocked. Alternative:
  an Allow action on the notification (can run without an unlock on some device settings, and ADR-0056 has not settled who counts as the human).
- **D4 (item D): presence suppression.** Recommendation: defer; ship deep link and inline inbox approve first. Alternative: a client-to-daemon "focused task" signal so a phone stays
  quiet while the desktop shows that task (it can reuse the existing `task.gitFocus` report, but adds a failure mode: a stale focus silences a real request).
- **D5 (item F): threshold.** Recommendation: 90 % used, once per window. Alternative: also at 100 % imminent, or a configurable level (a setting the Human then has to explain).
- **D6 (item B): source of the numbers.** Recommendation: client-derived "+x −y" now, replaced by the recorded diff when ADR-0053/0057 land, labelled as the tool's own count until
  then. Alternative: wait for the recorded diff (nothing visible for several PRs).

## What we are NOT doing

- No automatic approval of anything, and no new principal: the inbox, the notification and the phone call the handlers that exist.
- No change to ADR-0028's footer default, ADR-0045's no-merge rule or ADR-0053's retention; items that touch them are decisions above.
- No search over thinking or tool output, and no index outside `smind.db`.
- No telemetry: readiness, warnings and summaries are computed locally.
