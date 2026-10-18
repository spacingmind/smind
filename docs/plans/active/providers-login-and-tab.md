# Provider login for remote daemons + Providers tab redesign

Decision: [ADR-0033](../../decisions/0033-provider-login-remote-daemons-and-account-identity.md) (login contract, device
code, paste-the-redirect, remote detection, stable account identity), accepted 2026-10-18. UI shape per the 2026-10-18
Providers study (local-only research notes). Builds on ADR-0032 (shared query/zustand stores) and ADR-0028 (usage rate limits).

Size: **two L items.** B remote login — L (7): scope 2, PRs 2, risk 2 (credentials, concurrency), decisions 0 (ADR done), verify 1.
A Providers tab — L (6): scope 1, PRs 2, risk 1, decisions 1, verify 1 (screenshots, every state).
Status: **spec**, nothing built. Supersedes the "Open issues" of PR #370 (which only fixed external-link opening).

## Ground rules
- Never complete a real OAuth login or call a real vendor endpoint in tests or screenshots. Daemon tests use `httptest`
  fake authorize/token/device endpoints (the `With…HTTPClient` / endpoint-override options already used by
  `login_coordinator_test.go`); web tests use `FakeWsClient`; screenshots stop at the opener / fake daemon.
- The OAuth callback listener stays **loopback only** (asserted by a test).
- Authorization codes, redirect URLs, device codes' secrets and tokens never appear in logs or error text.
- A needs the shared account status from `client-state-stores` PR 1 (S1+S2). It **consumes** `useAccounts()`,
  `useAccountStatus()`, `useProviderCatalog()`, `useProviderHealth()`; it adds no account/health state of its own.
  B-web (PR-B4) may be built before A but must also use those hooks if PR 1 has merged by then; else it keeps state
  local to the connect form only (never account lists).

## Acceptance Criteria

### B — remote login (Go + web connect form)
**B1 — account identity (daemon, PR-B1)**
- AC-B1 `accounts.identity` (nullable) + partial unique index `(provider, identity) WHERE identity IS NOT NULL`, via the
  store's add-column migration helper and `schema.sql`; migrating an existing DB leaves rows `NULL` and loses nothing.
- AC-B2 `Login.Exchange` results carry a subject (Anthropic `account.uuid`; OpenAI `sub`+`chatgpt_account_id` from the
  id_token); identity = hex SHA-256 of `"<provider>:<subject>"`; never logged, never on the wire except `account.list`'s `hasIdentity`.
- AC-B3 Re-login (`account.oauthStart`) per ADR-0033 D6: same identity → in-place credential replace keeping id, label,
  routing links, workspace assignments, models, quota/cooldown rows, refresh-blocked cleared, `account.updated` emitted,
  `created:false`; `accountId` + different identity → `identity_mismatch`; `accountId` + legacy NULL → adopt
  (`adopted:true`), `identity_in_use` if owned elsewhere; otherwise create (`created:true`).

**B2 — login contract (daemon, PR-B2)**
- AC-B4 `account.oauthStart` emits `authorizeUrl {loginId,url}`; `loginId` is random 128-bit, one pending login per provider,
  login lifetime 10 min; cancel (request abort) frees the listener and the per-provider claim.
- AC-B5 `account.oauthComplete {loginId,redirectUrl}`: lenient parse (full URL / bare query / `code#state`), constant-time
  `state` check, first delivery wins (listener or paste), unknown/expired `loginId` is an error, wrong `state` errors and
  keeps the login pending (max 5 attempts), URL/code never logged or echoed.
- AC-B6 The callback listener binds loopback only.
- AC-B7 Stable error codes (`login_in_progress`, `login_unknown`, `state_mismatch`, `denied`, `timeout`, `identity_*`).

**B3 — device code (daemon, PR-B3)**
- AC-B8 `method:"device"` for `openai`: emits `deviceCode {loginId,verificationUrl,userCode,expiresAt,intervalSeconds}` to the
  requester only; polls at the vendor interval, caps at 15 min, stops on cancel; `device_unsupported` on the vendor's 404;
  `device_expired`; other providers reject `device`.

**B4 — connect form (web, PR-B4)**
- AC-B9 Remote detection (ADR D4): desktop by `Connection.kind` (`local` local; `url`/`ssh`/`relay` remote), web by hostname,
  manual override persisted per connection id.
- AC-B10 Method selector (ADR D5): Claude — subscription / API key; Codex — ChatGPT / device code / API key; the old
  "Paste a credential instead" disclosure is gone; API-key path keeps label, credential, Advanced base URL.
