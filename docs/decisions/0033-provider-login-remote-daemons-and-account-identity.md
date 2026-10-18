# 0033: Provider login for remote daemons, and stable account identity

## Status

Accepted (2026-10-18, by the Human: "ok theo đề xuất" on all six recommendations below). Plan:
[`docs/plans/active/providers-login-and-tab.md`](../plans/active/providers-login-and-tab.md). Builds on
ADR-0028 (account usage; the rate-limit rules still apply) and ADR-0031 (connection kinds `local`/`url`/`ssh`/`relay`).
Research behind it: a 2026-10-18 study of `refs/cliproxyapi`, `refs/codex`, `refs/claude-code`,
`refs/agent-client-protocol` (local-only notes, not committed).

## Context

`account.oauthStart` (ADR-0020/0021 era, `internal/accounts/login_coordinator.go`) runs a PKCE login by binding a
callback listener **on the daemon's host** at the vendor-fixed address (`localhost:54545` Anthropic, `localhost:1455`
OpenAI; `oauth_login.go:23-27`) and waiting 5 minutes. The vendors' registered `redirect_uri` is
`http://localhost:<port>/…`, so the redirect lands on **the machine the browser runs on**. That is the same machine only
for a local daemon. With a homelab daemon (url/ssh/relay connection), the redirect hits the *client's* localhost,
nothing listens, and the login hangs until the timeout. There is no paste or device path, and a re-login of an
account creates a **new** row (accounts are identified only by `id`/`label`), orphaning routing links, quota rows and
workspace assignments.

References: Codex CLI offers browser login (loopback `1455/1457`) and a **device-code** login for headless machines
(`codex-rs/login/src/device_code_auth.rs`, `POST …/api/accounts/deviceauth/usercode` then `…/deviceauth/token`, ≤15 min;
a 404 means "not enabled" → fall back). Claude Code accepts a **pasted OAuth code** when the callback cannot reach
localhost (WSL2/SSH/containers, changelog v2.1.126). cliproxyapi does both (paste via
`POST /v0/management/oauth-callback {provider,state,redirect_url}`, device code for Codex/Kimi/xAI), names accounts by
stable identity (`claude-<email>.json`) and merges on re-login. Its fixed-port **all-interface** callback listener and
its browser-opens-on-the-daemon-host default are the anti-patterns.

## Decision

### D1. Remote-daemon login is supported (accepted: yes)

A login started from a client whose browser is not on the daemon's host must be completable. The daemon keeps the
callback listener on **loopback only** (never `0.0.0.0`); remote completion goes through D2/D3, not through a wider listener.

### D2. Device-code login for Codex/OpenAI (accepted: yes, default when remote)

`account.oauthStart` takes `method: "browser" | "device"` (default `"browser"`). `device` is accepted for `openai` only
and uses the same endpoints and `redirect_uri` Codex CLI uses (`https://auth.openai.com/api/accounts/deviceauth/usercode`,
`…/deviceauth/token`; reference implementation `refs/cliproxyapi/sdk/auth/codex_device.go:27-30`). The daemon emits
`deviceCode {loginId, verificationUrl, userCode, expiresAt, intervalSeconds}` to the requesting connection, polls at the
vendor interval until `expiresAt` (hard cap 15 min) or cancel, then exchanges as the browser flow does. A 404 on the
user-code request returns the error code `device_unsupported`; the UI falls back to browser/paste. No port, no listener.

### D3. Paste-the-redirect for Claude (and as fallback for OpenAI) via `account.oauthComplete` (accepted: yes)

- `account.oauthStart` now emits `authorizeUrl {loginId, url}` (adds the `loginId`: random 128-bit, server-generated).
- New RPC `account.oauthComplete {loginId, redirectUrl}`. `redirectUrl` is what the user copies from the browser's
  address bar after the vendor redirected to a localhost page that did not load. A lenient parser accepts the full URL,
  a bare query (`code=…&state=…`) or Claude's `code#state` form (precedent: `refs/cliproxyapi/internal/misc/oauth.go:52`).
- The daemon checks that `loginId` is pending and of `browser` method, that `state` equals the login's (constant-time),
  and that a code is present; then it feeds the **same** result channel the loopback listener feeds. First delivery wins
  (the code is single-use); the second is ignored. `account.oauthComplete` returns `{}` immediately; the original
  `account.oauthStart` request resolves with the account. An unknown/expired `loginId` is an **error** (never a silent
  success); a wrong `state` is an error and leaves the login pending, with at most 5 attempts per login.
