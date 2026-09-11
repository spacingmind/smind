# OAuth account login flow

Replaces the "paste a credential JSON blob" UX (both `smind account add
<provider> <label> < credential` and the CRUD-UI accounts dialog from
PR #69) with a real browser-based OAuth login for the two providers smind's
proxy actually routes traffic to today: `anthropic` and `openai`. The manual
paste path stays as-is for headless/scripted setups — this is additive.

Prompted by comparing against `refs/cliproxyapi` (MIT-licensed, reference
only), which implements this exact flow for the same OAuth client IDs smind
already uses to *refresh* tokens (`internal/accounts/refresh_providers.go`).
What's missing in smind is the initial authorization step that produces the
first access/refresh token pair.

## Acceptance Criteria

- New `internal/wsapi` method `account.oauthStart` (params: `provider`,
  `label`):
  - Only `provider` in `{"anthropic", "openai"}` is accepted this pass —
    these are the two providers `internal/server/proxy.go` actually
    routes live traffic through (`providerAnthropic`/`providerOpenAI`).
    Any other value is a clear invalid-params error, not a silent no-op.
  - Generates a PKCE code verifier/challenge (S256) and a random `state`,
    builds the provider's authorize URL, and opens a temporary local HTTP
    listener bound to that provider's **vendor-fixed** redirect URI —
    not smind's choice, and not configurable:
    - anthropic: `http://localhost:54545/callback`
    - openai: `http://localhost:1455/auth/callback`
    (Confirmed against `refs/cliproxyapi/internal/auth/claude/anthropic_auth.go`
    and `.../auth/codex/openai_auth.go`, which use the same client IDs
    already hardcoded in `internal/accounts/refresh_providers.go`.)
  - Blocks (bounded wait, e.g. 5 minutes) until the vendor redirects back
    to that listener with `code`+`state`, validates `state`, exchanges
    `code` for tokens via the provider's existing `TokenURL` (reusing the
    per-provider HTTP client machinery `AnthropicRefresher`/
    `OpenAIRefresher` already use — Firefox/Chrome uTLS fingerprints
    respectively — but with `grant_type=authorization_code` + PKCE
    verifier instead of `refresh_token`), and stores the result via
    `registry.AddOAuth(provider, label, cred)`.
  - Returns the new account in the same credential-free shape
    `account.add`/`account.list` already return.
  - On timeout, the listener is torn down and a clear timeout error is
    returned — not a hang.
  - A second `account.oauthStart` for a provider that already has a flow
    in progress returns a clear "already in progress" error, not a
    confusing port-bind failure.
  - The temporary listener is torn down in every exit path (success,
    timeout, daemon shutdown mid-flow) — no leaked goroutine or bound
    port.
