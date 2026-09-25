# Providers settings: accounts as a Settings section (ADR-0015)

Moves account management out of the standalone Accounts dialog into a
"Providers" section under Settings, grouped by runtime provider (Claude
Code / GLM / Codex / Kimi), matching ZCode/Paseo's density. Source: UX
review `docs/research/local/ux-review-providers-agents-2026-09.md` and
sketch `docs/research/local/ui-sketch-run-config-2026-09.md` (local-only
files; content below is transcribed from them, so proceed even if they're
no longer on disk).

Runs in parallel with the separate "Run-config IA" task (sidebar/toolbar/
Agents rename) — don't touch `RunConfigToolbar`, the sidebar gear icon, or
Settings → Agents here; that's the other task's surface. This plan owns
only the Providers section and its backend support.

**This plan requires a backend/wire change** (new RPCs to rename/remove an
account — today's `internal/accounts` + `wsapi` only support add/list/
test, per the UX review), which per AGENTS.md rule (d) needs an ADR before
implementation, not a unilateral decision.

## Step 0 — ADR-0015 (write first, before any code)

Write `docs/decisions/0015-account-management-rpcs.md` following the
existing ADR format (see `docs/decisions/0014-agent-profiles.md` for the
most recent example of this repo's structure/tone). It must answer, and
default to the following unless something in the existing code makes a
default wrong:

- **New RPCs:** `account.update` (rename a label; swap/replace a
  credential) and `account.remove`, alongside `account.create`/`.list`/
  `.test`. Add matching `account.updated`/`account.removed` events per
  ADR-0009's event-topic convention (mirror how ADR-0014 added
  `profile.created/updated/deleted`).
- **Credentials never round-trip.** `account.update`'s response (and
  `account.list`) must never include the credential value — same
  constraint the existing `account.create`/`.list` already honor.
- **What happens to an account's in-flight run(s) on remove.** Proposed
  default: removing an account does not stop or affect any run already in
  progress on it (routing already picked a session); it only stops future
  routing from selecting it. State this explicitly rather than leaving it
  implicit.
- **What happens to routing/quota state tied to a removed account.**
  Proposed default: delete it along with the account row (no orphaned
  quota rows) — but check `internal/routing`/`internal/store` for whatever
  the existing account-delete-adjacent behavior already assumes before
  committing to this.
- **CLI:** add `smind account rm <id>` (there's already `account add`/
  `ls`/`test`/`login` in `cmd/smind`) for parity; `account edit`/rename can
  be left for later the same way ADR-0014 deferred `profile edit` — note
  that explicitly rather than silently omitting it.
- Confirm this is additive: existing clients that only know
  `account.create/list/test` keep working unchanged.

Leave the ADR in **Proposed** status and stop implementation on Step 1+
until this plan doc's Decisions section below records the user's
sign-off on the RPC shape (ask in the same terse style prior ADRs in this
repo were confirmed — a short list of the open points, not a wall of
text). This mirrors how ADR-0013/0014 were handled earlier in this
project: draft, present the specific open questions, wait for a short
reply, then flip to Accepted.

## Acceptance Criteria (Step 1+, after ADR-0015 is Accepted)

- **Daemon:** implement `account.update`/`account.remove` (+ events) in
  `internal/accounts` and `internal/wsapi` exactly as decided in
  ADR-0015. Unit tests for both, including the credential-never-returned
  and in-flight-run-unaffected behaviors.
- **CLI:** `smind account rm <id>` (and `edit` only if ADR-0015 decided to
  include it).
- **Settings → Providers section** (replaces the standalone Accounts
  dialog; the dialog's trigger goes away, its content moves into
  Settings):
  ```
  Providers                                        [ + Connect account ]
  CLAUDE CODE                                          routing: affinity
  │ ● work-claude   OAuth · Max plan   quota 62% ▓▓▓▓▓▓░░░░  [Test] [⋯] │
  GLM (Z.ai)
  │ ● coding-plan   API key · base_url quota 30% ▓▓▓░░░░░░░  [Test] [⋯] │
  CODEX · KIMI                                    ○ not connected [Connect]
  Other accounts (no agent uses these yet): xai · antigravity  ⓘ
  ```
  - Accounts grouped by **runtime provider** (Claude Code / GLM / Codex /
    Kimi) using the existing account↔runner mapping — don't invent a new
    grouping; reuse whatever `accounts-dialog.tsx` already computes for
    this.
  - Each row: health dot, label, credential-type badge, quota bar (where
    known), `[Test]` button (existing `account.test` RPC), `[⋯]` menu
    with Rename / Update credential / Remove (new RPCs from Step 0),
    Remove asks for confirmation.
  - A provider with zero accounts shows "○ not connected [Connect]"
    instead of an empty section.
  - Accounts whose provider has no runner mapped (e.g. xai, antigravity
    today) are listed separately at the bottom with a short explanatory
    "ⓘ" tooltip, not silently hidden.
- Screenshots (light + dark, temp daemon with sample accounts across at
  least two providers, one disconnected provider, one orphaned account)
  saved to `Downloads\smind-providers\`, self-reviewed against ZCode
  density before calling this done.

## Test Scenarios

1. `smind account rm <id>` removes the account; `smind account ls` no
   longer lists it; a run already attached to that account's session
   keeps running to completion.
2. Settings → Providers: accounts render grouped under their runtime
   provider headers, in the same provider order the composer/toolbar
   uses elsewhere.
3. Click `[⋯]` → Rename on an account → label updates in the list
   immediately (via the `account.updated` event, not a manual refetch).
4. Click `[⋯]` → Remove → confirmation appears → confirm → account
   disappears from the list and from any "Use provider" pickers
   elsewhere in the app.
5. A provider with no accounts shows the "not connected" row with a
   working `[Connect]` action (opens the existing add-account flow).
6. An account whose provider has no runner (xai/antigravity) appears
   under "Other accounts", not mixed into the grouped provider sections.
7. Credential value is never present in any `account.list`/`.update`
   network response (check via browser devtools/network tab against the
   temp daemon).
8. Light/dark screenshots of the full Providers section, the ⋯ menu open,
   and the remove-confirmation state.

## Decisions

(none yet — record the user's sign-off on ADR-0015's open RPC/behavior
questions here once given, then flip the ADR to Accepted before starting
Step 1)

## Progress

(none yet)

## Validation

(fill in once every acceptance criterion above is confirmed working,
citing the test run/screenshots that proved it, then move this file to
docs/plans/completed/)