- The browser login lifetime becomes 10 minutes (was 5) so a human can paste.
- Security: the redirect URL carries an authorization code — it is never logged and never echoed in an error message
  (errors name only the reason). The PKCE verifier never leaves the daemon.

### D4. "Remote" is derived on the client from the connection, with a manual override (accepted)

The daemon cannot tell: through an ssh tunnel or the relay every client looks loopback to it. The client decides:
- desktop: the selected `Connection.kind` — `local` → local; `url`, `ssh`, `relay` → remote;
- web build: the page's hostname — loopback (`localhost`, `127.0.0.0/8`, `::1`) → local, anything else → remote;
- a manual switch on the Connect form ("Sign in from this device" / "Sign in from another device"), persisted per
  connection id in the UI store, overrides the derivation.
Remote changes defaults only: OpenAI → device code, Anthropic → the paste step is shown immediately (the client still
opens the authorize URL in its own browser). Local keeps today's flow and shows paste as "Having trouble?" after ~15 s.

### D5. Connect method selector replaces "Paste a credential instead" (accepted: yes)

The Connect form offers the methods the provider supports: Claude — *Claude.ai subscription* (OAuth) / *API key*;
Codex — *Sign in with ChatGPT* / *Device code* / *API key*. API-key entry (including base URL under Advanced) is a
first-class choice, not a collapsed disclosure. Providers that are ACP agents render their advertised `authMethods`
(`agent` / `terminal`) instead of hard-coded buttons (ACP `initialize`; `env_var` is not used — ACP removed it as
non-generalizable to remote transports). The explicit login contract below is the daemon half of this.

### D6. Re-login updates the existing account via a stable identity (accepted: yes)