- AC-B11 State machine Starting → Waiting → Done/Error/Cancelled with a client generation counter; stale events dropped.
- AC-B12 Panels: local → link + (desktop) auto-open + "Having trouble?" paste after ~15 s; remote Anthropic → paste field shown
  immediately; remote/choice device → big user code + copy + verification link + expiry countdown; Cancel everywhere.
- AC-B13 Outcome toasts/rows: `created:false` → "Updated '<label>' — already connected"; `adopted` → confirm before start
  (when `hasIdentity` is false on the row), `identity_mismatch` → "Connect as a new account" offer.
- AC-B14 Desktop auto-open is suppressed for remote connections where the authorize URL cannot complete without paste
  being visible (the URL is still opened — the paste field is what changes); no open of the verification URL without a click.

### A — Providers tab redesign (web; after client-state PR 1)
- AC-A1 Master/detail card in Settings → Providers (ZCode chrome: 224px nav, `rounded-xl border bg-card`, `p-6` detail); nav rows =
  `ProviderGlyph` + label + tone dot (worst account); narrow desktop → icon rail; mobile/compact → list → pushed detail.
- AC-A2 Detail = header → Accounts card → Models (read-only) → Diagnostics (collapsed, copyable plain text).
- AC-A3 Account row from `useAccountStatus`: dot, label, credential badge, plan/tightest usage window bar, health badge
  (**Needs re-login** → **Cooling down · until HH:MM · reason/window** → **Error · reason** → **Active**), last Test result + age,
  actions Test / Re-login / ⋯ (rename, update credential, remove-with-consequences). No local account/health `useState`.
- AC-A4 Per-row **Connect** starts the flow directly for that provider with a default label (`account-N`, editable, rename later).
- AC-A5 Empty state: no accounts → "Connect your first account" card with one primary button per provider.
- AC-A6 Models: read-only list from the provider catalog with search, count, last-fetched time; reuses `lib/model-browser` and the
  `model-picker-rows` primitives; no custom model ids.
- AC-A7 Copy: the Claude Code / Codex note reads "Accounts here route sMind's /v1 proxy; agent runs use the CLI's own login."
- AC-A8 GLM/CLI providers: "Managed externally via CLI" explanation + Test; no Accounts card.
- AC-A9 Composer model picker header gets "Manage provider" → opens Settings → Providers with that provider selected (`lib/settings-intent`).
- AC-A10 Test button and usage calls unchanged: zero provider usage calls from mounting Providers / selecting a provider (count test).
- AC-A11 Light + dark screenshots of every state (empty, connected, cooling down, needs re-login, error, each connect-flow state,
  narrow, mobile); existing `data-testid`s asserted by current tests migrate in the same PR.

## Test Scenarios (named; PRs add the concrete files)
**B daemon**
- `identity migration keeps existing rows null`
- `oauth exchange derives a stable identity for anthropic and openai`
- `relogin same identity replaces credential in place and keeps routing links`
- `relogin with accountId and different identity is identity_mismatch`
- `relogin adopts identity for a legacy account`
- `relogin adopt is identity_in_use when another row owns it`
- `oauthStart emits authorizeUrl with a loginId`
- `oauthComplete finishes the login from a pasted redirect`
- `oauthComplete accepts bare query and code-hash forms`
- `oauthComplete wrong state errors and leaves login pending`
- `oauthComplete unknown loginId errors`
- `first of listener callback and paste wins`
- `cancel frees listener and provider claim`
- `callback listener is loopback only`
- `login secrets never appear in logs or errors`
- `device login emits deviceCode to requester only`
- `device login polls until approved then stores account`
- `device login stops at expiry and on cancel`
- `device login 404 is device_unsupported`
- `device method is rejected for anthropic`
**B web**
- `remote is derived from connection kind and hostname`
- `manual override beats the derived remote flag`
- `method selector offers the provider's methods`
- `stale generation events are dropped after cancel`
- `remote anthropic shows the paste field immediately`
- `local login shows paste after the trouble delay`
- `device panel shows code, link and countdown`
- `relogin updated toast on created false`
- `adopt confirm appears for legacy accounts`
- `identity mismatch offers connect as new account`
**A**
- `providers nav tone is the worst account tone`
- `account row health badge order`
- `row connect starts login with default label`
- `empty state offers first account`
- `models list is read only and searchable`
- `providers copy for claude and codex`
- `manage provider from composer opens that provider`
- `selecting a provider performs zero usage refreshes`
- `narrow layouts: icon rail on desktop, pushed detail on compact`

