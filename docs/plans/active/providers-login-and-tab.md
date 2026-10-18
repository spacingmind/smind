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
- AC-B7 Stable error codes (`login_in_progress`, `login_unknown`, `login_wrong_method`, `login_attempts_exceeded`, `login_bad_redirect`, `login_already_completed`, `state_mismatch`, `denied`, `timeout`, `identity_*`).

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
- [x] PR-B2 loginId + oauthComplete (branch `feat/oauth-complete`)
- [x] PR-B3 device code (branch `feat/device-login`)
- [ ] PR-B4 connect form
- [ ] PR-A1 Providers master/detail
- [ ] PR-A2 models/diagnostics/deep link

## Validation
- PR-B4 (hook): `login hook goes starting then waiting on authorizeUrl` — `web/packages/ui/src/hooks/use-account-login.test.tsx`
- PR-B4 (hook): `login hook shows device code state on deviceCode` — `web/packages/ui/src/hooks/use-account-login.test.tsx`
- PR-B4 (hook): `login hook marks pasteOnly from the event` — `web/packages/ui/src/hooks/use-account-login.test.tsx`
- PR-B4 (hook): `stale generation events are dropped after cancel` — `web/packages/ui/src/hooks/use-account-login.test.tsx`
- PR-B4 (hook): `cancel aborts the in-flight request and returns to idle` — `web/packages/ui/src/hooks/use-account-login.test.tsx`
- PR-B4 (hook): `desktop opens the authorize url once and web does not` — `web/packages/ui/src/hooks/use-account-login.test.tsx`
- PR-B4 (hook): `paste complete sends oauthComplete with the loginId` — `web/packages/ui/src/hooks/use-account-login.test.tsx`
- PR-B4 (hook): `paste errors keep the login waiting and show the message` — `web/packages/ui/src/hooks/use-account-login.test.tsx`
- PR-B4 (hook): `paste on an ended login moves to the error state` — `web/packages/ui/src/hooks/use-account-login.test.tsx`
- PR-B4 (hook): `login hook maps errors to a code, message and soft flag` — `web/packages/ui/src/hooks/use-account-login.test.tsx`
- PR-B4 (hook): `login result is upserted into the account cache and onConnected fires` — `web/packages/ui/src/hooks/use-account-login.test.tsx`
- PR-B4 (hook): `login hook never keeps the pasted url in state` — `web/packages/ui/src/hooks/use-account-login.test.tsx`
- PR-B4 (hook): `login hook prefers an explicit client` — `web/packages/ui/src/hooks/use-account-login.test.tsx`
- PR-B4 (hook): `login remote hook derives remote from the desktop connection kind` — `web/packages/ui/src/hooks/use-login-remote.test.ts`
- PR-B4 (hook): `login remote hook uses the hostname on web` — `web/packages/ui/src/hooks/use-login-remote.test.ts`
- PR-B4 (hook): `login remote hook override wins and persists per connection` — `web/packages/ui/src/hooks/use-login-remote.test.ts`

**PR-B4 (panels):**
- `device panel shows the code, link and countdown` — `web/packages/ui/src/components/connect/device-code-panel.test.tsx`
- `device panel copies the code` — `web/packages/ui/src/components/connect/device-code-panel.test.tsx`
- `device panel shows expired when the time has passed` — `web/packages/ui/src/components/connect/device-code-panel.test.tsx`
- `device panel cancel calls onCancel` — `web/packages/ui/src/components/connect/device-code-panel.test.tsx`
- `paste panel submits the trimmed address` — `web/packages/ui/src/components/connect/paste-redirect-panel.test.tsx`
- `paste panel disables finish while empty or pending` — `web/packages/ui/src/components/connect/paste-redirect-panel.test.tsx`
- `paste panel shows the error` — `web/packages/ui/src/components/connect/paste-redirect-panel.test.tsx`
- `paste panel submits on enter` — `web/packages/ui/src/components/connect/paste-redirect-panel.test.tsx`
- `done notice words created, updated and adopted differently` — `web/packages/ui/src/components/connect/login-notices.test.tsx`
- `error notice distinguishes soft from explanatory errors` — `web/packages/ui/src/components/connect/login-notices.test.tsx`
- `notices call onDismiss` — `web/packages/ui/src/components/connect/login-notices.test.tsx`
- `connect methods per provider` — `web/packages/ui/src/lib/login-methods.test.ts`
- `default connect method follows remote and availability` — `web/packages/ui/src/lib/login-methods.test.ts`

