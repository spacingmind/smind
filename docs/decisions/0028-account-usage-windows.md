# 0028: Account usage windows (plan quota) from the provider, feeding routing and UI

## Status

**Accepted** (2026-10-11). The user approved the design and answered every
open decision: D1–D4 and D6–D13 as the recommended defaults, **D5 changed**
(the web UI ports Paseo's Usage UX; see "Decisions (accepted 2026-10-11)").
Nothing here is built yet; implementation follows the plan's PR split.

**Amended 2026-10-12 (Human, firm rule):** the provider usage endpoints are
rate-limited, so smind must never call them carelessly ("có rate limit, đừng
có gọi vô tội vạ"). Section 3 now has a **Call budget (hard rules)**
subsection; sections 4 and 5 and decisions D2, D3 and D6 are amended to match.
Also approved the same day: the account filter shared by footer and modal, a
footer row always present while polling is enabled, the "Claude"/"Codex"
labels, an event after every completed fetch, and `Problem.ExpiresAt`.

Spec and PR split: [`docs/plans/active/account-usage-windows.md`](../plans/active/account-usage-windows.md).

## Context

### What smind has today

- `internal/quota` has `Usage{TokensUsed, TokensLimit, ResetsAt}`, a
  `Fetcher` interface, and a `Poller` that serves a TTL cache over the
  `quota_snapshots` table (`usage_data` is the JSON of `Usage`).
- `cmd/smind/serve.go` wires `noopQuotaFetcher`, which always returns the
  zero `Usage`. So `routing.Router.isExhausted()` (`TokensLimit > 0 &&
  TokensUsed >= TokensLimit`) can never be true: the router never learns
  that an account is out of quota. The proxy's `routing.PolicyPool`
  failover (`internal/server/proxy.go`) therefore never fires on quota.
- `quota.Usage` is a single token counter. Subscription accounts (Claude
  Pro/Max, ChatGPT Plus/Pro) are not limited by one token counter. They
  have **several rolling windows** (5-hour, weekly, per-model weekly),
  each reported as a percentage with a reset time. The model cannot
  represent that.
- Two latent behaviors would bite as soon as a real fetcher exists:
  - `Poller.Get` returns the fetch error, and `isExhausted` returns it, so
    **one failed usage call would fail routing** (503) instead of degrading.
  - `Poller` inserts a new `quota_snapshots` row on every miss and never
    prunes, and finds the latest row by listing the whole table.
- Per-run token usage already exists and is a different thing (ADR-0020):
  `run_usage` / `usage.summary` count what *smind's runs* used, from agent
  events. It cannot know what the account's other clients (Claude Code in a
  terminal, the Codex app, a phone) have used, and it has no notion of a
  window or a reset time.

### What Paseo ships (reference, read-only; `refs/paseo` at v0.11.1-43)

- **Model** (`packages/plugin/src/server/usage.ts`): a `UsageReport` is
  `available{planLabel, windows[], balances[], details[]}`,
  `unavailable{problem: expired | rejected | no_quota}`, or `error`. A
  `UsageWindow` has `id, label, shortLabel, summary, usedPct, remainingPct,
  resetsAt, runsOutAt, shortfallPct, tone`; a `UsageBalance` has a unit
  (`usd | credits | requests | tokens`). An account `key` is stable across
  token rotation, matches `[A-Za-z0-9._-]{1,128}`, and is never a credential
  or a raw email. `fetch` re-reads the login store and **never writes it**.
- **Claude source** (`plugins/claude-usage-source/server/usage.ts`):
  `GET https://api.anthropic.com/api/oauth/usage` with `Authorization:
  Bearer` and `anthropic-beta: oauth-2025-04-20`. Response keys `five_hour`,
  `seven_day`, legacy `seven_day_opus` / `seven_day_omelette` (each
  `{utilization, resets_at}`), a newer `limits[]` of `{kind:
  "weekly_scoped", percent, resets_at, scope:{model|surface:{id,
  display_name}}}`, and `extra_usage.is_enabled`. Entries are validated one
  at a time so one odd entry cannot drop the rest. A 429 records
  `Retry-After` per token and short-circuits further calls until then.
- **Codex source** (`plugins/codex-usage-source/server/usage.ts`):
  `GET https://chatgpt.com/backend-api/wham/usage` with a bearer token, a
  browser `User-Agent`, and `ChatGPT-Account-Id`. Response:
  `rate_limit.{primary_window, secondary_window}` (`used_percent`,
  `reset_at` epoch seconds, `limit_window_seconds`), `additional_rate_limits[]`,
  `code_review_rate_limit`, `credits.balance`, `plan_type`. An HTML body is
  treated as an error.
- **Server** (`packages/server/src/server/plugins/usage-sources/index.ts`,
  `session/usage/usage-session.ts`): a registry with a 5-minute TTL cache,
  in-flight de-duplication, a 20 s deadline per fetch, forced refresh, and a
  `usage.list_reports` request that streams one update per account.
- **App/CLI** (`packages/app/src/usage/*`, `packages/cli/src/commands/usage/*`):
  a sidebar footer item showing each account's source logo and its default or
  pinned window percents, a Usage modal over the current screen with
  per-account cards, a used/remaining preference, per-card and "Refresh all"
  buttons, a Usage settings section, a remembered host pick, per-agent plan
  usage in the context-window details, and `paseo usage ls|inspect
  --refresh`. The UI's history (`#5685` footer item, `#5786` default
  percents + logos + remembered host, `#5805` streamlined sidebar usage,
  `#5876` unavailable quota explained, `#5903` derived window names and one
  pin model, `#5921` Settings discoverable above reports, `#6216` modal over
  the current screen, `#5975` per-agent usage) is what the web UI in section
  5 ports.

### Constraints already decided in smind

- **ADR-0020:** runs never go through the proxy; the proxy exists for
  management and external clients. Per-run usage comes from agent events
  into `run_usage`. The proxy is fail-closed when the model is
  undeterminable. This ADR must not route runs, and must not re-count run
  usage.