## Implementation split (non-stacked, each off `origin/develop`, in order)
1. **PR-B1** daemon: identity column + migration + re-login semantics (M–L; Sonnet peer for the crypto-adjacent/DB parts, `reviewer` mandatory).
2. **PR-B2** daemon: `loginId`, `account.oauthComplete`, loopback assert, error codes (M; Sonnet peer, `reviewer`).
3. **PR-B3** daemon: device code for OpenAI (M; Sonnet peer, `reviewer`).
4. **PR-B4** web: connect form — remote detection, method selector, device/paste panels, generation counter (L; `peer-glm` with a detailed brief; screenshots).
5. **PR-A1** web: master/detail, accounts rows with health badge, row Connect, empty state, copy (L) — after client-state PR 1.
6. **PR-A2** web: models, diagnostics, composer deep link (M).
B1→B2→B3 are sequential (same package); B4 starts after B2 (paste) and rebases on B3 for device (or lands the device panel once B3 merges). A1 can overlap B-daemon work.

## Decisions
- 2026-10-18 (Human, "ok theo đề xuất"): 1 remote-daemon login; 2 device code for Codex, default when remote; 3 paste-the-redirect via
  `account.oauthComplete`, listener loopback; 4 remote from connection kind + manual override; 5 method selector replaces "Paste a credential
  instead"; 6 re-login updates the existing row via stable identity (ADR-0033); 7 row health badge, "Reset cooldown" later; 8 per-row Connect
  starts login (default label); 9 models read-only; 10 no custom OpenAI-compatible provider now (own ADR if ever); 11 defaults stay in Agents,
  Test unchanged, narrow = icon rail on desktop / pushed detail on compact; 12 copy "Accounts here route sMind's /v1 proxy; agent runs use the CLI's own login."
- Deferred: "Reset cooldown" (needs a daemon method), identity backfill for OpenAI from refresh responses, Kimi/xAI device flows.

## Progress
- [ ] ADR-0033 + this plan (docs PR)
- [x] PR-B1 identity (branch `feat/account-identity`)
- [ ] PR-B2 loginId + oauthComplete
- [ ] PR-B3 device code
- [ ] PR-B4 connect form
- [ ] PR-A1 Providers master/detail
- [ ] PR-A2 models/diagnostics/deep link

## Validation
(filled as PRs land)

**PR-B1 (AC-B1..AC-B3), all Go tests under `-race`:**
- `identity migration keeps existing rows null` -- `internal/store/migrate_test.go` (`TestMigrate_AccountsIdentity`, run twice for
  idempotence; also asserts the partial unique index rejects a duplicate `(provider, identity)` and allows many NULLs and the same
  identity under another provider). Store methods: `TestStore_AccountIdentity` in `internal/store/accounts_test.go`.
- `oauth exchange derives a stable identity for anthropic and openai` -- `internal/accounts/oauth_login_test.go`
  (`TestLoginExchange_Identity`, httptest token servers; stable, distinct, empty on missing subject, never the raw subject).
- `relogin same identity replaces credential in place and keeps routing links`, `relogin with accountId and different identity is
  identity_mismatch`, `relogin adopts identity for a legacy account`, `relogin adopt is identity_in_use when another row owns it` --
  `internal/accounts/login_coordinator_test.go` (`TestLoginCoordinator_Relogin`; also covers unknown/foreign `accountId` ->
  `login_unknown_account` and a subject-less login). The same-identity test asserts routing_decisions, quota_snapshots, models and
  label survive, the passive cooldown is cleared (ADR-0028 section 7), and the credential-replaced hook and `account.updated` fire.
- Wire: `TestServer_AccountOAuthStart_Relogin` and the extended `TestServer_AccountOAuthStart_HappyPath` in
  `internal/wsapi/oauth_test.go` (`created`/`adopted`, `identity_mismatch`/`login_unknown_account` through the `account.oauthStart:`
  wrapper); `TestAccount_ListExposesHasIdentityNeverIdentity` in `internal/wsapi/account_test.go` (raw JSON has `hasIdentity`, no
  `identity` key or value).
- Refresh-blocked clearing needs no new code: the quota poller's rejected-credential state is keyed on a credential fingerprint
  (`internal/quota/poller.go`), so a replaced credential unblocks by itself.