**PR-B4 (form):**
- `method selector offers the provider's methods` — `web/packages/ui/src/components/accounts-dialog.connect.test.tsx`
- `connect label defaults to account-N` — `web/packages/ui/src/components/accounts-dialog.connect.test.tsx`
- `remote openai defaults to the device method and shows the device code panel` — `web/packages/ui/src/components/accounts-dialog.connect.test.tsx`
- `remote anthropic shows the paste field immediately` — `web/packages/ui/src/components/accounts-dialog.connect.test.tsx`
- `local login shows paste after the trouble delay` — `web/packages/ui/src/components/accounts-dialog.connect.test.tsx`
- `pasted redirect calls account.oauthComplete and finishes the login` — `web/packages/ui/src/components/accounts-dialog.connect.test.tsx`
- `done notice words created and updated differently` — `web/packages/ui/src/components/accounts-dialog.connect.test.tsx`
- `api key method adds an account with the credential` — `web/packages/ui/src/components/accounts-dialog.connect.test.tsx`
- `the paste a credential disclosure is gone` — `web/packages/ui/src/components/accounts-dialog.connect.test.tsx`
- `location toggle switches remote and local defaults` — `web/packages/ui/src/components/accounts-dialog.connect.test.tsx`
(filled as PRs land)

**PR-B4 (lib):**
- `parseLoginEvent accepts authorizeUrl and deviceCode` -- `web/packages/ui/src/lib/login-events.test.ts`
- `parseLoginEvent rejects malformed events` -- `web/packages/ui/src/lib/login-events.test.ts`
- `parseLoginError splits code and detail` -- `web/packages/ui/src/lib/login-events.test.ts`
- `parseLoginError handles unknown and legacy messages` -- `web/packages/ui/src/lib/login-events.test.ts`
- `loginErrorMessage has copy for every known code` -- `web/packages/ui/src/lib/login-events.test.ts`
- `isSoftLoginError only for denied, device_expired and timeout` -- `web/packages/ui/src/lib/login-events.test.ts`
- `remote is derived from connection kind and hostname` -- `web/packages/ui/src/lib/login-remote.test.ts`
- `manual override beats the derived remote flag` -- `web/packages/ui/src/lib/login-remote.test.ts`
- `override is stored per connection id and survives a reload` -- `web/packages/ui/src/stores/login-location-store.test.ts`

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
  label and the passive cooldown survive, the credential-replaced hook does not fire, and `account.updated` does.
- Review fixes, same file unless noted (`TestLoginCoordinator_Relogin` subtests): `relogin without a subject cannot overwrite an
  identity-bearing row` (`identity_unverified`); `relogin adopt clears cooldowns because the row may be a different vendor account`
  (cooldowns cleared and hook fired on adopt); `adopt refuses a non-oauth account` (`login_not_oauth_account`).
  In `internal/accounts/oauth_login_test.go`: `openai identity requires both sub and chatgpt_account_id` and `identity hash input is
  delimiter safe` (`TestAccountIdentity_Derivation`); `exchange tolerates unexpected account and id_token shapes`
  (`TestLoginExchange_UnexpectedShapes`, also a padded base64url payload). In `internal/accounts/registry_test.go`:
  `relogin during an in-flight refresh keeps the new credential` (`TestRegistry_ReloginDuringRefresh`, the re-login waits on the
  account's refresh lock). CLI: `TestPrintLoginResult` in `cmd/smind/account_login_test.go` (`updated <label> (already connected)`).
- Wire: `TestServer_AccountOAuthStart_Relogin` and the extended `TestServer_AccountOAuthStart_HappyPath` in
  `internal/wsapi/oauth_test.go` (`created`/`adopted`, `identity_mismatch`/`login_unknown_account` through the `account.oauthStart:`
  wrapper); `TestAccount_ListExposesHasIdentityNeverIdentity` in `internal/wsapi/account_test.go` (raw JSON has `hasIdentity`, no
  `identity` key or value).
- `updateCredential clears identity` -- `internal/accounts/registry_test.go` (`TestRegistry_ReplaceCredentialIdentity`): a pasted
  credential clears the row's identity in the same UPDATE, a later OAuth login of that vendor account creates a new row (the pasted row
  is not converted), and an OAuth re-login of the same account keeps its identity. Store side: `TestStore_AccountIdentity`.
- Refresh-blocked clearing needs no new code: the quota poller's rejected-credential state is keyed on a credential fingerprint
  (`internal/quota/poller.go`), so a replaced credential unblocks by itself.

