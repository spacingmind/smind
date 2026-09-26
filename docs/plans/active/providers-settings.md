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
  default: removing an account does not stop or fail any run already in
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

- **Daemon:** implement `account.rename`/`account.updateCredential`/
  `account.remove` (+ events) in `internal/accounts` and `internal/wsapi`
  exactly as decided in ADR-0015. Unit tests for both, including the
  credential-never-returned and in-flight-run-unaffected behaviors.
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
    Remove asks for confirmation **with the ADR-0015 remove-warning copy
    (running-task count / last-account callout) when runs are using the
    account**.
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
   keeps running to completion (per ADR-0015: via failover to a sibling
   account; if it was the provider's last account the next proxied
   request 503s — test both branches).
2. Settings → Providers: accounts render grouped under their runtime
   provider headers, in the same provider order the composer/toolbar
   uses elsewhere.
3. Click `[⋯]` → Rename on an account → label updates in the list
   immediately (via the `account.updated` event, not a manual refetch).
4. Click `[⋯]` → Remove → confirmation appears (**with the
   running-task warning when applicable**) → confirm → account
   disappears from the list and from any "Use provider" pickers
   elsewhere in the app.
5. A provider with no accounts shows the "not connected" row with a
   working `[Connect]` action (opens the existing add-account flow).
6. An account whose provider has no runner (xai/antigravity) appears
   under "Other accounts", not mixed into the grouped provider sections.
7. Credential value is never present in any `account.list`/`.rename`/
   `.updateCredential` network response (check via browser devtools/
   network tab against the temp daemon).
8. Light/dark screenshots of the full Providers section, the ⋯ menu open,
   and the remove-confirmation state.

## Decisions

User sign-off on ADR-0015's open questions, 2026-09-25 — ADR flipped to
**Accepted** the same day:

1. **RPC split:** two RPCs — `account.rename` + `account.updateCredential`
   (not one PATCH-y `account.update`). Both emit `account.updated`.
2. **In-flight semantics on remove:** hard delete with explicit cascade.
   In-flight runs fall over to another account of the same provider, or
   fail with 503 if it was the last one. Two conditions:
   - (a) The Remove confirmation in the UI must warn when runs are
     currently using the account ("N running tasks will switch to another
     account" / "will fail: this is the last `<provider>` account").
   - (b) The cascade must also clear routing session-affinity rows, so no
     session stays pinned to the deleted account.
3. **CLI rename:** no CLI rename/edit in v1; editing is only via web
   Settings. (`smind account rm <id>` still ships, per the ADR.)
4. **Running-task count for the Remove warning (2026-09-26): no new RPC.**
   Runs carry a runner `provider`, never an account id (ADR-0015 fact 1:
   routing is per-request), so "how many running tasks use this account"
   is honestly answerable only at provider level -- which the warning copy
   already reflects ("N running tasks will switch to another account" /
   "will fail: this is the last `<provider>` account"). The UI computes
   both branches from data the Providers section already loads:
   `run.list` (running runs + their provider), `provider.list`
   (`accountProvider` maps claude-native→anthropic, kimi→kimi,
   codex-native→openai), and `account.list` (sibling count for the
   last-account branch). ADR-0015's rejected-alternatives section already
   declines `account.remove`-side readiness info, and an `account.usage`
   RPC would add wire surface for a join three existing responses already
   give. Revisit only if the confirmation needs per-account precision
   (which today's daemon cannot even represent).

## Progress

- 2026-09-25: ADR-0015 written, open questions answered (above), ADR
  Accepted. Step 1+ unblocked.
- 2026-09-26 (chunk 1, backend): store/registry rename +
  credential-swap (type may change) + hard-delete cascade
  (routing/session-affinity, quota snapshots, workspace links), with
  `account.updated`/`account.removed` notifier events; wsapi
  `account.rename`/`account.updateCredential`/`account.remove` wired
  (credential parsing shared with `account.add` via
  `accounts.Registry.ParseCredential`); `smind account rm <id>` CLI.
  Tests cover not-found, empty label, credential-never-echoed (RPC result
  and event payload), and the affinity-cascade guarantee.
- 2026-09-26 (chunk 1, usage warning): decided client-side from existing
  RPCs -- see Decisions 4.

## Validation

(fill in once every acceptance criterion above is confirmed working,
citing the test run/screenshots that proved it, then move this file to
docs/plans/completed/)