- **ADR-0019 #6:** run-scoped callers (agents) get read-only orchestration
  tools. A tool that makes an outbound call with the user's token is not
  read-only in the sense that matters (rate limit, provider-side effects).
- **Refresh:** smind stores account OAuth credentials itself
  (`accounts.OAuthCredential`, `accounts.Registry.EnsureFresh`), refreshing
  with uTLS-fingerprinted clients (`transport.Client`; Firefox for
  Anthropic, Chrome for OpenAI) because the vendors' hosts sit behind bot
  detection. `EnsureFresh` has **no per-account lock** today.
- **Tokens:** never in argv, logs, or events (ADR-0015, ADR-0017).

## Decision

### 1. Data model: a windows report replaces `quota.Usage`

`quota.Usage` is replaced by `quota.Report`, modeled on Paseo's, with the
additions the router needs (a machine-readable scope and a typed overage
flag). Go sketch (names final, fields may gain `omitempty` details in PR-A):

```go
package quota

// ReportVersion is written into every stored/wire report. A stored payload
// without "v" is the legacy Usage shape and decodes to "unknown".
const ReportVersion = 2

type Status string

const (
	StatusAvailable   Status = "available"   // windows/balances are meaningful
	StatusUnavailable Status = "unavailable" // expected, explainable: see Problem
	StatusError       Status = "error"       // transient: network, 5xx, 429, parse
)

type ProblemKind string

const (
	ProblemExpired  ProblemKind = "expired"  // credential expired and could not be refreshed
	ProblemRejected ProblemKind = "rejected" // provider answered 401/403
	ProblemNoQuota  ProblemKind = "no_quota" // account has no plan windows (API key, unsupported provider, polling off)
)

type Problem struct {
	Kind      ProblemKind `json:"kind"`
	Status    int         `json:"status,omitempty"`    // HTTP status for rejected
	ExpiresAt *time.Time  `json:"expiresAt,omitempty"` // for expired: when the login expired
	Detail    string      `json:"detail,omitempty"`    // fixed vocabulary, never a response body
}

type Tone string // "default" | "ok" | "warning" | "danger"

// ScopeKind says what a window is limited to. Only "" (the whole account)
// and "model" ever take part in routing (section 4).
type ScopeKind string

const (
	ScopeAccount ScopeKind = ""
	ScopeModel   ScopeKind = "model"   // e.g. Anthropic weekly Opus
	ScopeSurface ScopeKind = "surface" // e.g. a product surface; display only
	ScopeFeature ScopeKind = "feature" // e.g. Codex code review; display only
)

type Scope struct {
	Kind ScopeKind `json:"kind"`
	ID   string    `json:"id,omitempty"`   // provider's own id when present
	Name string    `json:"name"`           // display name, also the model matcher token
}

type Window struct {
	ID              string     `json:"id"`              // stable across refreshes: five_hour, weekly, weekly_model_opus, ...
	Label           string     `json:"label"`           // "Session", "Weekly", "Weekly · Opus"
	ShortLabel      string     `json:"shortLabel,omitempty"` // "5h", "wk"
	Scope           *Scope     `json:"scope,omitempty"` // nil = account-wide
	Summary         bool       `json:"summary,omitempty"` // shown in the sidebar item by default
	UsedPct         *float64   `json:"usedPct"`         // 0..100+, nil = provider did not say
	RemainingPct    *float64   `json:"remainingPct"`    // max(0, 100-UsedPct)
	ResetsAt        *time.Time `json:"resetsAt"`
	DurationSeconds *int64     `json:"durationSeconds,omitempty"`
	Tone            Tone       `json:"tone"`
}

type Balance struct {
	ID        string   `json:"id"`
	Label     string   `json:"label"`
	Used      *float64 `json:"used,omitempty"`
	Remaining *float64 `json:"remaining,omitempty"`
	Limit     *float64 `json:"limit,omitempty"`
	Unit      string   `json:"unit"` // usd | credits | requests | tokens
}

type Detail struct{ ID, Label, Value string }

type Report struct {
	Version        int       `json:"v"`
	Status         Status    `json:"status"`
	PlanLabel      string    `json:"planLabel,omitempty"` // "plus", "pro"; never an email
	Windows        []Window  `json:"windows,omitempty"`
	Balances       []Balance `json:"balances,omitempty"`
	Details        []Detail  `json:"details,omitempty"`
	OverageEnabled *bool     `json:"overageEnabled,omitempty"` // Anthropic extra_usage.is_enabled
	Problem        *Problem  `json:"problem,omitempty"`
	Error          string    `json:"error,omitempty"`     // fixed vocabulary: "timeout", "http 503", "malformed response"
	FetchedAt      time.Time `json:"fetchedAt"`
}
```