**PR-B2 (AC-B4..AC-B7), all Go tests under `-race` (`go test -race -count=3 ./internal/accounts ./internal/wsapi -run 'Login|OAuth|Oauth|Complete|Callback|Redirect|Secrets|Pasted'`). Named scenarios are `t.Run` subtests (spaces become `_` for `-run`):**
- `oauthStart emits authorizeUrl with a loginId` -- `internal/accounts/login_complete_test.go` (`TestLoginCoordinator_Complete`: 22-char
  base64url id, distinct per login, registered while waiting) and `internal/wsapi/oauth_complete_test.go`
  (`TestServer_AccountOAuthComplete`: the event is `{loginId,url}`, `url` stays first-class).
- `oauthComplete finishes the login from a pasted redirect` -- both files above; the wsapi version drives the start from one
  WebSocket and the paste from another, the paste answers `{}`, and the start request resolves with `created:true`.
- `oauthComplete accepts bare query and code-hash forms` -- `internal/accounts/redirect_parse_test.go`
  (`TestParsePastedRedirect_AcceptsBareQueryAndCodeHashForms`: full URL, fragment, schemeless, bare query, `?query`, `code#state`,
  vendor `error=`; `TestParsePastedRedirect_Rejects`: empty, 5000 bytes, garbage, `code=`, no code, bad escape, and no error ever
  echoes the input); end to end as `oauthComplete accepts bare query and code-hash forms (end to end)` in `login_complete_test.go`.
- `oauthComplete wrong state errors and leaves login pending`, `oauthComplete unknown loginId errors`,
  `oauthComplete rejects after five attempts` (malformed and wrong-state pastes both count; the 6th is `login_attempts_exceeded`, the
  login stays pending and the listener still finishes it), `oauthComplete wrong method errors`, `oauthComplete pasted error fails the
  login as denied` -- `login_complete_test.go`. Wire: `oauthComplete errors carry a code and never echo the paste` --
  `oauth_complete_test.go`.
- `first of listener callback and paste wins` -- `login_complete_test.go`: `listener then paste` and `paste then listener` (a gated
  fake `Exchange` keeps the login registered, so the loser deterministically gets `login_already_completed`) and `simultaneous` x5;
  exactly one account each time. The arbiter is `pendingLogin.resolved`, set under the coordinator mutex.
- `cancel frees listener and provider claim` -- `login_complete_test.go`: `context.Canceled`, loginId gone (`login_unknown`), port
  re-bindable, a second login for the provider starts.
- `callback listener is loopback only` -- `internal/accounts/oauth_server_test.go` (`TestCallbackListenerIsLoopbackOnly`):
  `localhost:0`, `127.0.0.1:0` (and `[::1]:0` when available) bind loopback; `0.0.0.0:0`, `:0`, `[::]:0`, `192.0.2.1:0`, a hostname
  and a port-less address are rejected; the real `anthropicCallbackAddr`/`openaiCallbackAddr` pass `checkLoopbackAddr` (parse only,
  no real port bound).
- `login secrets never appear in logs or errors` -- `internal/accounts/login_secrets_test.go`
  (`TestLoginSecretsNeverAppearInLogsOrErrors`, log and slog captured) and `internal/wsapi/oauth_complete_test.go`
  (`TestServer_AccountOAuthComplete_NoSecretsLeak`, RPC error messages and logs): code, state and pasted URL absent. Neither wsapi
  nor accounts logs request params at all (no `log.`/`slog.` call in `internal/wsapi/{conn,wsapi,server}.go` or `internal/accounts`),
  so nothing needed redacting; `account.oauthComplete`'s own param errors are fixed text (a JSON decode error is not wrapped, it can
  quote input).
- Review fixes (`login_complete_test.go` unless noted): `simultaneous` no longer fails when the paste wins and the listener closes
  under the racing GET (`tryCallback` ignores transport errors); `a paste acknowledged by oauthComplete is never dropped` (calls
  `await` directly with the paste acknowledged first and a listener result / cancelled ctx / expired timeout ready too, 200
  iterations each; removing the `pl.resolved` re-check under the mutex fails it); `oauthComplete error paste without matching state
  does not fail the login` (an `error=` paste must prove state, missing or wrong -> `state_mismatch`, login stays pending; the
  listener path is unchanged); `oauthComplete after cancel is login_unknown` (a login ended by ctx cancel or timeout reports
  `login_unknown`, never `login_already_completed`); `callback listener is loopback only` also rejects `localhost.evil:0` and
  `checkBoundLoopback` rejects a bound non-loopback address (`oauth_server_test.go`, fake listener).
  Flake check, both modes: `go test -count=10 ./internal/accounts` and `go test -race -count=10 ./internal/accounts` pass.