**Data model.** `accounts` gains `identity TEXT NULL` and a partial unique index
`UNIQUE (provider, identity) WHERE identity IS NOT NULL`.
- `identity` = hex SHA-256 of the **length-prefixed** provider and subject, `"<len>:<provider>|<len>:<subject>"` (length
  prefixes make the hash input delimiter-safe: no provider/subject text can make two different pairs collide, which a plain
  `"<provider>:<subject>"` join would allow), where `<subject>` is, from the **token exchange response**
  (no extra provider call, so ADR-0028's rate-limit rules are untouched): Anthropic → `account.uuid`
  (`refs/cliproxyapi/internal/auth/claude/anthropic_auth.go:134-139`); OpenAI → the id_token's
  `sub` and `chatgpt_account_id` claims, length-prefixed the same way, **both required** (with either missing the login has
  no identity, so two workspaces can never merge on `sub` alone;
  `refs/cliproxyapi/internal/auth/codex/jwt_parser.go:19,43`). The id_token payload is decoded without signature
  verification (it came straight from the token endpoint over TLS, the trust the access token already gets), and an
  unexpected `account` / `id_token` shape means "no identity", never a failed login. It is hashed so no
  vendor id/e-mail is stored or logged; it never crosses the wire (the wire only gets `hasIdentity: bool`). API-key
  and pasted-credential accounts keep `identity = NULL`; replacing a credential by paste (`account.updateCredential`)
  **clears** the row's identity, since it described the old credential.
- No e-mail is stored or exposed (ADR-0028's "no e-mail beyond the user's label" holds).

**Semantics of `account.oauthStart`** (optional `accountId`):
1. After the code exchange the daemon computes `identity`.
2. An account with the same `(provider, identity)` exists → its credential is **replaced in place**: id, label, routing
   links, workspace assignments, models, quota/cooldown rows are kept (it is the same vendor account, still as
   quota-exhausted as before, so the passive cooldown stays); the refresh-blocked state clears by itself (the poller's
   rejected-credential state is keyed on a credential fingerprint); `account.updated` is emitted; the result carries
   `created: false`. The requested label is ignored (the UI says "Updated 'work' — already connected"). Replacement is
   serialized with an in-flight token refresh of the same account, so a refresh of the old credential cannot overwrite it.
3. `accountId` is given (row-level Re-login) and that row has an identity that **differs** → error code
   `identity_mismatch` (the user logged into a different vendor account); the UI offers "Connect as a new account". If the
   login yielded **no** identity at all (the exchange response lacked the subject) and the row has one → `identity_unverified`
   and nothing is changed: a login that cannot prove who it is never overwrites an identity-bearing row.
4. `accountId` is given and the row has **no** identity (legacy) → the credential is replaced and the identity is
   **adopted** (`adopted: true`); the UI confirms first ("This replaces the credential of 'work'"). The row may be a
   different vendor account than the old credential, so its quota cooldowns are cleared (as for a pasted replace). If
   another row already owns that identity → error `identity_in_use` naming that row's id. Only OAuth rows take part: an
   `accountId` naming an API-key / pasted-credential row → `login_not_oauth_account`; one naming a missing row or another
   provider's → `login_unknown_account` (both checked before the browser step).
5. Otherwise a new account is created with the identity (`created: true`).

The CLI (`smind account login <provider> <label>`) goes through the same RPC, so it too now updates an existing
same-identity account instead of creating a second row, ignores the label in that case, and prints
`updated <label> (already connected)`.

**Migration.** `ALTER TABLE accounts ADD COLUMN identity TEXT` + the partial unique index via `internal/store/migrate.go`'s
add-column helper, and `schema.sql`. Existing OAuth rows keep `NULL` and gain an identity at their first Re-login
(adoption). They are **not** backfilled by a provider call (Anthropic would need `/api/oauth/profile`; ADR-0028 forbids
calling vendor endpoints casually). OpenAI rows could be backfilled from the refresh response's id_token
(`refresh_providers.go:361` already parses it) — allowed as a follow-up, not required. Legacy duplicates cannot be
detected before adoption; the UI offers nothing automatic for them. Rolling back is **not fully safe once identities have been written**: the column
is nullable and an older binary ignores it, but an older binary's paste-replace would not clear the identity, so a stale
identity could wrongly merge a later login into that row. Clear the column (`UPDATE accounts SET identity = NULL`) when
downgrading.

### Login contract (the daemon half; the UI half is the plan's PR-B4)

| RPC / event | Shape |
|---|---|
| `account.oauthStart` (streaming) | params `{provider, label, method?, accountId?}`; events `authorizeUrl {loginId,url}` or `deviceCode {loginId,verificationUrl,userCode,expiresAt,intervalSeconds}`; final result `{account…, created, adopted?}` |
| `account.oauthComplete` | `{loginId, redirectUrl}` → `{}` |
| cancel | aborting the in-flight `account.oauthStart` request (existing `task.cancel`-by-request-id path) cancels the login, frees the listener/poller and the per-provider claim |
| errors | stable codes in the message prefix: `login_in_progress`, `login_unknown`, `state_mismatch`, `device_unsupported`, `device_expired`, `identity_mismatch`, `identity_in_use`, `identity_unverified`, `login_unknown_account`, `login_not_oauth_account`, `denied`, `timeout`. On the wire the message is `account.oauthStart: <code>: <detail>` (the handler's method prefix, then the code): clients anchor on the code after that prefix, never on the detail text |

The client keeps a **generation counter** per Connect attempt: events/results from an older generation (after Cancel or a
restart of the flow) are dropped (cliproxyapi's TUI pattern). One login per provider at a time stays (existing claim).

## Alternatives considered

- **Bind the callback on all interfaces + SSH-tunnel hint** (cliproxyapi). Rejected: exposes the listener, the hint assumes
  `root@`/22, and it only works if the user forwards the port.
- **Relay the vendor redirect through the smind relay.** Impossible: the registered `redirect_uri` is fixed to localhost.
- **Open the browser on the daemon host.** Useless for a headless homelab daemon.
- **Daemon-reported "is local client".** Unreliable: tunnels and the relay make every client loopback to the daemon.
- **Identity by e-mail / by label.** Labels are user text; e-mail is PII we promised not to store (ADR-0028) and OpenAI
  tokens are per workspace, not per e-mail. Hashed vendor subject is stable and non-PII.
- **Device code for Claude.** No vendor flow exists in any reference.
- **Backfill identity with a provider call.** Violates the call budget; adoption at re-login is enough.

## Consequences

- Homelab users can sign in to Claude and Codex from the desktop/web client; the "hangs 5 min" failure goes away.
- Two new daemon surfaces handle authorization codes and device codes: they get an adversarial review, fake vendor
  endpoints in tests (`httptest`), and the rule that tests never reach real vendor URLs.
- A migration adds one nullable column; legacy rows are unaffected until re-login.
- `account.list` gains the boolean `hasIdentity` (never the identity), so the UI knows whether a row-level Re-login will adopt (and confirm first); `AccountUsage` gains nothing. `created`/`adopted` come from the login result.