Tone is derived once, in the quota package, from `UsedPct`: `> 90` danger,
`>= 70` warning, else ok, default when `UsedPct` is nil (Paseo's thresholds).
Paseo's `runsOutAt` / `shortfallPct` (pace prediction) are **not** in v1;
the ROADMAP's "Quota prediction" item stays open and can add them
additively.

The Fetcher contract changes to carry the decoded credential:

```go
type Fetcher interface {
	// Fetch returns a Report for every *explainable* outcome (available,
	// or unavailable with a Problem). It returns an error only for a
	// transient failure (network, timeout, 5xx, 429, unparseable body);
	// a 429 error carries RetryAfter so the Poller can back off.
	Fetch(ctx context.Context, account accounts.Account) (Report, error)
}
```

**Stable account key.** `quota.AccountKey(a) = "<provider>-<account id>"`,
e.g. `anthropic-3` (matches `[A-Za-z0-9._-]{1,128}`). It is derived only
from the provider and the `accounts.id`, never from the label, an email, a
token, or a JWT claim. It is stable across token refresh and credential
swaps (`account.updateCredential`), changes only if the account is removed
and re-added, and is per-daemon (the ids are). The wire carries both
`accountId` (for RPC joins) and `key` (for stable UI keys and pins).
Paseo derives keys from provider identities because it reads several
harness stores that can hold the same login; smind has one store, so the
row id is the identity. Two smind accounts that hold the same provider login
will show duplicate windows; de-duplicating them is a later add-on, not v1.

**Storage.** `quota_snapshots.usage_data` stays `TEXT`; it now holds the JSON
of `Report` with `"v":2`. `quota_snapshots` itself has **no schema change and
no SQL migration** (the call-budget state is one separate new table, section
3):

- A payload without `"v"` is the legacy `Usage` shape (only ever zeros,
  from `noopQuotaFetcher`). The decoder returns "no report" for it and the
  next fetch overwrites it.
- A payload with `v` greater than this build understands also decodes to
  "no report" (a downgrade never crashes on newer rows).
- The store gains `GetLatestQuotaSnapshot(accountID)` (`ORDER BY polled_at
  DESC LIMIT 1`) replacing the list-all scan, and the poller prunes older
  rows for the account in the same transaction as the insert, keeping the
  latest one (no history in v1; history is the prerequisite for pace
  prediction and would be an additive change).
- `expires_at` keeps its meaning: `polled_at + ttl`. Deleting an account
  already deletes its snapshots (`store/accounts.go`).

### 2. Fetchers: Anthropic OAuth and OpenAI/ChatGPT OAuth first

`internal/quota` gets a dispatching fetcher keyed by `account.Provider` and
`account.CredentialType`:

| Account | Result |
|---|---|
| `anthropic` + `oauth` | `GET https://api.anthropic.com/api/oauth/usage`, headers `Authorization: Bearer`, `anthropic-beta: oauth-2025-04-20`, `Accept: application/json`. Windows: `five_hour` ("Session", `5h`, summary), `seven_day` ("Weekly", `wk`, summary), legacy `seven_day_<model>` and `limits[]` entries of kind `weekly_scoped` (reconciled into one window per scope, id `weekly_model_<id or normalized name>`, scope kind `model` or `surface`). `extra_usage.is_enabled` sets `OverageEnabled` and a detail. |
| `openai` + `oauth` | `GET https://chatgpt.com/backend-api/wham/usage`, headers `Authorization: Bearer`, `ChatGPT-Account-Id` (from the access token's `chatgpt_account_id` claim, decoded locally, never sent anywhere else), browser-like `User-Agent`. Windows from `rate_limit.primary_window` / `secondary_window`, named by `limit_window_seconds` (18000 → `five_hour`, 604800 → `weekly`, else `<n>s`); `additional_rate_limits[]` keyed by `metered_feature` → scope kind `feature`; `code_review_rate_limit` → scope `feature` "Code review"; `credits.balance` → a `credits` balance; `plan_type` → `PlanLabel`. |
| any provider + `api_key` | **No call is made.** `unavailable / no_quota` ("API-key accounts have no plan windows"). Balance-only fetchers (e.g. a provider with a credit-balance endpoint) are a later add-on that fits the same `Balances` field. |
| any other provider | `unavailable / no_quota` ("provider not supported"). |

Rules that apply to every fetcher:

- **Lenient parsing, explicit structs.** The response is decoded into
  structs that list only the fields we use. Unknown fields are discarded
  (so an `email`, a name, or an org id in the response can never reach the
  stored report). Numbers are accepted as JSON numbers or numeric strings.
  Each window/limit entry is parsed independently: a malformed entry is
  skipped and does not drop its siblings. A body that is HTML (leading `<`),
  is not JSON, or yields no windows at all becomes an `error` result
  (`"malformed response"`), never an `available` report with nothing in it.
- **Everything degrades to "unknown".** These are unofficial endpoints
  (Anthropic's is the one Claude Code's `/usage` uses; ChatGPT's is the
  Codex app's). Their shape may change without notice. Any failure produces
  a `Report` or an error that the poller turns into `unknown`; **no failure
  ever propagates into routing as an error** (section 4).
- **Smind's own stored token only.** The fetcher reads the account's
  `OAuthCredential` from smind's registry. It never reads Claude Code's
  keychain, `~/.claude/.credentials.json`, or `~/.codex/auth.json`.
- **Refresh.** If the access token is expired (or within the existing
  30 s buffer), the fetcher calls the existing `Registry.EnsureFresh` with
  the provider's existing refresher, exactly as the proxy does, then makes
  the call. It does not proactively refresh a valid token, and a 401/403
  from the usage endpoint is **not** retried with a forced refresh: it is
  `unavailable / rejected` (a forced refresh on a rotating-token provider is
  how a second client loses its login). `EnsureFresh` gains a per-account
  mutex in PR-A, because the proxy and the usage poller can now call it for
  the same account concurrently and a refresh token that rotates would
  otherwise be consumed twice. A refresh failure is `unavailable /
  expired`.
- **Transport.** The same uTLS clients the proxy uses for the two
  providers, a 15 s request timeout, no redirects followed, a 1 MiB body
  cap. The URLs are constants; no config or account field can redirect a
  usage call (so an API-key account's custom `base_url` never receives one).
- **Not read-only for the credential store, on purpose.** Paseo's `fetch`
  never writes the login store because the harness owns refresh. In smind
  the daemon owns the credential and already refreshes it for the proxy, so
  the only write is the existing refreshed-token persist, and only when the
  token was expired.

### 3. Polling and the call budget

The usage endpoints are rate-limited and unofficial. smind treats every call
as expensive: it fetches only when a person is looking, and the **daemon**
enforces a hard budget that no client (a second browser tab, a script, an
agent, a bug) can exceed.

- **TTL 5 minutes**, not configurable below 5 minutes (`accountUsage.ttlSeconds`
  may only raise it). A cached report inside the TTL is returned without a call.
- **Last good report is kept.** A failed fetch never overwrites a stored
  `available` report; it is returned marked `stale` with `lastError`. This
  is what lets an exhausted verdict survive a flaky endpoint (section 4).
- **Opt-out switch.** One config key, `accountUsage.enabled` (YAML,
  `~/.smind/config.yaml`). When false, no outbound usage call is ever made,
  `account.usage` returns `{enabled:false, reports:[]}`, and routing never
  consults windows (it behaves exactly as today). Settings shows the state
  and one sentence saying what is called and with whose token. Other config:
  `accountUsage.ttlSeconds` (300, minimum 300), `accountUsage.routing`
  (bool, section 4), `accountUsage.exhaustThresholdPct` (100). The call-budget
  constants below are **not** configurable.
- **No secrets in logs or events.** Fetchers log only account id, provider,
  HTTP status, duration, outcome kind. Never the URL's query, headers, the
  body, or a parse error that echoes a value. The stored `Error` string is
  from a fixed vocabulary, never a body excerpt.

#### Call budget (hard rules)

Human rule (2026-10-12): usage endpoints are rate-limited; never call them
carelessly. These rules live in `quota.Poller` (the daemon), so they hold
whatever the client does, and each is pinned by a test that counts real HTTP
requests on an `httptest` server.

**Who may cause a fetch.** There are three entry points on the poller, and
only the third can reach the network:

| Entry point | Used by | Network? |
|---|---|---|
| `Peek(accountID)` | routing, the proxy, events, startup, reconnect re-list, `account.usage {mode:"cached"}` | **Never** |
| `Touch(accountID)` | an explicit user surface asking for data: opening the Usage modal or Settings → Usage, the sidebar footer item while visible, `smind account usage`, MCP `account_usage` | At most **one fetch per account per TTL** |
| `Refresh(accountID)` | an explicit refresh press: a card's or Refresh-all button, `smind account usage --refresh` (never MCP, never a run-scoped caller) | Bypasses the TTL, but under the floor and budget below |

1. **Daemon-side floor, per account.**
   - **Touch:** at most 1 network fetch per account per TTL (5 min), counted
     from the last *attempt* (success or failure), not the last success.
   - **Refresh:** at most 1 per account per **30 s**, **and** at most **6 per
     account per rolling hour** (a per-account budget).
   - A request over the floor or the budget is **served from the cache**, marked
     `stale`, with `nextRefreshAt` and `refreshBlocked` (`"floor"`,
     `"budget"`, `"backoff"` or `"rejected"`); it is **never queued** into a
     later extra call, and it is not an error.
   - Derived hard ceiling: since every attempt restarts the TTL clock, one
     account receives **at most 18 usage-endpoint requests per hour** (12
     TTL-paced plus 6 forced), whatever any client does. *Refresh all* starts
     the per-account fetches with at most **2 in flight** at once.
2. **Single-flight per account.** Concurrent callers of any kind (several
   browser tabs, the CLI, MCP, an in-flight touch) share one in-flight fetch
   and all receive its result. A forced refresh that arrives while a fetch
   is in flight joins it and spends no budget.
3. **Routing and the proxy never trigger a fetch.** They hold a
   `quota.Reader` (an interface with `Peek` only), so a fetch path does not
   exist from them at compile time. They read whatever the cache holds.
4. **No fetch on daemon startup, on `account.updated` / `account.updateCredential`,
   on account creation, or on client reconnect**, and **no background timer**
   (D2). A reconnecting client re-lists with `mode:"cached"`. Startup does
   not even read the state table until an account is first asked about.
   The footer item asks for data (`mode:"touch"`) at most at the **TTL
   cadence, only while the window is both visible and focused**; the timer
   is paused when the tab is hidden or blurred, and on becoming visible it
   asks only if its last report is older than the TTL. The daemon would
   serve the cache anyway; the client does this so it does not even send the
   request. Opening the modal or Settings → Usage asks once, then follows the
   same cadence while open and focused.
5. **Failure handling, with persisted state.**
   - **429:** honor `Retry-After` (delta-seconds or HTTP-date), **at least
     5 minutes when the header is absent**, capped at 6 h so a bad header
     cannot park an account forever. Consecutive failures also raise an
     exponential backoff: 1 min, 2, 4, … capped at 30 min, plus **additive**
     jitter of 0–20% (jitter never shortens a `Retry-After`). The next
     attempt is allowed at `now + max(retryAfterOrDefault, backoff + jitter)`.
   - **5xx, timeout, network error, malformed/HTML/empty body:** the same
     exponential 1 → 30 min backoff with jitter (a changed endpoint must not
     be hammered).
   - **401/403 (rejected) and a terminal refresh failure (`invalid_grant`):**
     `unavailable`, **no retry of any kind (touch or refresh) until the
     account's credential changes** (compared by a stored fingerprint, the
     first 8 bytes of a SHA-256 of the access token or key, never logged or
     put on the wire). A re-login, `account.updateCredential`, or a token
     refreshed through the proxy changes it; re-saving the same credential
     does not.
   - A token-refresh call made for an expired credential is itself
     rate-limited traffic: it obeys the same backoff and counts as an attempt.
   - **Success** resets the failure count and the backoff.
   - **The state is persisted**, so a daemon restart does not reset any of
     it. New table `quota_poll_state` (created from `schema.sql` with
     `CREATE TABLE IF NOT EXISTS`, which already applies to existing
     databases; deleted with the account like `quota_snapshots`):

     ```sql
     CREATE TABLE IF NOT EXISTS quota_poll_state (
         account_id INTEGER PRIMARY KEY REFERENCES accounts(id),
         last_attempt_at TIMESTAMP,          -- any attempt, success or not
         next_allowed_at TIMESTAMP,          -- end of backoff / Retry-After
         consecutive_failures INTEGER NOT NULL DEFAULT 0,
         forced_at TEXT NOT NULL DEFAULT '[]', -- JSON, <= 6 unix-ms of forced attempts in the last hour
         rejected_fp TEXT                    -- credential fingerprint after 401/403/invalid_grant
     );
     ```

     The attempt is written **before** the request goes out (write-ahead),
     so a crash or restart mid-fetch cannot cause a second call inside the
     TTL. A persisted `next_allowed_at` further than the 6 h cap in the
     future (clock jumped) is clamped to the cap.
6. **Tests assert exact call counts** on an `httptest` server (burst of 50
   concurrent calls → 1 request; repeated forced refresh → capped; restart
   during backoff → 0 requests; …). The plan names them (AC44–AC54).

### 4. Routing integration

`routing.Router` consults windows instead of `TokensLimit`:

- **The router never touches the network, and never causes a fetch.** It
  holds a `quota.Reader` and calls `Peek(accountID)`, which does no I/O and
  starts nothing (call-budget rule 3). Whatever the cache holds is what routing
  knows: a cold cache or a stale one is "unknown" and routes as today. This
  replaces `isExhausted`'s synchronous `Poller.Get`, which would have held a
  proxy request for up to 15 s on a cold cache and, worse, turned every
  routed request into a potential usage call. The cache is warmed only by
  user surfaces (rule 4), so an account nobody has looked at recently is
  unknown to the router (see Consequences).
- **`Route` becomes model-aware.** `Route(ctx, sessionKey, policy,
  candidates, opts ...RouteOption)` with `routing.WithModel(model)`; the
  proxy already has the model (it narrows candidates by it, ADR-0020 §5(b))
  and passes it. When the model is unknown, only account-wide windows count
  (consistent with the proxy's fail-closed model handling: an unknown model
  never *excludes* an account on the strength of a model-scoped window).
- **Which windows count toward exhaustion**, for an account serving model M:
  - every account-wide window (`Scope == nil`): Anthropic `five_hour` and
    `seven_day`, OpenAI primary and secondary;
  - a `model`-scoped window **only if it matches M**. Match = the
    lower-cased model id contains the lower-cased, normalized scope name
    token (`Opus` matches `claude-opus-4-1`; it does not match
    `claude-sonnet-4-5`). A scope whose name has no recognizable relation to
    any model id simply never matches. The matcher lives in `quota`
    (`Window.AppliesTo(model)`) and has table tests;
  - `surface` and `feature` windows (Codex code review, additional rate
    limits, an Anthropic surface) **never** count: they are displayed only.
    Counting a window we cannot tie to a request would exclude healthy
    accounts.
- **Exhausted** = a counted window with `UsedPct >= exhaustThresholdPct`
  (default 100) or `RemainingPct <= 0`, **and** `now < ResetsAt`. A window
  whose `ResetsAt` has passed is treated as reset (0 used) without waiting
  for a refetch (routing never fetches; the next user-surface touch
  refreshes the cache). A counted exhausted
  window with **no** `ResetsAt` counts only while its snapshot is at most
  `2 × ttl` old.
- **Suppressed when overage is on.** If `Report.OverageEnabled == true`
  (Anthropic "extra usage"), the account keeps serving past the window, so
  it is not exhausted. `nil` (unknown) does not suppress.
- **Unknown never exhausts.** A cold cache, `status: error`,
  `unavailable` (expired / rejected / no_quota), a legacy or too-new
  payload, polling disabled, or `accountUsage.routing: false` all mean "not
  exhausted". Errors from the poller are never returned by the router; the
  current `check quota for account N` error path is removed.
- **An exhausted verdict outlives a failing endpoint.** Because the last good
  report is kept (section 3), an account that was exhausted at the last good
  fetch stays exhausted until its `ResetsAt`, even if every later fetch
  fails. Windows do not recover before their reset, so this is safe, and it
  is the case that matters most (the endpoint rate-limits exactly when an
  account is busy). The one exception is the overage and
  no-`ResetsAt` rules above.
- **Recovery is `ResetsAt`-based**, not a counter: no state beyond the
  snapshot. A reset passing flips the account back immediately.
- **Affinity.** Unchanged shape. A pinned account that is now exhausted (for
  this model) is dropped and a new one selected, as today. A pinned account
  that is not exhausted is kept; windows do **not** trigger migration for
  "pressure" (an account at 85% stays pinned). That keeps a session on one
  account for prompt-cache reasons and avoids flapping on a noisy endpoint.
- **Pool policy.** The first non-exhausted candidate in order, as today.
  If all are exhausted the error names the earliest `ResetsAt` among the
  candidates ("all N accounts exhausted; earliest reset 14:05"). The proxy
  still answers 503 `route request: …` in v1 (changing the proxy's error
  contract is D12).
- **Hard policy.** Unchanged contract: exhausted ⇒ error, no failover, now
  with the reset time in the message. Unknown routes. (A hard-pinned account
  that the provider will reject anyway fails earlier and with a clearer
  message; the cost is that an inaccurate 100% would block it, which the
  overage and reset rules bound.)
- **Passive signals are out of scope.** Marking an account cooling down
  when the *upstream* answers 429 / usage-limit would also work with polling
  disabled, but it is a separate mechanism with its own false-positive
  risks. Not in this ADR; see Alternatives.

### 5. Surfaces

Naming, to keep the two apart everywhere: **"Plan usage"** = windows
(this ADR; `account.usage`); **"Token usage"** = per-run and proxy token
counts (ADR-0020; `usage.summary`, `run_usage`). Both stay; neither is
removed or reshaped.

- **wsapi RPC `account.usage`** (read-only). Params `{accountIds?:
  number[], mode?: "cached" | "touch" | "refresh"}`, default `"cached"`
  (a client must opt in to anything that can reach the network); result
  `{enabled: boolean, ttlSeconds: number, reports: AccountUsage[]}`. The
  modes map to the poller entry points in section 3 (`Peek` / `Touch` /
  `Refresh`); `refresh` is denied to run-scoped callers (treated as
  `touch`):

  ```json
  { "accountId": 3, "key": "anthropic-3", "provider": "anthropic",
    "label": "Work Max", "credentialType": "oauth",
    "stale": false, "nextRefreshAt": null, "refreshBlocked": null,
    "lastError": null,
    "report": { "v": 2, "status": "available", "planLabel": "",
      "windows": [ { "id": "five_hour", "label": "Session", "shortLabel": "5h",
        "summary": true, "usedPct": 41.0, "remainingPct": 59.0,
        "resetsAt": "2026-10-11T14:05:00Z", "tone": "ok" } ],
      "fetchedAt": "2026-10-11T12:01:12Z" } }
  ```

  No credential fields, no email, no raw provider body. `nextRefreshAt` is
  the earliest time a forced refresh would reach the network (null when it
  never will, i.e. `refreshBlocked:"rejected"`), and `refreshBlocked` says
  why one would not (`"floor"`, `"budget"`, `"backoff"`, `"rejected"`).
  `touch` and `refresh` wait up to 20 s for a fetch they started or joined
  and return whatever they have; a refused refresh returns the cached
  report with `refreshBlocked` set, not an error.
- **wsapi event `account.usage`** (ADR-0005/0009 topic style, full
  `AccountUsage` snapshot payload like `account.updated`): emitted after
  **every completed network fetch** (success, `unavailable` or error), so a
  refresh whose numbers did not change still advances `fetchedAt` ("Updated
  3m ago"); not emitted for a cache hit. This is how the UI receives a
  slow account's report without holding back the others (Paseo streams
  `usage.list_reports.update` for the same reason). A `mode:"refresh"` call
  with no `accountIds` fetches the eligible accounts (at most 2 in flight),
  each emitting as it settles. Registered in `knownTopics`.
- **CLI** `smind account usage [--refresh] [--json]` (`touch` by default,
  `refresh` with `--refresh`; a blocked refresh prints why and when it will
  be allowed): one block per
  account, one line per window (`Session  41% used · resets in 2h 4m`),
  unavailable accounts show their problem in words, exit 0 whenever the call
  worked (an unavailable account is data, not an error). Distinct from
  `smind usage` (tokens).
- **MCP** (read-only, ADR-0019 #6; D8): a new
  `account_usage` tool returning the same `AccountUsage[]` (`touch` only: it
  has no refresh input, so an agent cannot force a call), added to the catalog and
  `mcp_instructions.go`, instead of widening `usage_summary`.
- **Web (D5: a port of Paseo's Usage UX).** The interaction model is
  Paseo's; the data source is smind's accounts table. Paseo's `UsageReportEntry`
  becomes `AccountUsage`, its "source" becomes the account's provider
  (`anthropic` → "Claude", `openai` → "Codex"), its "account" is the smind
  account row, and its "host" (which machine's daemon to ask) has no
  equivalent inside one daemon: the remembered pick is an **account filter**
  instead.
  - **Sidebar footer Usage item.** A row in the existing footer
    (`SidebarFooter` in `app-sidebar.tsx`), next to Providers and Agents.
    Each account's provider logo followed, per pinned window, by a meter and
    "31% 5h" (the richest layout that fits one line: meters, else labels,
    else percents alone). With no explicit pins it shows each account's
    `summary` windows (the first window with a percent if none is marked);
    pins replace the defaults, an explicit empty selection stays empty (the
    row then reads plain "Usage"). Unavailable and error accounts are
    left out of the summary. It remembers the picked account filter. The
    row is hidden while `accountUsage.enabled` is false or the daemon lacks
    the RPC; the icon-collapsed rail shows the icon only.
  - **Usage modal.** Clicking the row opens Usage **over the current
    screen** (a dialog on wide layouts, a bottom sheet on narrow ones), never
    a route change, so closing it leaves the user where they were. It mounts
    on first open and unmounts after it closes, so reports load only while it
    is shown. Title row: an account filter ("All accounts" or one account;
    hidden with a single account), **Refresh all**, and a **Settings**
    button. The body is the per-account cards, scrollable on long lists.
  - **Cards.** One per account: provider logo, provider name, plan badge,
    a status dot with "Unavailable" / "Error" when not available, a refresh
    button whose tooltip says when the report was fetched ("Updated 3m
    ago", printed on the card itself where there is no hover). One bar per
    window: label, percent (used, or "N% left"), reset countdown ("resets
    2h", "resetting now"), a meter coloured by tone `ok` / `warning` /
    `danger` (default muted). Balances as "$x / $y" or "x left" lines,
    details as label/value rows, the account label in the footer. Problem
    states are explained in words, never a bare badge: *expired* ("Login
    expired 2h ago. Reconnect it in Providers."), *rejected* ("Login
    rejected (HTTP 401). Reconnect it in Providers."), *no_quota* (the
    report's detail: "API-key accounts have no plan windows"), *error* (the
    fixed-vocabulary error plus "retry in 3 min" when backing off). A failed
    refresh keeps the previous report on screen and adds "Unable to refresh
    usage".
  - **Pin windows, used/remaining.** While the sidebar summary is on, every
    window row is a checkbox that pins it to the footer item (the whole row,
    a pin glyph with a tooltip, half-strength hover highlight, the checked
    state in its accessible name: "Pin Claude Session, 31% · resets 2h").
    A pin is `{provider, windowId}` and matches that window on every
    account of the provider. A "Percentages" control switches the footer,
    the cards and Settings between **Used** and **Remaining**.
  - **Settings → Usage.** The existing Usage section gets a **Plan usage**
    block **above** the existing token table (retitled **Token usage**): the
    same cards and controls as the modal, with Refresh all and a visible
    **Settings** control (the "Summary in sidebar" switch and the
    Used/Remaining control) in the block's header, above the reports, so it
    is found without opening a menu. When polling is disabled the block says
    so, names `accountUsage.enabled`, and offers nothing to refresh. When
    the daemon predates `account.usage` it says "Update the daemon to see
    plan usage" and makes no call.
  - **Per-run plan usage (optional, last).** The run usage pill's popover
    may show the matching account's plan cards (no pins, no refresh), the
    way Paseo shows an agent's own account in its context-window details.
    Smind's runs use the user's own provider login, which may not be a
    smind account, so it shows only when **exactly one** OAuth account of
    the run's provider exists, labelled with that account; otherwise
    nothing. It is a separate, deferrable PR.
  - **Call discipline in the client.** Reconnect re-lists with `mode:"cached"`.
    The footer item and the open modal ask with `mode:"touch"` only at the TTL
    cadence while the window is visible and focused (paused when hidden or
    blurred). Refresh buttons use `mode:"refresh"`, are disabled for 30 s
    after a press, and are also disabled with the reason when the daemon
    reports `refreshBlocked` ("Rate limited, retry in 12 min", "Refreshed just
    now", "Refresh limit reached, next in 24 min", "Reconnect this account to
    refresh"). The daemon enforces the budget either way.
  - **State and rules.** Preferences (`displayAs`, `pins`, picked account,
    "Summary in sidebar") are client-side in `localStorage` (D11), with
    Paseo's rules: `null` pins mean defaults, an array (even empty) is the
    user's selection, an unknown account pick falls back to All. No new web
    dependency (smind's web has no data-fetching library): the reports live
    in a small module store fed by `account.usage` and the `account.usage`
    event, keeping the newer `fetchedAt` when a refresh and a list race. A
    missing or reshaped field renders as "not shown", never throws.
  - **Verification.** Component and logic tests mirror Paseo's e2e specs
    (listed in the plan's PR-D), and a light and dark screenshot pass
    (desktop and narrow) is required before PR-D merges.
  - The Settings → Providers rows are unchanged by this ADR (no per-row
    bars): plan usage has one home, the Usage surfaces above.

### 6. Security

- Tokens are read from smind's registry in the daemon process and placed
  only in an `Authorization` header of the outbound call. No CLI flag, env
  var, argv, log line, event, wsapi payload, or stored report contains a
  token, a refresh token, a JWT, or a response body. A test asserts it for
  the stored JSON, the wsapi payload, the event, and captured logs.
- The account key and every surface identify an account by provider and id.
  The `label` is user-chosen text and may itself be an email; it is shown to
  the local user as today and is **not** copied into logs. The report never
  carries a provider-returned email or name (explicit-struct parsing).
- The `chatgpt_account_id` JWT claim is decoded locally for the request
  header; it is not stored or displayed.
- Outbound calls go only to the two fixed provider hosts, never to a user
  `base_url`, and never from a run-scoped request path.
- Disclosure: the feature calls two undocumented endpoints using the user's
  tokens. The Settings switch states this in one sentence (D2).

## Decisions (accepted 2026-10-11)

The user accepted D1–D4 and D6–D13 as the recommended defaults and changed
D5. These are decided; they are not to be reopened inside a PR (AGENTS.md
rule d).

- **D1. Exhaustion threshold.** **100%** used on any counted window; config
  `accountUsage.exhaustThresholdPct`, default 100.
- **D2. Default for outbound usage polling.** **On, demand-driven** (no idle
  timer; zero calls unless a surface or the proxy asks), one
  `accountUsage.enabled` kill switch, and a one-line disclosure next to the
  switch.
- **D3. TTL, backoff, forced-refresh floor.** TTL 5 min; backoff 1→30 min
  with jitter and `Retry-After`; forced refresh at most once per 30 s per
  account.
- **D4. Providers in v1.** Anthropic OAuth + OpenAI/ChatGPT OAuth. API-key
  accounts and other providers show "no plan windows" with no HTTP call.
  Kimi, xAI, Antigravity and balance-only API-key providers are later.
- **D5. UI placement and UX — CHANGED from the proposed default.** The web
  UI **ports Paseo's Usage UX** (user: "tham khảo UI/UX paseo phần đó"):
  sidebar footer Usage item (provider logo + default/pinned window
  percents, remembers the picked account), Usage opens as a modal over the
  current screen, Settings → Usage discoverable above the reports with
  per-account cards (window bars, reset countdown, tone, balances, problem
  states with explanations), refresh, pin windows, used/remaining toggle,
  and per-run plan usage as an optional last PR. Section 5 and the plan's
  PR-D carry the details and the Paseo → smind file map. This replaces the
  proposed "bars inside each Settings → Providers row".
- **D6. Routing uses windows in the first release.** Yes, default
  `accountUsage.routing: true`.
- **D7. Hard policy with an exhausted account.** Keep the existing contract
  (error, no failover), now naming the reset time.
- **D8. MCP shape.** A **new read-only `account_usage` tool**, not an
  extension of `usage_summary` (different shape, no `groupBy` or
  `since`/`until`).
- **D9. Overage.** When Anthropic `extra_usage` is enabled, a full window
  does not count as exhausted. Unverified that `is_enabled` always means
  requests continue past the window; revisit after the live probe below.
- **D10. May the poller refresh an expired token?** Yes, only when the token
  is expired, via the existing `EnsureFresh` with a new per-account lock,
  never on a 401.
- **D11. Where UI preferences live.** Client `localStorage`.
- **D12. Proxy error when every candidate is exhausted.** Keep 503 with the
  reset time in the message for v1.
- **D13. Per-account opt-out.** Global switch only in v1.

**Amendments, 2026-10-12 (Human, firm rule: do not call the rate-limited
endpoints carelessly).** These replace the parts of D2, D3, D6 and D10 they
touch; the rest of each decision stands.

- **D2 (amended).** Demand-driven means **user surfaces only**: the proxy and
  routing never cause a fetch (the earlier "or the proxy asks" is removed);
  nothing fetches on startup, on `account.updated`, or on reconnect.
- **D3 (amended).** TTL 5 min is a daemon-enforced floor (1 fetch per account
  per TTL, not configurable below 5 min). Forced refresh: at most 1 per 30 s
  **and 6 per hour** per account; excess is served from cache, never queued.
  429 honors `Retry-After` (at least 5 min if absent); backoff 1→30 min with
  jitter; 401/403 stops retrying until the credential changes; all of this is
  persisted across restarts. See "Call budget (hard rules)".
- **D6 (amended).** Routing uses windows, but only what the cache already
  holds (`Peek`). Routing knowledge is therefore as fresh as the last user
  surface that looked; accepted.
- **D10 (amended).** An expired-token refresh made for a usage fetch is itself
  budgeted traffic (it obeys the same backoff) and a terminal refresh failure
  stops retries until the credential changes.

**Live probes need Human go-ahead.** Four facts are unverified: that
Anthropic's usage endpoint accepts smind's stored token (scope), that a
stored OpenAI token carries `chatgpt_account_id`, that `chatgpt.com` accepts
the uTLS fingerprint on that path, and what `extra_usage.is_enabled` means
for serving. Probing them calls real endpoints with a real token, which is
an outward-facing action: PR-A is fixtures-only until the Human approves a
specific manual probe (see the plan's Validation).

## Alternatives considered

- **Keep a single token counter (`TokensUsed/TokensLimit`) and fill it.**
  Rejected: subscription limits are percentages over several windows; any
  single counter either hides the 5-hour window or the weekly one.
- **Compute usage locally from `run_usage` and `request_log`.** Rejected as
  the source of truth (ADR-0020 already rules out estimating): it only sees
  smind's own traffic, not the account's other clients, and has no window
  or reset semantics. It remains the right answer for "what did my runs
  use" (token usage).
- **Read Claude Code's keychain / `~/.codex/auth.json` like Paseo.**
  Rejected: smind stores its own accounts; reading another app's secrets is
  a larger trust and OS-prompt cost (macOS keychain ACLs) and would report a
  different login than the one smind routes with.
- **Poll in the background on a timer, or keep the cache warm for routing.**
  Rejected: it wakes idle accounts and adds constant outbound traffic with
  user tokens to a rate-limited endpoint. The cost is that routing is blind
  to accounts nobody has looked at (Consequences); a keep-warm mechanism would
  need its own ADR and the Human's approval.
- **Block the proxy request on a fresh fetch, or refresh asynchronously from
  routing (the first draft).** Rejected: a synchronous fetch stalls up to
  15 s on a cold cache, and either form lets ordinary proxy traffic cause
  calls to a rate-limited endpoint. Routing reads the cache only.
- **Fail closed when usage is unknown.** Rejected: an unofficial endpoint
  would then be able to take routing down. Unknown must mean available.
- **Passive exhaustion from upstream 429s.** Not rejected on merit, only
  deferred: it needs no outbound polling and could cover the polling-off
  case, but it has to distinguish a quota 429 from a burst rate limit and
  would need a cooling-down state. A follow-up ADR if polling is turned off in practice.
- **Least-pressure ordering in the pool.** Deferred: picking the candidate
  with the most remaining quota is a routing-policy change (and wants
  history for pace); v1 only excludes exhausted accounts.
- **Per-account bars inside Settings → Providers rows (the first draft of
  D5).** Superseded by the Paseo port: one home for plan usage (footer item,
  modal, Settings → Usage) with pins and a summary, instead of bars buried in
  the connection-management list.
- **Move per-run tokens into windows (one usage concept).** Rejected: they
  answer different questions and have different sources and lifetimes (ADR-0020).

## Consequences

- The proxy's pool failover starts working on real quota for Anthropic and
  OpenAI OAuth accounts; the stub in `serve.go` and its comment go away.
- smind makes outbound calls with user tokens to two undocumented endpoints.
  They can change or start rejecting without notice; the design makes that
  a missing-data state, never a routing outage, but the windows UI will go
  blank for the affected provider until a parser update ships. The tests
  pin today's shapes with fixtures and never call the real endpoints, so
  they cannot detect drift by themselves; refreshing the fixtures from a
  real response is a manual step.
- **Routing freshness depends on a person looking.** Because only user
  surfaces fetch, the router knows an account is exhausted only if a surface
  touched it before the window filled; an exhausted verdict then holds until
  `resetsAt`, but an account nobody has viewed is unknown and routed to
  (fail-open). With the desktop app's footer item visible and focused the
  cache stays within one TTL; a headless daemon with no UI open gets no
  fresh data. This is the accepted price of the call budget.
- Token-refresh races become possible between the proxy and the poller;
  the new per-account `EnsureFresh` lock is part of PR-A and protects the
  proxy too.
- An account added from imported Claude Code credentials may share a rotating
  refresh token with the original app. smind already refreshes such
  credentials for proxying; polling does not add refreshes except when an
  idle account's token has expired (D10).
- `Router.Route` and `quota.Fetcher` change signatures; `quota.Usage` and
  `noopQuotaFetcher` are deleted, and the only callers are `serve.go` and
  `proxy.go`.
- A thin new config section (`accountUsage`), one new table
  (`quota_poll_state`), and one new wsapi RPC, one
  topic, one CLI subcommand, one MCP tool, and one web component family
  ported from Paseo's usage UI (several files, no new data-fetching
  dependency; a small segmented control and provider logo marks are the only
  new UI primitives).

## Cross-references

- ADR-0005 / ADR-0009 (event subscription and topic style: `account.usage`).
- ADR-0015 (account RPCs, secrets stay out of payloads), ADR-0020 (runs off
  the proxy; per-run usage; model-aware, fail-closed routing),
  ADR-0017 / ADR-0019 #6 (MCP read-only for run-scoped callers).
- `internal/quota`, `internal/routing/router.go`,
  `internal/accounts/refresh.go`, `internal/server/proxy.go`,
  `cmd/smind/serve.go`.
- `refs/paseo/packages/plugin/src/server/usage.ts`,
  `refs/paseo/plugins/claude-usage-source/server/usage.ts`,
  `refs/paseo/plugins/codex-usage-source/server/usage.ts`,
  `refs/paseo/packages/server/src/server/plugins/usage-sources/index.ts`,
  `refs/paseo/packages/app/src/usage/*` (ported by the web UI),
  `refs/paseo/packages/app/e2e/browser/usage-{modal,settings,sidebar-item}.spec.ts`,
  `refs/paseo/packages/cli/src/commands/usage/*`.
- `docs/ROADMAP.md` ("Quota prediction", quota `Fetcher` stub) and
  `docs/plans/active/orchestration-and-metering.md` (the "feed the router's
  `TokensUsed`" backlog item, superseded by this ADR).