- AC-B7 codes `login_in_progress`, `timeout`, `state_mismatch`, `denied`, `login_bad_redirect` now come from `*LoginError` on the
  listener path too (existing tests updated to assert codes via `requireLoginCode`). ADR-0033's error list gained
  `login_wrong_method`, `login_attempts_exceeded`, `login_bad_redirect`, `login_already_completed`.
- Gate: `account.oauthStart` has no run-scoped or local-only gate today (`runScoped` is set by nothing yet, ADR-0027), so
  `account.oauthComplete` has none either; revisit when run tokens land.

**PR-B3 (AC-B8 plus two callback-listener fixes), `go test ./...`, `go test -count=10 ./internal/accounts`, `go test -race -count=10 ./internal/accounts`, `go test -race -count=3 ./internal/wsapi -run 'Login|OAuth|Oauth|Complete|Device'` and `task lint` pass. No vendor endpoint is reached: the real `OpenAILogin` talks to an httptest fake behind `testHTTPClient`.**
- Listener hardening (`internal/accounts/oauth_server.go`, `newCallbackServer` now takes the login's state): `junk callback does not end the
  login` and `callback with matching state still completes the login` -- `login_complete_test.go` (`TestLoginCoordinator_Complete`; junk =
  no state, wrong state, empty state, `error=` with and without state, `/favicon.ico`, `/`, all answered with an error status and
  ignored; the matching callback, success and `error=` forms, still completes or denies). `TestCallbackServerHardening` --
  `oauth_server_test.go` (`ReadHeaderTimeout` set; `close` returns in bounded time with a half-sent request held open and frees the
  port). The old "mismatched listener callback ends the login with `state_mismatch`" tests became: the paste-path mismatch (kept,
  `oauthComplete wrong state errors and leaves login pending`) and the secrets test now checks the listener's 400 page does not echo
  the request (`login_secrets_test.go`).
- `busy callback port falls back to paste only` -- `login_complete_test.go` (a plain listener holds the fake `CallbackAddr()`; the
  event has `PasteOnly`; `Complete` finishes the login and creates the account; the other process's listener is untouched) with
  `a bind error other than address in use still fails the login` (bad port, `0.0.0.0`, non-loopback host: plain errors, no event).
  Wire: `oauthStart flags pasteOnly when the callback port is busy` and, in `oauthStart emits authorizeUrl with a loginId`, the raw
  event has no `pasteOnly` key normally -- `internal/wsapi/oauth_complete_test.go`.
- Events: `LoginEvent{Kind: "authorizeUrl"|"deviceCode", ...}` replaces the authorize-URL callback; `handleAccountOAuthStart` emits
  `authorizeUrl {loginId,url,pasteOnly?}` or `deviceCode {loginId,verificationUrl,userCode,expiresAt (RFC3339),intervalSeconds}` with
  `rc.Emit` (requesting connection only). CLI: `printLoginEvent` prints the verification URL and code for `deviceCode` without opening
  a browser -- `TestPrintLoginEventDeviceCode` in `cmd/smind/account_login_test.go`.
- AC-B8, accounts layer -- `internal/accounts/login_device_test.go`:
  - `device login polls until approved then stores account` (`TestLoginCoordinator_Device`, fake `DeviceLogin`: 3 pending polls then
    the grant, `created:true`, identity set, credential stored, `Complete` is `login_wrong_method` while polling and `login_unknown`
    after; a second device login of the same account updates in place) and the same name in `TestOpenAIDeviceLogin_Vendor` with the real
    `OpenAILogin` against the fake vendor (403/404 pending, the poll body carries the issued `device_auth_id`/`user_code`, the exchange
    form carries the device `redirect_uri`, the vendor's authorization code and the vendor's verifier).
  - `device login stops at expiry and on cancel` (shortened `deviceTimeout`: `device_expired`, polling stops, no account; cancel:
    `context.Canceled`, loginId `login_unknown`, the provider claim is free for a second login).
  - `device login 404 is device_unsupported` (both layers: `ErrDeviceUnsupported` from the vendor's 404, `device_unsupported` from
    `Login`, claim freed, the vendor body never in the error), `device method is rejected for anthropic` (no claim, no event; the real
    `AnthropicLogin` does not implement `DeviceLogin`), `unknown method is login_bad_method`, `device login one login per provider`,
    `device relogin keeps identity semantics` (`identity_mismatch`, in-place replace), and `device login failures` (start error,
    non-pending poll error, exchange error: `device_failed` with fixed text; three transient poll errors are retried, a pending answer
    resets the count, a fourth in a row fails).
  - Vendor protocol: `start parses user_code or usercode and interval as string or number` (default 5s, clamp to [1s,30s], garbage and
    1e300 safe), `poll maps 403 and 404 to pending and others to failure` (500 is neither pending nor transient, a dead server is
    `ErrDeviceTransient`, a cancelled ctx is its own error), `device login exchange yields the browser path's result` (same subject
    derivation as the browser exchange).
  - `device login secrets never appear in logs or errors` -- `TestDeviceLoginSecretsNeverAppearInLogsOrErrors` (log and slog captured;
    success plus non-2xx, garbage and cancelled paths at all three endpoints, vendor bodies carrying distinctive secrets: user code,
    `device_auth_id`, authorization code, verifier, tokens and the body marker are absent from every error and log).