- CLI: `smind account login <provider> <label>` calls `account.oauthStart`,
  prints the authorize URL, best-effort opens it in the system browser
  (always print the URL too — the daemon may be headless/SSH'd into), and
  on success prints the account line in the same format `account add`
  uses today.
- Web UI: `accounts-dialog.tsx` gets a "Connect" action per known OAuth
  provider (anthropic, openai) that triggers the same RPC, shows a
  pending state with the authorize link visible (in case the browser
  can't auto-open — e.g. daemon on a different host than the browser),
  and refreshes the account list on success. The existing raw-paste form
  stays for providers without a wired login flow yet.
- `docs/ROADMAP.md` updated: the UI/UX audit's "accounts dialog is
  paste-only" gap is closed; the long-standing "not yet exercised with a
  real provider account" note in Phase 1 gets resolved if the manual
  real-login validation below is actually completed.
- Fix a pre-existing bug surfaced while building this: the accounts
  dialog's manual "Add account" provider dropdown is populated from
  `provider.list` (`taskrunner.SupportedProviders()`: `claude-native`,
  `glm`, `kimi`, `codex-native` — task-execution backends), but
  `internal/server/proxy.go` matches accounts against `"anthropic"` /
  `"openai"` (the account-credential vocabulary from
  `internal/accounts/refresh_providers.go`). Nothing validates
  `account.add`'s `provider` field, so an account added via the current
  UI dropdown silently never matches the proxy's routing lookup. Fix:
  the manual-add dropdown should offer the account-credential provider
  IDs (`anthropic`, `openai`, plus `kimi`/`xai`/`antigravity` for
  completeness even though unrouted), not `provider.list`'s task
  providers — these are two different vocabularies and the dialog was
  using the wrong one.
- Out of scope this pass: Kimi/xAI/Antigravity login (their refreshers
  exist but aren't wired into `proxy.go`'s live routing yet — follow-up
  once they are), account removal/re-auth-in-place, and any change to
  remote/mobile-relay operation (this flow assumes the daemon and the
  browser share `localhost`, which doesn't hold once Phase 3+'s mobile
  relay exists — noted as a known limitation, not solved here).

## Test Scenarios

- Go: PKCE generation — verifier/challenge use the S256 method, correct
  charset/length per RFC 7636.
- Go: `account.oauthStart` happy path against a fake token endpoint
  (`httptest.Server` standing in for `platform.claude.com` /
  `auth.openai.com`) — validates `state`, exchanges `code`, stores an
  `OAuthCredential` via the registry, returns a credential-free result.
- Go: callback with a mismatched `state` is rejected, not blindly trusted.
- Go: no callback arrives before the timeout — returns a clear timeout
  error; the listener is confirmed closed afterward (e.g. by successfully
  rebinding the same port in-test).
- Go: concurrent `account.oauthStart` for the same provider while one is
  pending returns "already in progress" — no second listener, no crash.
- Go: daemon shutdown mid-flow cleans up the listener (check under
  `go test -race`).
- Go: CLI dispatch test for `smind account login`, mirroring the existing
  `account add` dispatch test's fake-WS-server pattern.
- Web: manual-add provider dropdown offers account-credential provider
  IDs (anthropic/openai/kimi/xai/antigravity), not `provider.list`'s
  task-execution providers — regression test for the vocabulary bug.
- Web: `accounts-dialog.test.tsx` — clicking "Connect" calls
  `account.oauthStart`, renders the pending authorize-link state, and
  refreshes the list on resolution (FakeWsClient, no real browser).
- Manual: a real login against a real Anthropic account and a real OpenAI
  account, end-to-end — this is the actual fix for the roadmap's
  long-standing "not yet exercised with a real provider account" gap.
  Record honestly in Validation whether this was actually done; it
  requires real vendor accounts and can't be faked.

## Decisions

- Flow lives in the daemon, not the CLI process: both the CLI and the web
  UI need to trigger the same login, and only the long-running daemon can
  reliably own the temporary listener and hold the PKCE verifier/state
  between the authorize redirect and the callback landing.
- Redirect URI/port per provider is vendor-fixed, not smind's choice —
  binding anywhere else will make the vendor reject or mis-deliver the
  callback. The daemon opens a listener there only while a flow is
  in-flight; it is not smind's regular port (4648).
- Scope this pass: `anthropic` + `openai` only, matching what's actually
  wired into `proxy.go`'s live routing today.
- The existing manual `account add < credential` path is untouched —
  kept for headless/scripted setups and for providers without a wired
  login flow.

## Progress

- [x] PKCE + state generation
- [x] `account.oauthStart` (anthropic)
- [x] `account.oauthStart` (openai)
- [x] CLI: `smind account login`
- [x] Web: accounts-dialog "Connect" action
- [x] Tests
- [ ] Manual real-provider login — anthropic DONE (2026-09-11, via imported
      Claude Code credential + a real end-to-end run); openai still pending,
      see Validation
- [x] ROADMAP update
- [x] Verification

## Implementation notes / deviations

- `Login.Exchange`'s signature ended up as `Exchange(ctx, code, state string, pkce PKCECodes)`,
  not the `Exchange(ctx, code string, pkce PKCECodes)` sketched in this plan's
  "What to build" section. Reason: the *verified* Anthropic token-exchange
  request body (confirmed against `refs/cliproxyapi`) includes `state` as a
  JSON field alongside `code`/`code_verifier`/etc., but OpenAI's
  form-encoded body never sends `state` at all. Keeping `Exchange`
  state-unaware (as sketched) would have made it impossible for
  `AnthropicLogin.Exchange` to send that field without smuggling state
  through some other channel (e.g. a struct field written by `AuthorizeURL`
  and read back by `Exchange`, which is stateful/order-dependent and worse).
  Passing `state` through explicitly is a small, mechanical addition, not a
  deviation from any of the verified wire details themselves -- every
  request body/header called out in this plan's "Verified OAuth details"
  section is implemented and test-asserted exactly as specified (see
  `internal/accounts/oauth_login_test.go`'s `TestAnthropicLogin_Exchange`
  and `TestOpenAILogin_Exchange`, which assert the JSON-vs-form body shape,
  the exact field names, and that OpenAI's request has no `state` field).
- `LoginCoordinator`'s 5-minute callback timeout lives in the named const
  `loginCallbackTimeout`, per the plan, but `LoginCoordinator` also carries
  an unexported `timeout` field (defaulted from that const in both
  constructors) purely so this package's own tests can shorten it instead
  of a real 5-minute wait -- not exposed as a constructor option, since no
  real caller has a legitimate reason to want a different value.

## Validation

- **`account.oauthStart` provider restriction (anthropic/openai only,
  clear error otherwise)**: `internal/accounts/login_coordinator_test.go`'s
  `TestLoginCoordinator_Login_UnsupportedProvider`;
  `internal/wsapi/oauth_test.go`'s
  `TestServer_AccountOAuthStart_UnsupportedProvider` (wire-level, through a
  real WebSocket connection). `NewDefaultLoginCoordinator` in
  `internal/accounts/login_coordinator.go` wires exactly `anthropic` and
  `openai`, matching `internal/server/proxy.go`'s `providerAnthropic`/
  `providerOpenAI` routing.
- **PKCE (S256) + random state, vendor-fixed listener addresses, blocking
  wait for the callback, state validation, code exchange via the existing
  refresher machinery, `registry.AddOAuth` persistence, credential-free
  result shape**: `internal/accounts/oauth_login_test.go` (`TestGeneratePKCECodes`
  — verifier/challenge shape and independently-recomputed S256 derivation;
  `TestGenerateState`; `TestAnthropicLogin_AuthorizeURL`/
  `TestOpenAILogin_AuthorizeURL` — every documented query param, including
  the vendor-fixed `redirect_uri`; `TestAnthropicLogin_Exchange`/
  `TestOpenAILogin_Exchange` — exact request body/headers against a fake
  token endpoint, reusing `refresh_providers_test.go`'s
  `rewriteTransport`/`testHTTPClient` pattern) and
  `internal/accounts/login_coordinator_test.go`'s
  `TestLoginCoordinator_Login_HappyPath` (state validated, code exchanged,
  `registry.AddOAuth` persists it, `Account` returned carries the
  credential -- the internal type may, only `wsapi`'s `accountResult` must
  be credential-free) plus `internal/wsapi/oauth_test.go`'s
  `TestServer_AccountOAuthStart_HappyPath` (same flow end-to-end through a
  real WebSocket connection, asserting the wire response never contains
  the access/refresh token strings).
- **Timeout torn down cleanly, clear error**:
  `TestLoginCoordinator_Login_Timeout` -- asserts the error message and
  that the callback address can be rebound immediately after.
- **Mismatched state rejected**: `TestLoginCoordinator_Login_StateMismatch`.
- **Concurrent same-provider login → "already in progress", no leaked
  listener**: `TestLoginCoordinator_Login_ConcurrentSameProvider`.
- **Context cancellation mid-flow tears the listener down (`-race`)**:
  `TestLoginCoordinator_Login_ContextCancelled`; the whole
  `internal/accounts` and `internal/wsapi` suites were run with
  `go test -race` (see below) and passed clean, no goroutine/listener leaks
  detected.
- **CLI `smind account login`**: `cmd/smind/account_login_test.go`'s
  `TestRunAccountLoginDispatchesOAuthStartAndPrintsAccount` (against a
  hand-rolled fake WS server, not a real `LoginCoordinator` -- deliberately,
  since the real one would actually try to bind the vendor's fixed
  localhost port and block for a browser that will never arrive in a unit
  test) -- confirms the exact RPC/params, that the authorize URL from the
  streamed event is printed, and that the final line matches `account add`'s
  own `%d\t%s\t%s\t%s\n` format; `TestRunAccountLoginUsage` for the
  arg-count usage errors. Browser auto-open (`openBrowser` in
  `cmd/smind/account.go`) is best-effort by construction -- there is no
  `xdg-open`/`open` binary in this environment, so its failure path (log to
  stderr, don't fail the command) is exercised as a side effect of every
  CLI test run here, not a dedicated assertion.
- **Web `accounts-dialog.tsx`**: `accounts-dialog.test.tsx`. The
  provider-vocabulary regression test opens the manual-add `Select` and
  asserts its five options are exactly `anthropic`/`openai`/`kimi`/`xai`/
  `antigravity` (by label) and that `provider.list` is never called from
  this component anymore. Separate tests cover: Connect requires a label
  first; Connect calls `account.oauthStart` with `{provider, label}`; the
  authorize link renders once the `authorizeUrl` event fires; the account
  list refreshes via `account.list` once `account.oauthStart` resolves; and
  a daemon error from `account.oauthStart` surfaces inline, mirroring
  `add()`'s existing error handling. (Fixing this test uncovered that
  jsdom has no `Element.scrollIntoView`, which Radix's `Select` calls
  unconditionally on open -- added a no-op stub to
  `web/packages/ui/src/test/setup.ts`, the same pattern already used there
  for `matchMedia`/`ResizeObserver`.)
- **ROADMAP update**: `docs/ROADMAP.md`'s Phase 1 section now describes
  `smind account login` and the accounts dialog's Connect button alongside
  the pre-existing paste-only path, and is explicit that real-provider
  end-to-end validation is still outstanding. Searched
  `docs/research/uiux-audit.md` for the "accounts dialog is paste-only"
  wording this plan's Acceptance Criteria expected to find and close there
  — it isn't present in that file (that audit doesn't mention accounts at
  all); nothing to close in it, so nothing was added there. The gap that
  *does* exist verbatim in `docs/ROADMAP.md` (Phase 1's "not yet exercised
  with a real provider account" note) is the one actually updated.
- **Manual real-provider login — anthropic half DONE (2026-09-11), openai
  still pending.** The original session had no real credentials and no
  browser, so this criterion was left open. A later session verified the
  anthropic side end-to-end: the user's real Claude Code credential
  (`~/.claude/.credentials.json` claudeAiOauth — accessToken/refreshToken/
  expiresAt, exactly OAuthCredential's shape) was imported via
  `smind account add anthropic claude-main`, then a Playwright-driven
  prompt through the web UI ran a real agent turn against the live
  provider: assistant reply exact-match, `stopReason: end_turn`, recorded
  in the daemon's run_events. So routing → credential → live Anthropic
  API is proven with a real account (imported credential, not an
  in-browser OAuth consent screen — the authorize/redirect UI path itself
  still has no real-consent-screen exercise). Openai remains unverified
  against a real account: run `smind account login openai <label>` with a
  real browser, or import a Codex credential the same way, then send a
  run; until then Phase 1's ROADMAP note stays open for the openai half.
- **Verification**: `task build` (Go + web build), `task test` (full Go
  suite, all packages green, including `internal/accounts` and
  `internal/wsapi` under plain `go test`; the OAuth-specific tests were
  additionally run individually under `go test -race`), `task lint` (go
  vet + gofmt, clean); in `web/packages/ui`: `bunx tsc -b` (clean) and
  `bun run test` (12 files / 128 tests, all green). All green; nothing
  skipped except the manual real-provider step noted above.
