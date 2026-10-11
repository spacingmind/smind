# 0028: Account usage windows (plan quota) from the provider, feeding routing and UI

## Status

**Proposed, docs-only** (2026-10-11). Nothing here is built. The "Open
decisions for Human" section lists what must be answered before PR-A
starts; each has a recommended default. Until the user answers, this is a
design to pick up, not a commitment.

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
  a sidebar item showing pinned windows (default: windows flagged `summary`),
  a modal with per-account cards, used/remaining display preference, a
  refresh button, and `paseo usage ls|inspect --refresh`.

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

## Decision (proposed)

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
	Kind   ProblemKind `json:"kind"`
	Status int         `json:"status,omitempty"` // HTTP status for rejected
	Detail string      `json:"detail,omitempty"` // fixed vocabulary, never a response body
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
of `Report` with `"v":2`. There is **no schema change and no SQL
migration**:

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

### 3. Polling: demand-driven, cached, backed off

- **TTL 5 minutes** (matches `defaultQuotaTTL` and Paseo). A cached report
  inside the TTL is returned without a call.
- **No idle timer.** The daemon does not poll in the background on its own.
  A refresh happens only when (a) a surface asks (`account.usage`, the UI,
  CLI, MCP) and the snapshot is older than the TTL, or (b) the proxy is
  about to route among at least one candidate and the snapshot is older
  than the TTL (an **asynchronous** refresh; the request is not held up, see
  section 4). An idle daemon makes zero outbound usage calls.
- **In-flight de-duplication** per account (singleflight): concurrent
  callers share one fetch.
- **Backoff** per account, in memory: on 429 honor `Retry-After` (seconds
  or HTTP-date); on 5xx, timeout or network error use exponential backoff
  1 min → 2 → 4 → … capped at 30 min, with jitter; reset on success. During
  backoff the poller serves the last report, marked `stale`, with
  `nextRetryAt`. A 401/403 (`rejected`) and a no_quota result are cached
  for the normal TTL, not hammered. Backoff state is not persisted: a
  restart clears it, and the worst case is one more 429.
- **On-demand refresh** (`account.usage {refresh:true}`, `smind account
  usage --refresh`, the UI refresh button) bypasses the TTL but **not** an
  active backoff window, and not more than once per 30 s per account.
  Refresh from a run-scoped caller (an agent over MCP) is ignored: it gets
  the cached report. An agent must not be able to make smind hit a provider
  in a loop.
- **Last good report is kept.** A failed fetch never overwrites a stored
  `available` report; the poller returns it marked `stale` with the error
  alongside (`lastError`). This is what lets an exhausted verdict survive
  a flaky endpoint (section 4).
- **Opt-out switch.** The whole feature is controlled by one config key,
  `accountUsage.enabled` (YAML, `~/.smind/config.yaml`). When false, no
  outbound usage call is ever made, `account.usage` returns
  `{enabled:false, reports:[]}`, and routing never consults windows (it
  behaves exactly as today). Settings shows the switch's state and the one
  sentence that explains what is called and with whose token. Other
  config: `accountUsage.ttlSeconds` (300), `accountUsage.routing` (bool,
  section 4), `accountUsage.exhaustThresholdPct` (100).
- **No secrets in logs or events.** Fetchers log only `account id,
  provider, HTTP status, duration, outcome kind`. Never the URL's query,
  headers, the body, or a parse error that echoes a value (parse errors log
  the field path only). The stored `Error` string is from a fixed
  vocabulary, never a body excerpt (an HTML error page is not stored).

### 4. Routing integration

`routing.Router` consults windows instead of `TokensLimit`:

- **The router never blocks on the network.** It reads the cache
  (`Poller.Peek(accountID)`, which does no I/O). If the snapshot is older
  than the TTL it triggers an asynchronous refresh (de-duplicated) and
  decides on what it has. A cold cache (no snapshot yet) is "unknown", so a
  fresh daemon's first request is routed as today, and the next one sees
  the fetched windows. This replaces `isExhausted`'s synchronous
  `Poller.Get`, which would have held a proxy request for up to 15 s on a
  cold or expired cache.
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
  for a refetch; the async refresh then confirms. A counted exhausted
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
  number[], refresh?: boolean}`; result `{enabled: boolean, reports:
  AccountUsage[]}`:

  ```json
  { "accountId": 3, "key": "anthropic-3", "provider": "anthropic",
    "label": "Work Max", "credentialType": "oauth",
    "stale": false, "nextRetryAt": null, "lastError": null,
    "report": { "v": 2, "status": "available", "planLabel": "",
      "windows": [ { "id": "five_hour", "label": "Session", "shortLabel": "5h",
        "summary": true, "usedPct": 41.0, "remainingPct": 59.0,
        "resetsAt": "2026-10-11T14:05:00Z", "tone": "ok" } ],
      "fetchedAt": "2026-10-11T12:01:12Z" } }
  ```

  No credential fields, no email, no raw provider body. `refresh:true`
  waits up to 20 s for the fetch and returns whatever it has.
- **wsapi event `account.usage`** (ADR-0005/0009 topic style, full snapshot
  payload like `account.updated`): emitted after a fetch that changed the
  stored report (any window pct/reset, status, problem), not on every poll.
  Registered in `knownTopics`.
- **CLI** `smind account usage [--refresh] [--json]`: one block per
  account, one line per window (`Session  41% used · resets in 2h 4m`),
  unavailable accounts show their problem in words, exit 0 whenever the call
  worked (an unavailable account is data, not an error). Distinct from
  `smind usage` (tokens).
- **MCP** (read-only, ADR-0019 #6): see D8. Recommended: a new
  `account_usage` tool returning the same `AccountUsage[]` (no `refresh`
  input for run-scoped callers), added to the catalog and
  `mcp_instructions.go`, instead of widening `usage_summary`.
- **Web**:
  - **Settings → Accounts/Providers rows** (the existing account list in
    `providers-section.tsx`): per account, one bar per window with used %,
    tone color, and the reset time ("resets in 2h 4m", absolute time in a
    tooltip); account-wide windows first, scoped ones after; balances as a
    line; unavailable states in words ("Login expired – reconnect", "Provider
    rejected the token", "No plan windows for API keys"); a "stale · retry
    in 3 min" hint; refresh button; the polling switch.
  - **Sidebar footer item**: pinned windows (default: windows with
    `summary: true`, i.e. the account-wide ones) as compact meters with a
    used/remaining toggle; click opens a popover with every account's
    windows. Pins and the toggle are client preferences in `localStorage`
    (D11); nothing is stored in the daemon. Hidden when there is nothing to
    show.
  - The existing Settings → Usage section keeps showing token usage and is
    retitled "Token usage" if the two would otherwise be confused.
  - Light and dark screenshot pass before the UI PR merges (standing rule).

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

## Open decisions for Human

Each has a recommended default; PR-A should not start until D1–D4, D9, D10
are answered (they shape types and the fetcher). Others can be answered
before their PR.

- **D1. Exhaustion threshold.** At what used % is an account "exhausted"?
  *Recommended:* **100%** on any counted window (config
  `accountUsage.exhaustThresholdPct`, default 100). Lower values (e.g. 95)
  fail over earlier but waste quota and amplify endpoint noise.
- **D2. Default for outbound usage polling.** On or off out of the box?
  *Recommended:* **on, demand-driven** (no idle timer; zero calls unless a
  surface or the proxy asks), a single `accountUsage.enabled` kill switch,
  and a one-line disclosure next to the switch. Alternative: off until the
  user opts in (safer, but the feature is invisible until discovered).
- **D3. TTL, backoff, forced-refresh floor.** *Recommended:* TTL 5 min,
  backoff 1→30 min with jitter and `Retry-After`, forced refresh at most
  once per 30 s per account.
- **D4. Providers in v1.** *Recommended:* Anthropic OAuth + OpenAI/ChatGPT
  OAuth (the two providers the proxy routes and the only two with a login
  flow); API-key accounts and other providers show "no plan windows". Kimi,
  xAI, Antigravity (refreshers exist, no known usage endpoint, no proxy
  route) and balance-only API-key providers are later.
- **D5. UI placement.** *Recommended:* windows inside each account row of
  Settings → Accounts/Providers **plus** a sidebar footer item with pins
  (the Paseo pattern); the Usage settings section stays tokens only.
  Alternative: one dedicated "Plan usage" settings page.
- **D6. Should routing use windows at all in the first release?**
  *Recommended:* **yes, default `accountUsage.routing: true`**, because
  only a 100% verdict ever changes routing and every failure is fail-open.
  Alternative: ship display first (PR-A, C, D) and enable routing (PR-B)
  one release later behind `routing: false`.
- **D7. Hard policy with an exhausted account.** *Recommended:* keep the
  existing contract (error, no failover), now naming the reset time.
  Alternative: windows are advisory only for hard policy and the upstream
  decides.
- **D8. MCP shape.** The Supervisor brief suggested extending
  `usage_summary`. *Recommended:* a **new read-only `account_usage` tool**
  instead: `usage_summary`'s contract is aggregated rows keyed by `groupBy`
  with a `scope` of proxy/runs/all; windows are per-account snapshots with a
  different shape and no `since`/`until`, so folding them in would make most
  of its inputs meaningless for one scope. Cost: one more catalog entry.
- **D9. Overage.** When Anthropic `extra_usage` is enabled, should a 100%
  window not count as exhausted? *Recommended:* **yes** (the account still
  serves). Unverified that `is_enabled` always means requests continue past
  the window; if the user disagrees, drop the rule.
- **D10. May the poller refresh an expired token?** *Recommended:* **yes,**
  only when the token is expired, via the existing `EnsureFresh` with a new
  per-account lock, never on 401. Alternative: report `unavailable /
  expired` and let the next proxied request refresh it (no new refresh
  traffic from polling, but a never-proxied account shows "expired"
  forever).
- **D11. Where UI preferences live (pins, used/remaining).**
  *Recommended:* client `localStorage` (no schema, matches Paseo).
  Alternative: daemon-side so desktop/web/mobile agree.
- **D12. Proxy error when every candidate is exhausted.** *Recommended:*
  keep 503 with the reset time in the message for v1. Alternative: a
  provider-shaped 429 with `Retry-After` (better for client retry
  behavior, but a public proxy-contract change that wants its own decision).
- **D13. Per-account opt-out.** *Recommended:* global switch only in v1.
  Alternative: `account.update` field to exclude one account.

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
- **Poll in the background on a timer.** Rejected for v1: it wakes idle
  accounts, adds constant outbound traffic with user tokens, and the
  demand-driven path already gives fresh data whenever anyone looks or the
  proxy routes.
- **Block the proxy request on a fresh fetch.** Rejected: up to a 15 s stall
  on a cold cache, on an unofficial endpoint. Cache-only routing with an
  async refresh keeps the proxy's latency independent of the provider's
  usage API.
- **Fail closed when usage is unknown.** Rejected: an unofficial endpoint
  would then be able to take routing down. Unknown must mean available.
- **Passive exhaustion from upstream 429s.** Not rejected on merit, only
  deferred: it needs no outbound polling and could cover the polling-off
  case, but it has to distinguish a quota 429 from a burst rate limit and
  would need a cooling-down state. A follow-up ADR if D2 lands on "off".
- **Least-pressure ordering in the pool.** Deferred: picking the candidate
  with the most remaining quota is a routing-policy change (and wants
  history for pace); v1 only excludes exhausted accounts.
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
- A thin new config section (`accountUsage`) and one new wsapi RPC, one
  topic, one CLI subcommand, one MCP tool, and one web component family.

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
  `refs/paseo/packages/app/src/usage/*`,
  `refs/paseo/packages/cli/src/commands/usage/*`.
- `docs/ROADMAP.md` ("Quota prediction", quota `Fetcher` stub) and
  `docs/plans/active/orchestration-and-metering.md` (the "feed the router's
  `TokensUsed`" backlog item, superseded by this ADR).