- AC-B8, wire -- `internal/wsapi/oauth_device_test.go` (`TestServer_AccountOAuthDevice`): `device login emits deviceCode to requester
  only` (two connections; the raw event has exactly `loginId, verificationUrl, userCode, expiresAt, intervalSeconds`, expiry about 15
  minutes out, no `device_auth_id` anywhere; the bystander receives nothing; cancelling the request ends the login), `device login
  finishes through account.oauthStart` (`created:true`, identity), `device method errors carry stable codes` (`device_unsupported` for a
  provider without a device flow, `login_bad_method` without echoing the value).
- ADR-0033's error list gained `device_failed` and `login_bad_method`.
- CLI: `smind account login [--device] <provider> <label>` sends `method:"device"` -- `TestParseAccountLoginArgs` and
  `TestRunAccountLoginDeviceFlag` (start params carry `method:"device"` only with the flag, the code and URL are printed, no browser)
  in `cmd/smind/account_login_test.go`; usage text in `cmd/smind/main.go`.
- `exchange errors never echo the vendor body` -- `internal/accounts/oauth_exchange_errors_test.go`
  (`TestExchangeErrorsNeverEchoTheVendorBody`, log and slog captured): the anthropic and openai browser-path `Exchange` errors keep
  `status N` and drop the body (non-200 at 400/401/500 and a garbage 200). Refresh errors (`refresh_providers.go`) still quote the
  body; they are not part of the login flow and are untouched.
- Windows: `isAddrInUse` (`oauth_server.go`) is `errors.Is(err, syscall.EADDRINUSE)` or a `syscall.Errno` of 10048 (WSAEADDRINUSE),
  used by the paste-only fallback -- `TestIsAddrInUse` in `oauth_server_test.go` (real busy port, synthetic
  `net.OpError{Err: os.NewSyscallError("bind", syscall.Errno(10048))}`, wrapped, other errnos and the loopback-guard error are
  false). `GOOS=windows go build ./...` and `GOOS=windows go vet ./internal/accounts` pass.
- Review fixes (`login_device_test.go`): `device login retries a poll that times out` (`TestLoginCoordinator_Device`: a fake
  returning the raw `context.DeadlineExceeded` once, four in a row ending `device_failed`, the caller's own deadline not retried;
  `TestOpenAIDeviceLogin_Vendor`: a vendor that hangs on the first two polls is cut off by the coordinator's `pollTimeout` and
  retried, an always-hanging vendor exhausts the 3 retries) and `device login retries 429 and 5xx polls` (429/500/502/503: three in
  a row absorbed, a fourth fails after exactly four polls; 400/401/410 fail on the first poll; `PollDevice` maps 429 and 5xx to
  `ErrDeviceTransient`, other 4xx to a plain failure, neither carries the body). The per-poll timeout was the bug: it surfaced as a
  raw deadline error and skipped the retry budget.
- Notes (also in ADR-0033 D3): an `error=` redirect without `state` is now ignored on the listener path (vendors echo state on
  errors; the user can paste the redirect), so a login whose only redirect was such an error hangs until the 10-minute timeout. In
  the paste-only fallback a process squatting on the port can see `code`+`state` but cannot use them without the PKCE verifier,
  which never leaves the daemon; a squatter on `[::1]` is not detected. Windows `WSAEACCES` (10013, excluded port ranges) still
  fails the login -- follow-up.
- Not in this PR: the web connect form (PR-B4).
