# Account usage windows (plan quota) → routing and UI

Size: L (8) — scope 2, PRs 2, risk 2 (user tokens, outbound calls, refresh races), decisions 0 (all answered), verify 2 (UI screenshots; manual live-provider check)

Status: **spec, ADR accepted** (2026-10-11), docs-only so far. Design:
[ADR-0028](../../decisions/0028-account-usage-windows.md) (**Accepted**).
D1–D4 and D6–D13 were accepted as the recommended defaults; **D5 changed**:
the web UI ports Paseo's Usage UX (PR-D1/D2 below). PR-A may start, but its
live-endpoint probes need Human go-ahead first (see Validation); until then
it is fixtures-only.

**Amended 2026-10-12, round 2 (Human):** the sidebar footer Usage item is
**off by default** (enabled in Settings → Usage); plan usage shows on **hover
of the usage icon in each chat** (PR-E, now **required**) and in **Settings →
Usage**; order **D1 → E → D2**; accounts are named by `accounts.label`
(fallback Claude/Codex only if empty or a provider default); Settings → Usage
plugs into the registry of the full-screen Settings screen (parallel PR
`feat/settings-fullscreen`); and a zero-extra-call **passive rate-limit
signal** is **D14 (Proposed, needs the Human)** with a conditional **PR-F**
(AC59–AC66).

**Amended 2026-10-12 (Human, firm):** usage endpoints are rate-limited; never
call them carelessly. ADR-0028 §3 "Call budget (hard rules)" is binding;
AC44–AC54 and the `budget_*` tests below pin it. Nothing in this plan may add
a fetch path that does not go through `quota.Poller`'s `Touch`/`Refresh`.

Goal: smind learns each OAuth account's real plan windows (5-hour, weekly,
per-model weekly; reset times) from the provider, uses them so the proxy's
pool failover stops sending traffic to an exhausted account, and shows them
in the web UI, CLI and MCP. Today `serve.go` wires a `noopQuotaFetcher`, so
`routing.Router.isExhausted` never fires.

Not in scope: routing smind's own runs (ADR-0020), per-run token usage
(unchanged), pace prediction (`runsOutAt`; the UI port omits Paseo's
"at risk / runs out" row), usage history, API-key balance
fetchers, providers other than Anthropic and OpenAI/ChatGPT OAuth,
least-pressure pool ordering, passive 429-based cooldown, per-account
opt-out, de-duplicating two smind accounts that hold the same login.

## Acceptance Criteria

Data model and storage (PR-A)

- **AC1** `quota.Report` / `Window` / `Balance` / `Problem` exist as in the ADR;
  `quota.Usage` and `noopQuotaFetcher` are gone; `quota.Fetcher.Fetch` takes
  an `accounts.Account` and returns a `Report`.
- **AC2** `quota_snapshots.usage_data` stores `Report` JSON with `"v":2`; no
  change to `quota_snapshots` and no SQL migration entry (the one new table is
  AC52). A legacy payload (no `"v"`) or a
  too-new `v` decodes to "no report" without error and is overwritten by the
  next successful fetch.
- **AC3** The store has `GetLatestQuotaSnapshot(accountID)`; after an insert
  the poller keeps only the latest row for that account (older rows deleted
  in the same transaction). `Poller` no longer lists the whole table.
- **AC4** `quota.AccountKey` returns `<provider>-<id>`, matches
  `^[A-Za-z0-9._-]{1,128}$`, and does not change when the label, credential or
  token changes.

Fetchers (PR-A)

- **AC5** Anthropic OAuth accounts: a successful response yields windows
  `five_hour` and `weekly` (both `summary`), legacy `seven_day_<model>` and
  `limits[]` `weekly_scoped` entries as scoped windows (one window per scope,
  legacy and `limits[]` twins reconciled), and `OverageEnabled` from
  `extra_usage.is_enabled`.
- **AC6** OpenAI OAuth accounts: `primary_window` / `secondary_window` become
  windows named by `limit_window_seconds` (18000 → `five_hour`, 604800 →
  `weekly`, else `<n>s`, unknown duration → `unknown_primary` /
  `unknown_secondary`); `additional_rate_limits` and `code_review_rate_limit`
  become `feature`-scoped windows; `credits.balance` becomes a balance;
  `plan_type` becomes `PlanLabel`; the request carries `ChatGPT-Account-Id`
  from the access token's claim when present and omits it when not.
- **AC7** API-key accounts and unsupported providers return `unavailable /
  no_quota` and make **zero** HTTP requests.
- **AC8** Parsing is lenient: unknown fields ignored; numeric strings
  accepted; a malformed entry is skipped and its siblings survive; a body
  that is HTML, not JSON, or yields no windows is an `error` result
  (`"malformed response"`), never an empty `available`.
- **AC9** 401/403 → `unavailable / rejected` (no forced refresh, no retry);
  429 → transient error carrying `Retry-After`; 5xx, timeout, network error
  → transient error; an expired token is refreshed once via
  `Registry.EnsureFresh`, and a refresh failure → `unavailable / expired`.
- **AC10** `Registry.EnsureFresh` is serialized per account: two concurrent
  callers for the same expired account cause exactly one refresh call.
- **AC11** Fetches use only smind's stored credential, only the two fixed
  hosts (no redirects, 15 s timeout, 1 MiB cap), through the same uTLS
  clients the proxy uses; no config or account field can change the URL.

Poller (PR-A)

- **AC12** `Poller` has exactly three entry points (AC44): `Peek` does no
  I/O; `Touch` and `Refresh` are the only paths to the network. The cache is
  served inside the TTL (default and minimum 5 min), concurrent callers share
  one fetch (AC47), and no entry point returns an error for a fetch failure (it
  returns the last report marked `stale`, or "no report").
- **AC13** Backoff, `Retry-After`, rejected handling and persistence are
  specified by AC49–AC52 (they replace the earlier in-memory backoff wording).
- **AC14** A failed fetch never overwrites a stored `available` report.
- **AC15** With `accountUsage.enabled: false` no outbound usage call is ever
  made, `Peek`/`Touch`/`Refresh` return "no report", and the daemon starts
  normally. `accountUsage.{enabled,ttlSeconds,routing,exhaustThresholdPct}`
  load with the ADR defaults and reject invalid values (`ttlSeconds < 300`,
  threshold outside 1–100); the call-budget constants (30 s floor, 6/hour, 2
  in flight, 5 min `Retry-After` default, 6 h cap, 1→30 min backoff) are not
  configurable.
- **AC16** No token, refresh token, JWT, header or response body appears in
  any log line, stored report, wsapi payload or event; the stored `Error`
  and `Problem.Detail` come from a fixed vocabulary.

Call budget (hard rules, PR-A; ADR-0028 §3). Every count below is asserted
on an `httptest` server that counts requests; the clock is injected.

- **AC44** *Entry points.* `quota.Poller` exposes `Peek`, `Touch`, `Refresh`.
  `quota.Reader` (interface: `Peek` only) is what routing, the proxy and the
  event path hold, so no fetch path exists from them at compile time.
  `Peek` never makes a request, whatever the cache state.
- **AC45** *Touch floor.* At most **1 network fetch per account per TTL (5 min)**,
  counted from the last attempt (success or failure). `Touch` within the TTL,
  or during backoff, or for a rejected account, makes 0 requests and returns
  the cache.
- **AC46** *Refresh floor and budget.* `Refresh` makes at most **1 request per
  account per 30 s** and **6 per account per rolling hour**. Beyond either, it
  returns the cached report with `stale`, `nextRefreshAt` and
  `refreshBlocked` (`floor` | `budget` | `backoff` | `rejected`); it is not
  an error and is **never queued** (no extra call happens later because of
  it). Derived ceiling: at most **18 requests per account per hour** under any
  call pattern. Refresh-all runs at most **2 fetches in flight**.
- **AC47** *Single-flight.* Concurrent `Touch`/`Refresh` callers for one account
  (tabs, CLI, MCP, an in-flight fetch) share one in-flight request and all
  get its result; a `Refresh` that joins an in-flight fetch spends no budget.
- **AC48** *No stray triggers.* Daemon startup, account creation, `account.updated`,
  `account.updateCredential`, a client reconnect and the passage of time
  make **0 requests**; there is no background timer; the state table is read
  lazily, on the first request about an account.
- **AC49** *429.* `Retry-After` (delta-seconds or HTTP-date) is honored; absent,
  the wait is at least **5 min**; capped at 6 h; consecutive failures add an
  exponential backoff 1, 2, 4 … 30 min with **additive** 0–20% jitter; next
  attempt at `now + max(retryAfterOrDefault, backoff + jitter)`; jitter never
  shortens a `Retry-After`.
- **AC50** *Other failures.* 5xx, timeout, network error and malformed/HTML/empty
  bodies use the same 1 → 30 min backoff; success resets the count and the
  backoff; a token-refresh call made for an expired credential obeys the same
  backoff and counts as an attempt.
- **AC51** *Rejected.* 401/403 and a terminal refresh failure (`invalid_grant`)
  produce `unavailable` and **no retry (touch or refresh) until the credential
  fingerprint changes** (a re-login, `account.updateCredential` or a
  proxy-refreshed token changes it; re-saving the same credential does not).
  The fingerprint (8 bytes of SHA-256) is never logged or sent on the wire.
- **AC52** *Persistence.* New table `quota_poll_state` (`account_id` PK,
  `last_attempt_at`, `next_allowed_at`, `consecutive_failures`, `forced_at`
  JSON of at most 6 unix-ms, `rejected_fp`) is created from `schema.sql`
  (`CREATE TABLE IF NOT EXISTS`, which applies to existing databases) and
  deleted with the account. The attempt is written **before** the request
  (write-ahead). A daemon restart resets nothing: not the TTL, the backoff,
  the forced-refresh budget, nor the rejected state. A persisted
  `next_allowed_at` more than 6 h ahead (clock change) is clamped to 6 h.
- **AC53** *Callers (PR-B, PR-C).* The proxy and routing cause **0** requests
  (AC23). `account.usage` defaults to `cached`; a reconnect re-list is `cached`;
  the CLI is `touch` unless `--refresh`; MCP `account_usage` is `touch` only;
  a burst of 10 websocket clients touching at once yields 1 request; a
  refused refresh returns the cache with the reason, never an error and never
  a queued call.

Routing (PR-B)

- **AC17** `Router.Route` accepts `routing.WithModel(m)`; the proxy passes
  the model it already determined; with no model, only account-wide windows
  count.
- **AC18** Counted windows: account-wide windows always; `model`-scoped only
  when `AppliesTo(model)` is true; `surface`/`feature` never.
- **AC19** An account is exhausted iff a counted window has `UsedPct >=
  threshold` (or `RemainingPct <= 0`) and `now < ResetsAt`; a window whose
  `ResetsAt` has passed counts as reset without a refetch; a counted
  exhausted window with no `ResetsAt` counts only while the snapshot is
  `<= 2 × ttl` old; `OverageEnabled == true` suppresses exhaustion.
- **AC20** Unknown never exhausts: no snapshot, `error`, `unavailable`,
  legacy/too-new payload, `enabled:false`, `routing:false`. The router never
  returns a poller error, and `Route` makes no network call (a blocked
  fetcher does not delay it).
- **AC21** An exhausted verdict from the last good report stands through
  failing refetches until `ResetsAt`.
- **AC22** Pool: first non-exhausted candidate in order; all exhausted →
  error naming the earliest reset. Hard: exhausted → error naming the reset
  time, unknown → routes. Affinity: a pinned exhausted account is replaced;
  a pinned account under the threshold is kept regardless of other
  candidates' usage.
- **AC23** Routing and the proxy never cause a fetch (AC44): over 1000 routed
  requests with a cold cache, a stale cache, and an account in backoff, the
  usage `httptest` server sees **0 requests**, and `Route` returns without
  waiting on anything.

Surfaces: wsapi, CLI, MCP (PR-C)

- **AC24** `account.usage` takes `{accountIds?, mode?: "cached"|"touch"|"refresh"}`,
  default `"cached"`, and returns `{enabled, ttlSeconds, reports[]}` with the ADR's
  shape (including `stale`, `nextRefreshAt`, `refreshBlocked`); an unknown id
  is an error; `cached` makes 0 requests; `touch` and `refresh` map to AC45/AC46
  and wait up to 20 s for a fetch they started or joined; a refused refresh
  returns the cached report with `refreshBlocked` (not an error); `refresh` from
  a run-scoped caller is treated as `touch`.
- **AC25** `account.usage` events are emitted after every completed network
  fetch (success, `unavailable` or error), so `fetchedAt` advances even when
  the numbers did not change, and not for a cache hit; a refresh without
  `accountIds` fetches all eligible accounts in parallel, one event each as
  it settles; the topic is accepted by `events.subscribe`.
- **AC26** `smind account usage [--refresh] [--json]` uses `touch` (and
  `refresh` only with `--refresh`), prints per account one
  line per window with used % and time to reset, prints unavailable accounts'
  problems in words, and exits 0 when the daemon answered.
- **AC27** The MCP tool (D8: `account_usage`) is read-only, present in the
  catalog and in `mcp_instructions.go`, uses `touch` only, and has no refresh
  input. `usage_summary` and `usage.summary` are unchanged.
Web: Paseo's Usage UX ported (PR-D1 Settings + modal, PR-E chat hover card, PR-D2 footer item and pins)

- **AC28** (D2) Sidebar footer Usage item, **off by default**: absent on a
  fresh profile; the **Show summary in sidebar** switch in Settings → Usage
  (and the modal's Settings control) turns it on; when on, the existing footer
  shows each account's provider logo followed by its default windows as "31% 5h",
  "54% wk" (windows flagged `summary`; the first window with a percent when
  none is flagged); unavailable and error accounts are left out; with no
  summary data the row reads plain "Usage"; the row exists only while the
  switch is on, `accountUsage.enabled` is true and the daemon supports
  `account.usage`; it follows the account filter; the icon-collapsed rail
  shows the icon only; turning the switch off removes the row and makes
  window rows plain bars.
- **AC29** (D2) The footer line shows the richest layout that fits one line
  (meters + percent + short label, else percent + short label, else percent
  alone), shows labels until widths are measured, never overflows or wraps,
  and a window with an empty short label shows its percent alone.
- **AC30** (D2) Pins: while "Show summary in sidebar" is on, every window row is
  a checkbox that pins it (whole row clickable, pin glyph with tooltip,
  half-strength hover highlight, accessible name `Pin <display name> <Window>,
  <value> · <reset>` with the checked state); the first toggle snapshots the
  displayed defaults and then toggles one window; pins render in report
  order grouped by provider; a pin matches the window on every account of
  that provider; an explicit empty selection persists across reload and does
  not fall back to defaults; with the switch off rows are plain bars.
- **AC31** (D2) "Percentages" Used / Remaining switches the footer, cards and
  Settings together ("69% left"), without touching pins, and persists.
- **AC32** (D1) Usage opens as a modal over the current screen, from the chat
  hover card's "Open Usage" link, the command palette ("Usage: open plan
  usage", AC68) and the footer item when it is on: a dialog on
  wide layouts, a bottom sheet (at most 80% of the height, backdrop visible)
  on narrow ones; opening and closing do not change the route or the open
  task, tabs or scroll underneath; the modal mounts on first open and
  unmounts after it closes; a long list scrolls to its last window at
  1440×900 and 390×844.
- **AC33** (D1) Account filter in the modal title row: hidden with one or
  zero accounts; "All accounts" by default, otherwise one account; the pick
  persists in `localStorage`, is the account the footer item (when on) and
  Settings → Usage show and the modal opens on after a reload, orders the
  chat hover card's accounts (AC55), and falls back to All when that account no
  longer exists.
- **AC34** (D1) Cards: provider logo and the account's **display name** (AC58;
  a muted provider name when the display name is not already the provider's),
  plan badge, a status dot with
  "Unavailable" / "Error" when not available, one bar per window (label,
  percent used or "N% left", reset countdown "resets 2h" / "resetting now",
  a meter coloured ok / warning / danger at the 70 / 90 thresholds, muted
  when the tone is default), balances ("$x / $y" or "x left"), detail rows,
  and the report's freshness ("Updated 3m
  ago") in the refresh button's tooltip, and printed on the card on narrow
  or touch layouts.
- **AC35** (D1) Problem states are explained in words: expired ("Login
  expired 2h ago. Reconnect it in Providers."), rejected ("Login rejected
  (HTTP 401). Reconnect it in Providers."), no_quota (the report's detail),
  error (fixed-vocabulary text plus "retry in 3 min" while backing off),
  disabled (names `accountUsage.enabled`, nothing to refresh); a failed
  refresh keeps the previous report on screen and adds "Unable to refresh
  usage"; an account that recovers replaces its message with windows.
- **AC36** (D1) Refresh: a card's button sends `account.usage {accountIds:[id],
  mode:"refresh"}` for that account only; it is disabled for 30 s after a press
  and, when the daemon reports `refreshBlocked`, disabled with the reason and
  time ("Rate limited, retry in 12 min", "Refreshed just now", "Refresh limit
  reached, next in 24 min", "Reconnect this account to refresh"); Refresh all sends one refresh for
  every account; both show a spinner and are disabled while pending; other
  cards are untouched; a report with a later `fetchedAt` is never replaced
  by an earlier one when a refresh races a list or an event; plain list
  loads never force.
- **AC37** (D1) Settings → Usage is a section registered with
  `registerSettingsSection` (id `usage`, kept): a **Plan usage** block above
  the existing table, which is retitled **Token usage** and otherwise
  unchanged; the block header (inside the section body, not the Settings
  screen's own header) has Refresh all and a visible **Settings** control
  holding the **Show summary in sidebar** switch (default off) and the
  Used/Remaining control; it shares pins, toggle and account pick with the
  modal, footer and hover card; disabled shows the disabled message; a
  daemon without `account.usage` shows "Update the daemon to see plan usage"
  and the UI makes no `account.usage` call.
- **AC38** (D1) Live updates: an `account.usage` event upserts that account's
  report in every open surface without a refetch (a new account is
  appended); `account.removed` drops it; a reconnect re-lists.
- **AC39** (D1) Robustness: a missing, null or reshaped field renders as "not
  shown" (percent "—", window left out of the summary), never throws; unknown
  providers get a generic gauge logo and their provider id as the name.
- **AC40** (D1+D2) No new web dependency and no data-fetching library; the
  Used/Remaining control is a small shared segmented control component;
  keyboard works (rows focus and toggle with Space, modal traps focus and
  closes on Escape, controls have accessible names).
- **AC41** (D1+D2) Required screenshot pass: light and dark, 1440×900 and
  390×844, for footer (defaults, pinned, remaining, collapsed rail, no
  data), modal (one and several accounts, long list, each problem state,
  refresh in flight, options open) and Settings → Usage; looked at and fixed
  by the implementer before the PR is opened; attached to the PR.
- **AC54** (D1+D2) Client call discipline (ADR-0028 §3 rules 4): a reconnect
  re-lists with `mode:"cached"`; the footer item (when on) and an open
  modal/Settings → Usage send `mode:"touch"` at most at the TTL cadence and **only while the
  window is visible and focused** (timer paused when hidden or blurred; on
  becoming visible a touch is sent only if the last report is older than the
  TTL); with the footer item off and no modal or Settings → Usage open the web UI
  sends no `touch` (the default state sends none at all); opening the modal or
  Settings → Usage sends one `touch`; the chat hover card sends only
  `mode:"cached"` (AC56); the client never sends `refresh` except from a
  button press.
- **AC42** (E, **required**) Chat plan usage on hover: the chat header's usage
  pill (`ChatUsageTotal`) and each run's usage pill (`RunUsagePill`) open a hover
  card (pointer hover or keyboard focus; Escape closes) containing the existing
  token rows unchanged and, below them, a **Plan usage** section for the
  chat's provider (`claude-native` → `anthropic`, `codex-native` → `openai`; any
  other provider shows no section). The trigger is shown when the chat has
  token usage **or** plan data for its provider, so a chat with no reported
  tokens can still show plan usage.
- **AC55** (E) Which accounts: **every OAuth account of that provider**, compactly
  (display name, plan badge, one meter line per `summary` window with
  percent and reset, freshness), the filter-selected account first; with
  exactly one account the section has no note; with several it says "This
  chat's login isn't tracked; showing all <Provider> accounts"; API-key
  accounts and a provider with no OAuth account show no section; unavailable
  accounts show their problem in one line. No pins, no refresh button.
- **AC56** (E) **Cache only, never a fetch**: opening, hovering, focusing or
  tapping the card sends at most one `account.usage` with `mode:"cached"` (only
  when the client store is empty) and never `touch` or `refresh`; with no
  cached data the section reads "No plan data yet" and offers "Open Usage"
  (which opens the modal, where an explicit surface fetches under the budget);
  data older than the TTL shows its age ("Updated 40 min ago") and is not
  refetched by the card.
- **AC57** (E) Input modes: on hover-capable pointers the card opens on hover
  (with open/close delay) and stays open while the pointer is over it; on touch
  devices a tap opens the same content in the existing popover; the card
  honours Used/Remaining; with polling disabled or the daemon lacking
  `account.usage` the Plan usage section is absent and the existing token
  content is unchanged (existing `chat-usage-total` / `run-usage-pill` tests
  pass untouched).
- **AC58** (A1, C, D1) **Display name.** `quota.DisplayName(account)` returns
  the trimmed `accounts.label`; when the label is empty or equals a provider
  default (`anthropic`, `openai`, `claude`, `codex`, `default`,
  case-insensitive) it returns the provider default name ("Claude" for
  `anthropic`, "Codex" for `openai`); the wire carries `displayName`; the web
  cards, account filter, pin labels, footer accessible name, hover card, the
  CLI and the MCP tool all use it; it never appears in the account key or in
  logs.
- **AC67** (D1) Registry-driven Settings: Settings → Usage renders correctly in
  the current in-shell Settings layout **and** in the full-screen Settings screen
  (`feat/settings-fullscreen`: left settings sidebar, framed content view,
  narrow list → detail page): no dependence on the app sidebar being visible,
  no fixed width, its own controls inside its body, existing `settings-*` test
  ids untouched; a section test mounts it inside both containers.
- **AC68** (D1) Command palette: "Usage: open plan usage" opens the Usage modal
  (and from inside Settings, the modal portals above the Settings screen).

PR-F, conditional on D14 (Proposed; ADR-0028 §7). Not started until the Human
accepts D14. The observer is write-only and never fetches.

- **AC59** `quota.Observer` (write-only: `Observe(account, headers, status, now)`)
  is the only thing the proxy holds for passive data; it has no path to
  `Touch`/`Refresh`/the network; across proxied traffic the usage `httptest`
  server sees **0** requests; with `accountUsage.passive: false` or
  `enabled: false` it is a no-op.
- **AC60** Anthropic parse (allowlisted names only): `…-5h-status`/`-reset`/
  `-utilization`, `-7d-*`, `-unified-status`, `-reset`, `-representative-claim`,
  `-overage-status`; utilization fraction × 100 → `UsedPct`, epoch seconds →
  `ResetsAt`; `5h` → `five_hour`, `7d` → `weekly`, `7d_oi` → a `feature`-scoped,
  display-only window; overage `allowed`/`rejected` → `OverageEnabled`;
  unknown, missing, non-numeric, over-long (> 512) or control-character values
  and more than 64 headers are ignored; raw header values are never stored or
  logged.
- **AC61** Exhaustion rule: only an explicit `rejected` window status (or a
  utilization at or above the threshold) marks a window exhausted; a **bare
  429** (no `rejected` header, or 5h and 7d `allowed`) never does; `Retry-After`
  alone never does.
- **AC62** Codex parse (fixtures-only, expected inert through the proxy):
  `x-codex-primary-*` / `x-codex-secondary-*` percent, window-minutes → window
  name (300 → `five_hour`, 10080 → `weekly`), reset-at, additional families by
  prefix → `feature` scope with `-limit-name`, `x-codex-credits-*` → balance,
  `x-codex-plan-type` → `PlanLabel`; `x-ratelimit-*` is ignored.
- **AC63** Precedence: `Peek` returns the fetched report with passive windows
  overlaid per window id only when the passive observation is newer than the
  fetched report and (the fetched report is older than the TTL, or the passive
  window is `rejected`); a fresh fetched report's numbers win; passive windows
  are flagged `source:"passive"`.
- **AC64** Storage and write rate: `quota_snapshots.kind` (`fetched` |
  `passive`, default `fetched`, `addColumnIfMissing` migration entry),
  retention keeps the latest row per account and kind; a passive write happens
  only when the parsed observation changed and at most once per account per
  60 s unless the exhausted verdict flips; passive writes never touch
  `quota_poll_state`, the TTL or the forced budget; the proxy never blocks on
  the write.
- **AC65** Routing: a passive `rejected` 5-hour or 7-day window makes the account
  exhausted for the very next routed request, until its reset, with **0** usage
  requests; a passive `allowed` newer than a stale fetched "exhausted" verdict
  clears it; `7d_oi` and feature windows never count.
- **AC66** Surfaces: wire `Window.source` / `Window.status`; tone from status when
  there is no percent (`allowed_warning` warning, `rejected` danger); the UI
  marks passive windows "from recent traffic"; an `account.usage` event is
  emitted when the effective report changes; `accountUsage.passive` (default
  true) loads and validates.

Process

- **AC43** `task test` and `task lint` pass on every PR; the ADR's D-list is
  not reopened inside a PR (a change needs the Human, AGENTS.md rule d); the plan
  moves to `docs/plans/completed/` when every AC has a Validation line;
  `docs/ROADMAP.md` ("Quota prediction", quota `Fetcher` stub), `README.md`
  (config/CLI mention) and the stale comment in the orchestration-and-metering
  backlog are updated in the closing PR.

## Test Scenarios

Fixtures live in `internal/quota/testdata/` as redacted JSON/HTML files;
tests serve them from `httptest.Server` through an injected transport. **No
test calls a real endpoint.**

### Go table tests: Anthropic parser (`internal/quota/anthropic_test.go`)

| Name | Fixture / input | Expect |
|---|---|---|
| `anthropic_full` | `five_hour`, `seven_day`, `seven_day_opus`, `extra_usage.is_enabled:true` | 3 windows, ids `five_hour`/`weekly`/`weekly_model_opus`, summary on the first two, `OverageEnabled=true` |
| `anthropic_limits_array` | `limits[]` with `weekly_scoped` model + surface entries, no legacy keys | scoped windows with scope kind `model` / `surface`, ids unique and stable |
| `anthropic_legacy_and_limits_twin` | `seven_day_opus` and a `limits[]` Opus entry | exactly one Opus window; limits[] values win, nil fields fall back to legacy |
| `anthropic_null_utilization` | `five_hour:{utilization:null}` | window present with nil `UsedPct`, tone default |
| `anthropic_numeric_string` | `"utilization":"41.5"` | 41.5 |
| `anthropic_one_bad_entry` | one `limits[]` entry of the wrong type | that entry skipped, other windows kept |
| `anthropic_unknown_fields` | extra `email`, `organization`, `foo` keys | ignored; stored JSON contains none of them |
| `anthropic_empty_object` | `{}` | `error` "malformed response" (no windows) |
| `anthropic_truncated_json` | cut-off body | `error` "malformed response" |
| `anthropic_html` | `<html>…` 200 | `error` "malformed response", body not stored |
| `anthropic_401_403` | status 401 / 403 | `unavailable / rejected` with status; one request only (no retry) |
| `anthropic_429_retry_after_seconds` / `_http_date` / `_absent` | 429 | transient error, `RetryAfter` 120 s / parsed / 0 |
| `anthropic_5xx_timeout_network` | 503, slow server, closed conn | transient error each, no panic |
| `anthropic_redirect` | 302 to another host | not followed; `error` |
| `anthropic_oversize_body` | > 1 MiB | `error` |
| `anthropic_request_shape` | any 200 | `Authorization: Bearer <test token>`, `anthropic-beta: oauth-2025-04-20`, GET, fixed host/path, no query |

### Go table tests: OpenAI parser (`internal/quota/openai_test.go`)

| Name | Fixture / input | Expect |
|---|---|---|
| `openai_full` | primary 18000 s, secondary 604800 s, `plan_type`, credits | windows `five_hour`, `weekly` (both summary), `PlanLabel`, credits balance |
| `openai_unknown_duration` | `limit_window_seconds` null | `unknown_primary` / `unknown_secondary` |
| `openai_epoch_reset` | `reset_at` epoch seconds | `ResetsAt` correct UTC |
| `openai_additional_limits` | named and unnamed entries | named → `feature` scope id `limit:<escaped>`; unnamed skipped |
| `openai_code_review` | `code_review_rate_limit` | `feature` scope "Code review" |
| `openai_credits_string` | `"balance":"12.5"` | 12.5 |
| `openai_no_rate_limit` | only `plan_type` | `error` "malformed response" (no windows) |
| `openai_html` / `_401` / `_403` / `_429` / `_5xx` | as Anthropic | same mapping |
| `openai_account_id_header` | access token JWT with `chatgpt_account_id` | header sent; absent claim → header omitted; claim never appears in stored report or logs |
| `openai_email_dropped` | response with `email` | not in stored JSON |
| `openai_request_shape` | any 200 | bearer + UA + fixed host/path |

### Go tests: dispatch, refresh, account key (`internal/quota`, `internal/accounts`)

- `fetcher_apikey_no_call`: anthropic and openai `api_key` accounts, custom `base_url` → `no_quota`, transport saw 0 requests.
- `fetcher_unsupported_provider`: `kimi` oauth account → `no_quota` "provider not supported", 0 requests.
- `fetcher_expired_token_refreshes_once`: expired credential → refresher called once, new token used in the request, credential persisted.
- `fetcher_refresh_failure_expired`: refresher error → `unavailable / expired`, 0 usage requests.
- `fetcher_valid_token_no_refresh`: unexpired → refresher never called.
- `ensurefresh_concurrent_single_refresh`: 8 goroutines, one expired account → refresher called exactly once, all get the same fresh credential (race detector on).
- `ensurefresh_other_accounts_independent`: two accounts refresh in parallel (lock is per account).
- `displayname_uses_label`: label "Work Max" → "Work Max"; surrounding whitespace trimmed; an email label is kept as written.
- `displayname_empty_label_falls_back_to_provider_default`: "" and "   " → "Claude" (anthropic) / "Codex" (openai).
- `displayname_provider_default_label_falls_back`: labels `anthropic`, `Claude`, `OPENAI`, `codex`, `default` → the provider default name; a label like "Claude work" is **kept**.
- `displayname_unknown_provider_falls_back_to_provider_id_titlecased`.
- `displayname_never_in_key_or_logs`: the account key and captured logs never contain the label.
- `accountkey_stable`: rename, `updateCredential`, token refresh → same key; regex holds for provider+id inputs; label containing an email never appears in the key.
- `report_roundtrip_and_versioning`: v2 round-trips; legacy `{"tokens_used":0,…}` → no report; `v:3` → no report.
- `tone_thresholds`: nil → default, 70 → warning, 90 → warning, 90.1 → danger, 69.9 → ok.
- `window_appliesto`: table of (scope, model) → bool: Opus vs `claude-opus-4-1` true, vs `claude-sonnet-4-5` false, surface/feature always false, nil scope always true, empty model + model scope false.

### Go tests: poller (`internal/quota/poller_test.go`)

- `poller_ttl_hit_no_fetch`; `poller_ttl_expired_touch_refetches`.
- `poller_peek_no_io`: `Peek` with a blocking fetcher returns immediately and starts nothing.
- `poller_error_keeps_last_good`: good then failing fetch → stored report unchanged, returned `stale`.
- `poller_prunes_old_snapshots`: after N inserts one row per account remains.
- `poller_disabled_no_calls`: `enabled:false` → 0 requests across `Peek`, `Touch`, `Refresh`.
- `poller_account_deleted`: snapshots and `quota_poll_state` gone, no panic on a late fetch result.
- `poller_notifies_after_every_completed_fetch`: callback fires after each network fetch (including an unchanged result and an error), never on a cache hit.
- `config_accountusage_defaults_and_validation` (`internal/config`): defaults; `ttlSeconds < 300` rejected; threshold outside 1–100 rejected; budget constants are not config keys.

### Go tests: call budget (PR-A, `internal/quota/budget_test.go`)

Harness: an `httptest` server with `Calls()` (total and per path) and
`MaxConcurrent()`, an injectable fake clock, and a helper that closes and
reopens the SQLite store at the same path to simulate a daemon restart. Every
test asserts an **exact** request count.

Entry points and floors (AC44–AC47):
- `budget_reader_has_no_fetch_surface`: compile-time assertion that `quota.Reader` has only `Peek`, and that `*Poller` satisfies it.
- `budget_peek_never_calls`: cold cache, stale cache, backoff, rejected: 1000 `Peek` → 0 requests.
- `budget_touch_within_ttl_zero_calls`: first `Touch` → 1; 100 more within the TTL → still 1.
- `budget_touch_ttl_boundary`: at TTL−1 s → 0 more; at TTL → exactly 1 more.
- `budget_touch_ttl_counts_from_last_attempt_not_last_success`: a failed attempt restarts the TTL (and backoff) clock.
- `budget_burst_50_concurrent_touch_one_call`: 50 concurrent `Touch` on one account → **1** request, all 50 get the same result.
- `budget_burst_50_concurrent_mixed_touch_refresh_one_call`: 25 `Touch` + 25 `Refresh` → **1** request; the joining refreshes spend no forced budget.
- `budget_single_flight_is_per_account`: 50 concurrent calls across 5 accounts → exactly 5 requests.
- `budget_forced_floor_30s`: 10 `Refresh` within 30 s → 1 request, the other 9 returned from cache with `refreshBlocked:"floor"` and `nextRefreshAt`; at 30 s one more is allowed.
- `budget_forced_hourly_cap`: `Refresh` every 31 s for an hour → exactly **6** requests; the 7th is refused with `refreshBlocked:"budget"` and `nextRefreshAt` = oldest forced + 1 h.
- `budget_excess_refresh_not_queued`: after refused refreshes, advance the clock 2 h with no further calls → **0** extra requests (nothing was queued).
- `budget_hourly_ceiling_adversarial_client`: a client alternating `Touch` and `Refresh` every second for one fake hour → **≤ 18** requests.
- `budget_refresh_all_max_two_in_flight`: 6 accounts → 6 requests total (one each), `MaxConcurrent() ≤ 2`.
- `budget_refresh_all_skips_blocked_accounts`: accounts in backoff/rejected → 0 requests for them.

No stray triggers (AC48):
- `budget_startup_zero_calls`: build `Poller` + registry over accounts that already have snapshots and state; run the fake clock 24 h → **0** requests; `quota_poll_state` not read before the first request about an account (query counter).
- `budget_account_events_zero_calls`: `NotifyAccountUpdated`, `account.updateCredential` (valid or rejected-fp change), account creation → **0** requests until a `Touch`.
- `budget_no_background_timer`: with accounts configured and the clock advanced 24 h, a goroutine/timer leak check and **0** requests.

Failures (AC49–AC51):
- `budget_429_retry_after_seconds`: `Retry-After: 600` → 0 requests for 600 s (touch and refresh), then exactly 1.
- `budget_429_retry_after_http_date`: parsed correctly against the fake clock.
- `budget_429_absent_header_min_5m`: no header → 0 requests for 5 min, then 1.
- `budget_429_retry_after_capped_6h`: `Retry-After: 999999` → next attempt at 6 h.
- `budget_429_jitter_never_shortens_retry_after`: 200 seeded runs, wait ≥ `Retry-After` always and ≤ `Retry-After` + 20% of the backoff term.
- `budget_consecutive_failures_exponential_capped`: 503 ×8 → waits 1, 2, 4, 8, 16, 30, 30, 30 min (within jitter bounds), request count exact at each step; success → count and backoff reset.
- `budget_timeout_network_malformed_html_empty_backoff`: each failure kind backs off like a 503 (a changed endpoint is not hammered).
- `budget_refresh_call_for_expired_token_obeys_backoff`: failing token endpoint → backoff applies to both the refresh call and the usage call; counts per host asserted.
- `budget_401_no_retry_until_credential_changes`: 401 → `unavailable / rejected`; 100 `Touch`/`Refresh` over 7 fake days → **1** request total; `account.updateCredential` with a new token → next `Touch` makes exactly 1; re-saving the same token → still 0.
- `budget_403_same_as_401`; `budget_invalid_grant_terminal_until_credential_changes` (token endpoint call count too).
- `budget_proxy_refreshed_token_changes_fingerprint_allows_one_retry`.
- `budget_fingerprint_not_in_logs_or_wire`.

Persistence (AC52):
- `budget_backoff_persists_across_restart`: 429 with `Retry-After: 600`, close and reopen the store, new `Poller`: `Touch` and `Refresh` → **0** requests until 600 s, then exactly 1.
- `budget_ttl_persists_across_restart`: successful fetch, restart immediately → `Touch` → 0 requests.
- `budget_forced_budget_persists_across_restart`: 6 forced in the hour, restart → 7th refused (`budget`).
- `budget_rejected_state_persists_across_restart`: 401, restart → 0 requests until the credential changes.
- `budget_write_ahead_crash_mid_fetch`: kill the poller after the attempt row is written and before the response; reopen within the TTL → 0 requests.
- `budget_clock_skew_clamped`: persisted `next_allowed_at` 3 days ahead is clamped to 6 h; a clock that moves backwards never shortens a backoff.
- `budget_state_table_created_on_existing_db`: open a database from the previous schema → table exists, no migration entry needed.
- `budget_state_row_deleted_with_account`.

### Go tests: routing (`internal/routing/router_test.go`), fake poller reports

- `route_pool_skips_account_exhausted_on_five_hour`.
- `route_pool_skips_account_exhausted_on_weekly`.
- `route_model_scoped_window_excludes_only_matching_model`: Opus window at 100% → `claude-opus-4-1` goes to the next account, `claude-sonnet-4-5` still uses it.
- `route_unknown_model_ignores_model_scoped_windows`.
- `route_surface_and_feature_windows_never_exhaust`.
- `route_threshold_configurable`: 95% at threshold 90 → exhausted; at 100 → not.
- `route_unknown_report_not_exhausted`: no snapshot / `error` / `unavailable` each → account used.
- `route_poller_error_never_fails_routing` (the old `check quota for account N` path is gone).
- `route_exhausted_stands_while_cache_is_stale` until `ResetsAt`, then eligible again without a successful fetch (fake clock).
- `route_exhausted_without_resetsat_expires_after_2ttl`.
- `route_overage_enabled_not_exhausted`; `route_overage_nil_still_exhausted`.
- `route_all_exhausted_error_names_earliest_reset`.
- `route_hard_exhausted_errors_with_reset`; `route_hard_unknown_routes`.
- `route_affinity_exhausted_pinned_replaced`; `route_affinity_pinned_85pct_kept_over_idle_candidate`; `route_affinity_pinned_outside_candidates_ignored` (existing behavior kept).
- `route_never_fetches_call_count_zero` (PR-B): real `Poller` over the usage `httptest` server; 1000 `Route` calls with a cold cache, a stale cache and an account in backoff → **0** usage requests, `Route` never waits on I/O.
- `route_holds_reader_only` (compile-time: `Router` is built from a `quota.Reader`).
- `route_routing_disabled_never_consults_windows` (`routing:false`) and `route_polling_disabled_same_as_today`.
- Proxy (`internal/server`): `proxy_1000_requests_zero_usage_calls` (cold, stale, backoff, exhausted); `proxy_passes_model_to_router`; `proxy_exhausted_pool_returns_503_with_reset_message`; existing model-aware and fail-closed tests stay green.

### Go tests: passive signal (PR-F, conditional on D14; `internal/quota/passive_test.go`)

Fixtures are built from the header sets in the refs
(`refs/cliproxyapi/sdk/cliproxy/auth/quota_signals_test.go:68-110`,
`.../claude_executor_ratelimit_test.go:30-41,517-530`,
`refs/codex/codex-rs/core/tests/suite/client.rs:3206-3222`), not from a live
call.

- `passive_observer_is_write_only` (compile-time: no `Touch`/`Refresh`/fetcher reachable from `Observer`); `passive_zero_usage_requests_across_1000_proxied_responses` (usage `httptest` server sees 0).
- `passive_anthropic_allowed_full_header_set`: 5h 0.0, 7d 0.53, resets, representative claim, overage → windows `five_hour` 0%, `weekly` 53%, `source:"passive"`, `OverageEnabled` per `overage-status`.
- `passive_anthropic_rejected_7d`: 429 header set → weekly `rejected`, reset parsed, account exhausted.
- `passive_anthropic_rejected_5h`.
- `passive_anthropic_ordinary_429_with_allowed_windows_is_not_exhaustion` (the cliproxyapi sibling-models case): no exhaustion, no state change.
- `passive_bare_429_and_retry_after_alone_never_exhaust`.
- `passive_anthropic_7d_oi_is_display_only_feature_scope_and_never_counts_for_routing`.
- `passive_anthropic_unknown_malformed_overlong_control_char_values_ignored`; `passive_header_count_cap_64`.
- `passive_codex_primary_secondary_windows_by_minutes` (300 → `five_hour`, 10080 → `weekly`), `passive_codex_additional_family_is_feature_scope`, `passive_codex_credits_and_plan`, `passive_codex_x_ratelimit_headers_ignored`.
- `passive_precedence_fresh_fetched_numbers_win`: fetched 80% at T, passive 60% at T+1 min (fetch younger than TTL) → 80%.
- `passive_precedence_rejection_after_fetch_overrides_fresh_fetch`: fetched 80% at T, passive `rejected` at T+1 min → exhausted.
- `passive_precedence_stale_fetch_yields_to_newer_passive`; `passive_allowed_newer_clears_stale_exhausted_verdict_without_any_call`.
- `passive_older_than_fetched_is_ignored`.
- `passive_write_only_on_change_and_at_most_once_per_60s` (200 identical responses → 1 write), `passive_write_immediate_when_exhausted_verdict_flips`.
- `passive_write_does_not_touch_poll_state_ttl_or_forced_budget`: after passive writes, `Touch` still makes exactly 1 request per TTL and forced budget is unchanged.
- `passive_write_does_not_block_the_proxy_response` (slow store, response latency unchanged).
- `passive_storage_kind_column_migration_on_existing_db`; `passive_retention_latest_per_account_and_kind`.
- `passive_routing_next_request_skips_account_after_rejected_response` (end to end through the proxy with a fake upstream).
- `passive_disabled_by_config_and_by_enabled_false`.
- `passive_no_raw_headers_in_stored_json_logs_wire` (sentinel header values).

### Go tests: secrets (`internal/quota`, `internal/wsapi`)

- `no_secret_in_logs_report_event`: run every fetch outcome with a sentinel access token, refresh token and JWT; capture `log` output, stored `usage_data`, the `account.usage` RPC result and the event payload; none contains a sentinel, a response body fragment, or a provider-returned email.
- `error_vocabulary_is_fixed`: `Report.Error` and `Problem.Detail` are from the allowed set for every failure fixture (HTML body never echoed).
- `usage_account_json_keys_allowlist`: the wsapi `AccountUsage` JSON has only the documented keys.

### Go tests: wsapi, CLI, MCP

- wsapi (`internal/wsapi/account_usage_test.go`): `account_usage_all`, `account_usage_ids`, `account_usage_unknown_id_errors`, `account_usage_disabled_returns_enabled_false`, `account_usage_default_mode_is_cached_zero_calls`, `account_usage_touch_respects_ttl`, `account_usage_refresh_waits_and_returns`, `account_usage_refresh_timeout_returns_cached`, `account_usage_refresh_blocked_returns_cached_with_reason_not_error` (floor, budget, backoff, rejected), `account_usage_refresh_from_run_scoped_caller_is_touch`, `account_usage_reconnect_relist_cached_zero_calls`, `account_usage_ten_clients_burst_one_call` (10 ws clients touch at once → 1 request), `account_usage_event_after_every_fetch_not_on_cache_hit`, `account_usage_refresh_all_emits_one_event_per_account_as_each_settles`, `account_usage_topic_subscribable`, `account_usage_account_removed_event_flow` (usage state removed with the account).
- CLI (`cmd/smind`): `account_usage_default_is_touch_not_refresh`, `account_usage_refresh_flag_blocked_prints_reason_and_next_time`, `account_usage_text_output` (golden: window line, reset formatting "resets in 2h 4m", unavailable wording for expired/rejected/no_quota, stale hint), `account_usage_json`, `account_usage_refresh_flag`, `account_usage_daemon_down_exit_code`, `account_usage_no_accounts`.
- MCP: `account_usage_tool_is_touch_only` (no refresh input; 10 calls in a loop → at most 1 request per TTL), `account_usage_tool_in_catalog_and_instructions`, `account_usage_tool_readonly`, `account_usage_tool_no_refresh_input_for_run_scope`, `usage_summary_unchanged`.

### Web UI tests (vitest + Testing Library, `web/packages/ui`)

smind's web has no in-repo Playwright, so Paseo's three e2e specs
(`packages/app/e2e/browser/usage-{modal,settings,sidebar-item}.spec.ts`) are
mirrored as component-level tests against a fake `WsClient` (the existing
`App.test.tsx` style) that answers `account.usage` and can emit the
`account.usage` / `account.removed` events and record every request. Logic
tests port Paseo's unit tests (`pinned`, `model`, `preferences`, `format`,
`copy`). The fixtures reuse Paseo's: a Claude account at 31% 5h / 54% wk and a
Codex account at 7% 5h / 12% wk.

Logic (`lib/account-usage/*.test.ts`):

- `pinned.test.ts` ← `pinned.test.ts`: `choosePinnedUsageLayout` shows meters
  only when every meter fits beside the labels, drops labels before
  overflowing, shows labels until measured; `resolvePinnedUsage` defaults to
  provider-wide pins from each account, pins replace defaults and removing all
  restores defaults only when the selection is `null`, groups by provider in
  report order not pin order, names a window by its label when the source
  gives no short label, defaults to every `summary` window when any is marked,
  keeps an empty short label empty, formats "N% left" under Remaining, leaves
  out a pinned window no report has or with no percent, shows a pin once per
  account of the provider, and keeps unavailable/error reports out.
- `preferences.test.ts` ← `preferences.test.ts`: pins in the order pinned;
  unpin keeps order; Used/Remaining does not touch pins; the account pick does
  not touch pins; preferences saved before the pick existed read as no pick;
  default windows read as pinned and the first toggle removes only that row;
  an explicit empty selection survives a write/read round trip; garbage in
  storage falls back to defaults.
- `model.test.ts` ← `model.test.ts`: `displayPercent` (used, left, none);
  `usageWindowRowLabel`; `formatUsageFreshness`; `resolveUsageRefresh`
  (pending beats an earlier failure, failed, idle); `replaceReport`,
  `upsertReport` (starts a list, appends, replaces in place, keeps a copy
  fetched later), `settleReports` (drops accounts the daemon no longer has,
  keeps a copy refreshed while the list loaded); `resolveUsageView`
  (daemon too old, loading → ready → error); `resolveAccountFilter`
  (picked account, else All, unknown pick falls back).
- `format.test.ts` / `copy.test.ts`: amounts per locale and unit, percent
  rounding and clamping, "resets 2h" / "resetting now", reset days, and every
  problem sentence (expired with relative time, rejected with status,
  no_quota detail, error with retry, disabled).
- `tone.test.ts`: 69.9 ok, 70 and 90 warning, 90.1 danger, null default, the
  report's own tone wins.

Surfaces (`components/account-usage/*.test.tsx`, `settings/usage-section.test.tsx`):

From `usage-sidebar-item.spec.ts`:
- `narrow_footer_entry_points_open_usage_as_sheet` (both the row and, in the
  collapsed rail, the icon).
- `wide_usage_opens_as_dialog_over_the_workspace_and_closes_back_to_it`
  (route, open task and tab state unchanged; mounts on open, unmounts after
  close).
- `long_reports_scroll_to_the_final_window` at wide and narrow sizes.
- `pinned_windows_show_in_footer_follow_used_remaining_and_persist`: defaults
  "31% 5h", "54% wk", "7% 5h", "12% wk" with logos; pin and unpin steps;
  Remaining flips footer, accessible name ("Claude … 69% left, Codex …") and
  cards; narrow sheet checks; reload keeps pins and toggle; Settings → Usage
  shares them; unpin everything stays empty after reload and the row reads
  "Usage".
- `without_summary_data_the_row_reads_plain_usage_and_disabled_hides_it`.
- `summary_in_sidebar_switch_turns_pins_and_summary_on_and_off`.
- `provider_logos_render_and_unknown_provider_falls_back_to_gauge`.

From `usage-modal.spec.ts`:
- `opens_from_the_footer_on_the_accounts_reports_with_no_filter_for_one_account`.
- `refreshes_one_report_from_its_card`: request is `accountIds:[id],
  mode:"refresh"`; freshness text updates; other cards untouched.
- `shows_accounts_once_the_connection_is_ready_after_a_cold_load_on_a_phone`.
- `tells_the_user_to_update_a_daemon_without_account_usage` (no call made).
- `account_picked_in_the_modal_is_the_footers_account_after_a_reload`, plus
  `vanished_account_pick_falls_back_to_all`.
- `expired_login_refreshes_to_windows_with_visible_pin_toggles`.
- `narrow_rows_always_show_the_pin_glyph`.
- `codex_window_shapes_use_provider_durations_on_card_and_footer` (five_hour,
  weekly, `unknown_primary` / `unknown_secondary`, feature-scoped windows).
- `each_problem_state_is_explained` (expired, rejected, no_quota, error with
  retry, disabled) and `an_account_that_recovers_shows_only_its_windows`.
- `refresh_failure_keeps_previous_report_and_says_so`.
- `settings_control_opens_with_switch_and_percentages` at wide and narrow.

From `usage-settings.spec.ts`:
- `settings_usage_renders_every_account_returned`.
- `refresh_all_forces_a_fresh_report` (23% → 64%; exactly one forced
  request; plain loads never force).
- `asks_to_update_a_daemon_without_account_usage_and_never_calls_it`.
- `plan_usage_block_sits_above_token_usage_and_token_table_is_unchanged`.
- `settings_control_is_visible_above_the_reports`.

Call discipline (AC54), with a fake timer and a fake `document.visibilityState` / focus:
- `footer_touches_at_ttl_cadence_only_while_visible_and_focused` (exactly one `touch` per TTL; none before it).
- `hidden_or_blurred_window_pauses_the_timer_and_sends_nothing`.
- `becoming_visible_touches_only_if_last_report_older_than_ttl`.
- `reconnect_relists_with_cached_mode`.
- `footer_hidden_and_no_modal_means_no_touch`; `opening_modal_sends_one_touch`.
- `refresh_is_sent_only_from_a_button_press`; `refresh_button_disabled_30s_after_press`.
- `refresh_button_disabled_with_reason_when_daemon_reports_refresh_blocked` (backoff, floor, budget, rejected texts).
- `two_tabs_each_touch_daemon_decides` (client does not try to coordinate; daemon floor is the guarantee).

smind-specific:
- `account_usage_event_upserts_without_refetch_and_removal_drops_the_card`.
- `older_fetchedat_never_replaces_newer` (refresh racing an event).
- `reshaped_report_renders_without_throwing` (missing windows, null percent,
  unknown tone, unknown provider).
- `no_account_usage_call_while_modal_is_closed_and_footer_off` (default state: no `touch`).
- `keyboard_rows_toggle_with_space_modal_closes_on_escape`.
- `segmented_control_a11y` (`components/ui/segmented-control.test.tsx`).

Default placement and Settings (round 2):
- `footer_usage_row_absent_by_default_and_no_touch_sent` (fresh profile: no row, no `touch`).
- `show_summary_in_sidebar_switch_adds_and_removes_the_footer_row` (and window rows become pinnable only while on).
- `settings_usage_section_is_registered_and_renders_in_the_in_shell_container` and `…_in_the_full_screen_settings_container_and_narrow_detail_page` (AC67; the container is a test double of each layout; no dependence on the app sidebar; `settings-*` ids unchanged).
- `settings_usage_controls_live_in_the_section_body_not_the_screen_header`.
- `command_palette_opens_usage_modal` and `modal_opens_above_settings_screen` (AC68).
- `account_titles_use_the_label_and_fall_back_to_provider_default` on cards, account filter, pin labels, footer accessible name and hover card (AC58).

Chat hover card (AC42, AC55–AC57; fake `WsClient` records requests):
- `chat_pill_hover_shows_token_rows_and_plan_usage_for_the_chats_provider`.
- `run_pill_hover_uses_the_runs_provider`.
- `provider_map_claude_native_to_anthropic_codex_native_to_openai_others_none` (a GLM/ACP chat shows no plan section).
- `one_oauth_account_shows_it_without_a_note`.
- `several_oauth_accounts_all_listed_compactly_filter_selected_first_with_the_untracked_login_note`.
- `api_key_accounts_and_providers_without_oauth_accounts_show_no_section`.
- `hover_sends_at_most_one_cached_list_and_never_touch_or_refresh` (empty store → exactly one `mode:"cached"`; filled store → zero; 20 hovers → no `touch`, no `refresh`).
- `empty_cache_says_no_plan_data_yet_and_open_usage_opens_the_modal`.
- `stale_data_shows_its_age_and_is_not_refetched`.
- `card_has_no_pin_and_no_refresh_controls`; `honours_used_remaining`.
- `focus_opens_and_escape_closes`; `pointer_can_move_into_the_card_without_closing_it`.
- `touch_tap_opens_the_same_content_in_the_popover` (`matchMedia('(hover: none)')`).
- `trigger_shown_when_only_plan_data_exists` and `existing_chat_usage_total_and_run_usage_pill_tests_pass_unchanged`.
- `plan_section_absent_when_polling_disabled_or_daemon_lacks_account_usage`.

**Screenshot pass (required, AC41).** Playwright from the mobile workspace's
`node_modules`, against the built web UI and a temp `SMIND_HOME` daemon on a
spare port (never :4648); the `account.usage` responses and events are
supplied by the browser (`routeWebSocket`/injected stub) because the fetchers'
URLs are deliberately not overridable. Light and dark at 1440×900 and
390×844 for: footer defaults, pinned, Remaining, collapsed rail, no data;
modal with one account, several accounts, long list, every problem state,
refresh in flight, Settings control open; Settings → Usage with both blocks in
the in-shell Settings and (when available) the full-screen Settings screen,
wide and narrow; the chat hover card (one account, several accounts, no data,
stale) on the chat header pill and a run pill.
The implementer looks at them and fixes issues before reporting; the
supervisor looks at a couple before the PR is opened.

### Manual (not in CI)

- One run against a real Anthropic OAuth account and a real ChatGPT OAuth account on the author's machine, comparing the windows with `claude /usage` and the Codex app, with a throwaway log capture checked for token leakage. Redacted real responses refresh the fixtures. Result recorded in Validation.

## Implementation split (non-stacked, each branches from `origin/develop`)

Order: **PR-A1 → PR-A2 → (PR-B ∥ PR-C) → PR-D1 → PR-E → PR-D2 → close-out;** **PR-F** (passive signal) only if the Human accepts D14, after PR-B.
B and C may be worked in parallel because their write scopes do not overlap,
except one wiring line each in `cmd/smind/serve.go`; whichever merges second
rebases onto develop. D1 needs C's RPC and event merged; D2 is sequenced after
D1 is merged (not stacked on it). Points use the WORKSPACE_PROTOCOL axes.

| PR | Outcome | Write scope (one owner each) | Depends on | Size |
|---|---|---|---|---|
| **PR-A1** types, fetchers, snapshot store | AC1–AC11, AC14, AC16, AC58 (Go half): `Report` model, `AccountKey`, `DisplayName`, Anthropic and OpenAI fetchers with fixtures, API-key/unsupported → no_quota, per-account `EnsureFresh` lock, `GetLatestQuotaSnapshot` + prune. **Fixtures only; live probes after Human go-ahead.** No poller yet: nothing calls the fetchers outside tests | `internal/quota/{types,report,anthropic,openai,fetcher}*.go` (+ `testdata/`), `internal/store/quota_snapshots.go` (+ test), `internal/accounts/refresh.go` (lock only) | ADR accepted (done) | M (4): scope 1, PRs 1, risk 2, decisions 0, verify 0 |
| **PR-A2** poller, call budget, config, wiring | AC12–AC13, AC15, AC44–AC52: `Poller` with `Peek`/`Touch`/`Refresh`, `quota.Reader`, TTL/floor/budget/single-flight, persisted backoff and rejected state (`quota_poll_state`), `accountUsage` config, serve.go wiring replacing `noopQuotaFetcher` (display data can now flow; routing still ignores it) | `internal/quota/{poller,budget,reader}*.go` (+ tests), `internal/store/{quota_poll_state.go,schema.sql,accounts.go (delete cascade)}` (+ tests), `internal/config/config.go`, `cmd/smind/serve.go` | PR-A1 merged | M (5): scope 1, PRs 1, risk 2, decisions 0, verify 1 |
| **PR-B** routing integration | AC17–AC23: `WithModel`, windows → exhausted rules (reads `quota.Reader` only, never fetches), error text with reset, `isExhausted` rewrite, proxy passes model | `internal/routing/**`, `internal/server/proxy.go` (the `Route` call and its error text only), one line in `serve.go` | PR-A2 merged | M (4): scope 1, PRs 1, risk 2, decisions 0, verify 0 |
| **PR-C** wsapi, CLI, MCP | AC24–AC27, AC53: `account.usage` RPC (`cached`/`touch`/`refresh` modes, `displayName` on the wire, AC58) + per-fetch event + topic, run-scoped refresh → touch, `smind account usage`, MCP `account_usage`, instructions, catalog tests, the `enabled` state the UI reads | `internal/wsapi/{account_usage.go,events.go,handlers.go}` (+ tests), `cmd/smind/{account.go,mcp_*.go}` (+ tests), one line in `serve.go` | PR-A2 merged | M (3): scope 1, PRs 1, risk 1, decisions 0, verify 0 |
| **PR-D1** web core: Settings → Usage + modal | AC32–AC40, AC54, AC58 (web half), AC67, AC68 (no footer, no chat card yet): types/model/format/copy/tone, preferences (`displayAs`, account pick, "Show summary in sidebar" stored, default off), report store + event wiring, display-name use, cards, plain window bars, balance bars, meter, refresh buttons, problem copy, modal (dialog/sheet) + account filter + Settings popover, segmented control, provider logo marks, the **registered** Plan usage block in Settings → Usage (works in both the in-shell and the full-screen Settings containers), command-palette entry; screenshots for the modal and Settings (and, once `feat/settings-fullscreen` is on develop, inside it) | `web/packages/ui/src/{lib/account-usage/**, hooks/use-account-usage.ts, hooks/use-usage-preferences.ts, components/account-usage/{usage-card,usage-list,usage-body,usage-controls,usage-options,usage-modal,usage-window-bar,usage-balance-bar,usage-meter,usage-refresh-button,usage-source-icon}.tsx, components/ui/segmented-control.tsx, components/settings/usage-section.tsx, components/command-palette.tsx (one entry)}` (+ tests), `lib/storage.ts` (one key) | PR-C merged. Independent of `feat/settings-fullscreen` (registry contract only); rebase onto it if it lands first | M (4): scope 1, PRs 1, risk 0, decisions 0, verify 1 |
| **PR-E** (**required**) chat plan usage on hover | AC42, AC55–AC57 (+ AC41 chat screenshots): hover card on `ChatUsageTotal` and `RunUsagePill` (HoverCard on hover-capable pointers, existing Popover on touch), shared `ChatPlanUsage` section, provider → account-provider map, all-OAuth-accounts-of-provider compact list with filter-selected first, cache-only reads, "No plan data yet / Open Usage", trigger visible with plan data only | `web/packages/ui/src/components/{chat-usage-total.tsx, run-usage-pill.tsx, task-detail.tsx (pass the chat provider only), account-usage/{chat-plan-usage,plan-usage-compact}.tsx}`, `lib/account-usage/chat-provider.ts` (+ tests) | PR-D1 merged | M (3): scope 1, PRs 1, risk 0, decisions 0, verify 1 |
| **PR-D2** web sidebar footer item (default off) and pins | AC28–AC31, AC41 (footer part), AC54 (footer cadence): footer Usage row with logos/defaults/pinned percents, layout chooser (`ResizeObserver` measuring), pinnable window rows, pin glyph, the switch wiring (row exists only while on), hidden/plain states, collapsed rail, footer screenshots | `web/packages/ui/src/components/{app-sidebar.tsx (footer slot + provider only), account-usage/{usage-sidebar-item,usage-sidebar-root}.tsx, account-usage/usage-window-bar.tsx (pinnable mode)}`, `lib/account-usage/pinned.ts` (+ tests) | PR-E merged (order D1 → E → D2) | M (3): scope 1, PRs 1, risk 0, decisions 0, verify 1 |
| **PR-F** (conditional on **D14**) passive rate-limit signal | AC59–AC66: `quota.Observer`, Anthropic and Codex header parsers (fixtures built from the refs' header sets), effective-report merge in `Peek`, `quota_snapshots.kind` + migration, throttled async write, proxy hook after the upstream response, `accountUsage.passive`, wire `source`/`status`, routing uses passive exhaustion | `internal/quota/{observer,passive*,merge}*.go` (+ `testdata/`), `internal/store/{quota_snapshots.go,migrate.go}` (+ tests), `internal/server/proxy.go` (one observer call after the upstream response), `internal/config/config.go` (one key), wsapi wire fields | PR-B and PR-C merged; **Human accepts D14** | M (4): scope 1, PRs 1, risk 1 (parsing untrusted headers, hot-path write), decisions 1, verify 0 |
| **Close-out** docs | AC43: ROADMAP/README/stale backlog text, plan → `completed/` with Validation | `docs/**`, `README.md` | all | S |

Note: "PR-A" elsewhere in this plan and the ADR means PR-A1 and PR-A2
together (split because the call budget and its persisted state made one PR
too large to review); the budget tests (`budget_*`) and AC44–AC52 land in PR-A2.

### PR-D: Paseo → smind file map

Paseo is React Native + Unistyles + react-query + a plugin registry with a
host (machine) concept; smind's web is React 19 + Tailwind v4 + shadcn/Radix
(`web/packages/ui`), no react-query, one daemon, and an accounts table rather
than harness-login discovery. **Ported** = same behaviour and structure, **adapted** =
same idea, rewritten for smind's stack/data, **dropped** = no smind
equivalent. All smind paths are under `web/packages/ui/src/`. Read Paseo's file
before writing its counterpart (memory: prefer porting over writing UI from
scratch).

| Paseo (`packages/app/src/usage/`) | smind | How |
|---|---|---|
| `types.ts` | `lib/account-usage/types.ts` | Adapted: hand-written TS types for the `AccountUsage` wire shape (no zod protocol package); `UsageView` kept; guards tolerate missing fields (AC39) |
| `model.ts` | `lib/account-usage/model.ts` | Ported: `usedPercent`, `displayPercent`, `usageWindowRowLabel`, `formatUsageFreshness`, `resolveUsageRefresh`, `replaceReport`, `upsertReport` (laterFetched wins), `settleReports`, `resolveUsageView`. **Adapted:** `resolveUsageHostId` / `resolveUsageModalHostId` → `resolveAccountFilter` (picked account, else All); "host not connected / needs update" → "daemon not connected / lacks `account.usage`". **Deferred to PR-E:** `resolveAgentUsageView` |
| `preferences.ts` | `lib/account-usage/preferences.ts` | Ported rules (null pins = defaults, array incl. empty = selection, first toggle snapshots defaults, pins match a provider's window on every account, `effectiveUsagePins`, `toggleUsagePin`); **adapted:** `serverId` → `accountKey: string \| null`, `sourceId` → `provider`, zod → `readStored`/`writeStored` validators with a new `STORAGE_KEYS.usage` (`smind:usage-preferences`); the COMPAT legacy-`pinned` shim is dropped (no legacy) |
| `pinned.ts` | `lib/account-usage/pinned.ts` | Ported: `choosePinnedUsageLayout` (constants `MIN_METER_WIDTH` 16, `METER_GAP` 4), `resolvePinnedUsage` |
| `tone.ts` | `lib/account-usage/tone.ts` | Ported (daemon also sends `tone`; this is the fallback) |
| `format.ts` | `lib/account-usage/format.ts` | Ported: `formatPct`, `formatDisplayPct`, `formatResetLabel`, `formatAmount` (`Intl`); token amounts reuse `lib/usage-format.ts` `formatTokens` instead of Paseo's `formatTokenCount` |
| `copy.ts` | `lib/account-usage/copy.ts` | Ported single-file copy; **adapted:** remedies say "Reconnect it in Providers." (smind owns the login; Paseo's "Run <harness> to refresh it" does not apply); adds the disabled and old-daemon sentences; relative time via `lib/relative-time.ts` |
| `queries.ts` | `hooks/use-account-usage.ts` | **Adapted:** react-query is replaced by a module store + `useSyncExternalStore`: `account.usage` list (stale after 60 s, daemon cache is 5 min), `refreshOne(accountId)`, `refreshAll()`, `account.usage` / `account.removed` events through `useDaemonEvents`, reconnect re-list, per-account upsert with the same race rules as Paseo's `upsertReport`/`settleReports`. Streaming list becomes events (the RPC itself is not streamed) |
| `display.ts` | `hooks/use-usage-preferences.ts` | Ported (`useUsageDisplay`) over a small `localStorage` store shared by every surface (same pattern as `use-notification-sound-preference.ts`) |
| `hosts.ts`, `in-sidebar.ts` | `hooks/use-usage-preferences.ts` (account pick, `inSidebar`) | **Adapted:** host pick → account pick; Paseo's `useSidebarNavItems("footer")` registry does not exist in smind, so "Summary in sidebar" is a stored boolean (default on) |
| `sidebar-item.tsx` | `components/account-usage/usage-sidebar-item.tsx`, `usage-sidebar-root.tsx`; footer row in `components/app-sidebar.tsx` (**PR-D2, off by default**) | Ported structure (`UsageSidebarRoot` owns open state, mounts the modal on first open and unmounts after close; `PinnedUsageTrigger`/`PinnedUsageLine` with hidden measuring copies); **adapted:** RN `onLayout` → `ResizeObserver`, `SidebarMenuButton` row in `SidebarFooter` (icon-only in the collapsed rail) |
| `usage-modal.tsx` | `components/account-usage/usage-modal.tsx` | **Adapted:** `AdaptiveModalSheet` → `components/ui/dialog.tsx` on wide, `components/ui/sheet.tsx` (bottom) when `useIsMobile()`; max width 640; sheet ≤ 80% height; body scrolls; keyed by account filter so a refresh in flight on one pick never shows on another |
| `controls.tsx` | `components/account-usage/usage-controls.tsx` | Ported layout (filter, Refresh all, Settings); **adapted:** `HostFilter` → a `components/ui/select.tsx` account filter ("All accounts" + each account label), hidden with ≤ 1 account |
| `options.tsx` | `components/account-usage/usage-options.tsx`, `components/ui/segmented-control.tsx` | **Adapted:** `DropdownMenu` + `SettingsSwitch` + `SegmentedControl` → `components/ui/popover.tsx` (sheet on narrow) with a Switch "Show summary in sidebar" (default **off**) and a new small Radix `ToggleGroup`-based segmented control for Used / Remaining. In Settings → Usage the same control is rendered as a visible labelled **Settings** button in the block header (Paseo #5921: discoverable above reports) |
| `usage-section.tsx`, `host-usage-section.tsx` | `components/settings/usage-section.tsx` (existing file, registered via `registerSettingsSection`) | **Adapted:** one block, no host: Plan usage (`UsageBody`, `UsageMessage`, error `Alert` with Try again) above the existing token table, retitled Token usage. Registered section id `usage` stays. **Settings redesign:** it renders into whatever container the Settings screen gives it (in-shell today, `feat/settings-fullscreen`'s framed content view or narrow detail page later), keeps its controls in its own body (Paseo's `SettingsSection trailing` becomes a header row inside the block), uses no app-sidebar assumptions, and leaves the registry API and `settings-*` test ids untouched |
| `list.tsx` | `components/account-usage/usage-list.tsx` | Ported (one card per account; rows pinnable only while the summary switch is on) |
| `card.tsx` | `components/account-usage/usage-card.tsx` | Ported: header (logo, **display name** (`accounts.label`, provider default only if empty/default), plan `status-badge`, status dot/label, per-card refresh with freshness tooltip), messages, bars, balances, details, footer (inline freshness on narrow/touch; Paseo's account-label footer is gone because the label is now the title), "Unable to refresh usage". **Adapted:** `loginErrors` (several harness logins per account) is dropped: one credential per smind account |
| `window-bar.tsx` | `components/account-usage/usage-window-bar.tsx` | Ported: checkbox row when pinnable, pinned/hover highlight layers, pin glyph + tooltip, `usageWindowRowLabel` accessible name. **Dropped:** the "runs out" at-risk line (no `runsOutAt` in v1) |
| `balance-bar.tsx` | `components/account-usage/usage-balance-bar.tsx` | Ported (`Intl` language from the browser) |
| `meter.tsx` | `components/account-usage/usage-meter.tsx` | Ported: 4 px track, tone fills (`ok`/`warning`/`danger`/default) using smind's existing status colour tokens, `quiet` variant for the footer |
| `refresh-button.tsx` | `components/account-usage/usage-refresh-button.tsx` | Ported ("Refresh all", spinner via `components/ui/inline-spinner.tsx`) |
| `source-icon.tsx` | `components/account-usage/usage-source-icon.tsx` + inline Claude and OpenAI marks | **Adapted:** Paseo ships catalog SVGs per source; smind has no provider icons yet, so PR-D1 adds two inline `currentColor` marks keyed by `provider` and a lucide `Gauge` fallback |
| `agent-usage.tsx` + `useAgentUsage` + `resolveAgentUsageView`, and Paseo's context-window hover card (#5975, `fe10c40b`) | `components/account-usage/chat-plan-usage.tsx`, `plan-usage-compact.tsx`, `components/chat-usage-total.tsx`, `components/run-usage-pill.tsx` | **Required (PR-E), adapted:** Paseo resolves the agent's own account from its launch environment; smind cannot (runs use the user's own login, ADR-0020), so the section lists **all OAuth accounts of the chat's provider**, compactly, filter-selected first, from the cache only (`mode:"cached"`, no streaming per-agent query). Hover card where hover exists, tap popover on touch. Unpinnable, no refresh button (as Paseo's `refreshable=false`) |
| `*.test.ts` (`pinned`, `model`, `preferences`, `format`, `copy`) | `lib/account-usage/*.test.ts` | Ported (names above) |
| e2e `usage-modal/-settings/-sidebar-item.spec.ts` | `components/account-usage/*.test.tsx`, `settings/usage-section.test.tsx` | Adapted to Testing Library with a fake `WsClient` (names above) |
| plugin registry, `useHosts`, host runtime, `HostFilter`, `AdaptiveModalSheet`, Unistyles themes | — | Dropped / replaced by smind's own sidebar, dialog/sheet and Tailwind tokens |

## Decisions

- Design and rationale: [ADR-0028](../../decisions/0028-account-usage-windows.md)
  (**Accepted 2026-10-11**). Answers recorded there: D1–D4 and D6–D13 as the
  recommended defaults (D1 100%, D2 on and demand-driven, D3 5 min TTL and
  1→30 min backoff, D4 Anthropic + OpenAI OAuth, D6 routing on, D7 hard keeps
  its error, D8 new `account_usage` MCP tool, D9 overage suppresses, D10
  poller refreshes only expired tokens, D11 `localStorage`, D12 503 with reset
  time, D13 global switch only). **D5 changed:** the web UI ports Paseo's
  Usage UX. Round 2 (2026-10-12): footer item **off by default**, plan usage
  on **chat hover** (PR-E required) and in Settings → Usage, order
  D1 → E → D2, display name = `accounts.label`, Settings → Usage plugs into
  the registry. **D14 (passive rate-limit signal) is Proposed**, awaiting the
  Human. Not reopened inside a PR.
- Decided here, not in the ADR (small, reversible):
  1. Naming: "Plan usage" (windows, `account.usage`) vs "Token usage"
     (`usage.summary`, `run_usage`) in every surface. Provider display names:
     `anthropic` → "Claude", `openai` → "Codex" (as in Paseo's fixtures).
  2. Fixtures are redacted copies of real responses; the live probe is the
     manual step below, gated on Human go-ahead.
  3. The wsapi/web switch for `accountUsage.enabled` reuses whatever
     mechanism the daemon has for reading and writing `config.yaml` at the
     time of PR-C. If none exists, PR-C ships read-only state in
     `account.usage` (`enabled`) and the config file is the switch; a
     write RPC would be a new decision.
  4. Per-run and proxy token usage are untouched; `usage.summary` gains no
     window concepts.
  5. "Remembers the picked account" is an account filter ("All accounts" or
     one), the smind analogue of Paseo's remembered host; **accepted by the
     Human (2026-10-12)** as shared by footer, modal and Settings → Usage.
  6. ~~The footer Usage row is present whenever polling is enabled~~
     **Superseded (2026-10-12):** the footer row is off by default and exists
     only while "Show summary in sidebar" is on (Settings → Usage); it also
     controls whether window rows are pinnable.
  7. The chat hover card lists **all OAuth accounts of the chat's provider**,
     compactly, filter-selected first, from the cache only. Reason: smind does
     not know which login a run used (ADR-0020), a silent single pick can show
     the wrong account, and the filter is a modal preference that should not
     hide data elsewhere.
  8. Display name = `accounts.label`; provider default only if empty or a
     provider default (`quota.DisplayName`, one implementation for all
     surfaces).
  9. Settings → Usage is registry-driven and container-agnostic so it works in
     the in-shell Settings today and `feat/settings-fullscreen`'s full-screen
     screen later (that plan's decision 8 says the Plan usage block must not
     be blocked).
- Superseded: the `orchestration-and-metering.md` Step 4 backlog item
  "feeding the router's `TokensUsed` from locally counted tokens" (ADR-0028
  uses provider-reported windows instead, for the reasons in its
  Alternatives).

## Progress

- 2026-10-11: ADR-0028 (Proposed) and this plan written, docs only, on
  `docs/account-usage-windows`. Source reading done: `internal/quota`,
  `internal/routing/router.go`, `internal/accounts/{refresh,credential}.go`,
  `internal/server/proxy.go`, `cmd/smind/serve.go`, wsapi account/usage
  handlers, Paseo's usage plugin, server registry, app and CLI.
- 2026-10-11: Human approved ADR-0028; D1–D4, D6–D13 as recommended, D5
  changed to "port Paseo's Usage UX". ADR set to Accepted; plan updated: web
  ACs rewritten (AC28–AC42), PR-D split into D1/D2 (+ optional E) with the
  Paseo → smind file map, test scenarios mirror Paseo's e2e specs. Paseo
  read: `packages/app/src/usage/*`, `e2e/browser/usage-*.spec.ts`, history
  of #5685/#5786/#5805/#5876/#5903/#5921/#6216/#5975.
- 2026-10-12: Human approved the three open items (account filter shared by
  footer and modal; footer row always present while polling is enabled;
  Claude/Codex labels) and both extras (event after every completed fetch;
  `Problem.ExpiresAt`), and added a firm rule: usage endpoints are
  rate-limited, never call them carelessly. ADR-0028 §3 got "Call budget (hard
  rules)"; AC44–AC54 and the `budget_*` tests added; routing/proxy now read
  the cache only; backoff persisted in `quota_poll_state`.
- 2026-10-12 (round 2): Human accepted the account filter; set the footer
  Usage item **default off** (enabled in Settings → Usage); required plan usage
  on **hover of the chat usage icon** (PR-E, now required) and in Settings →
  Usage, order D1 → E → D2; display name = `accounts.label`; asked that
  Settings → Usage plug into the registry of the full-screen Settings screen
  (parallel `feat/settings-fullscreen`); proposed **D14**, a zero-extra-call
  passive rate-limit signal. ADR §5 rewritten, §7 added (evidence from
  refs/cliproxyapi, refs/codex, refs/claude-agent-sdk-python); plan ACs
  AC28/AC30/AC32–AC34/AC37/AC54 amended, AC42 required, AC55–AC58, AC67,
  AC68 added, conditional PR-F (AC59–AC66) added.
- [x] D1–D13 answered by the user
- [ ] **D14 (passive rate-limit signal) decided by the Human**
- [ ] PR-A1 types, fetchers, snapshot store (fixtures only)
- [ ] PR-A2 poller with the call budget, persisted state, config, wiring
- [ ] Human go-ahead for the manual live probe (see Validation)
- [ ] PR-B routing integration
- [ ] PR-C wsapi, CLI, MCP
- [ ] PR-D1 web core (store, cards, modal, Settings → Usage, palette entry)
- [ ] PR-E chat hover card (required)
- [ ] PR-D2 web footer item (default off) and pins
- [ ] PR-F (conditional on D14) passive rate-limit signal
- [ ] Manual live check against one real account per provider
- [ ] Close-out docs; plan moved to `completed/`

## Validation

Docs-only PRs so far (#296), nothing built. Checked: the ADR number 0028 was
free on `origin/develop` (last 0027) and in open PRs; the diff touches only
`docs/**`; no code, config, or generated file changed.

**Live endpoint probes are a PR-A manual step and need the Human's go-ahead
before they run; until then PR-A is fixtures-only.** They call real provider
endpoints with a real token (outward-facing), so they are not run by an agent
on its own. The probes settle four unknowns, each recorded here with the
date and a yes/no (never a token, email or body):
1. Anthropic: does `GET /api/oauth/usage` accept the token smind stores for a
   login done through smind's own flow (scope `user:profile`)? Which
   top-level keys come back (names only)?
2. OpenAI: does a token stored by smind's login carry the
   `chatgpt_account_id` claim the `ChatGPT-Account-Id` header needs?
3. OpenAI: does `chatgpt.com` accept the uTLS Chrome fingerprint on
   `/backend-api/wham/usage` (Paseo uses a plain `fetch` with a browser
   `User-Agent`)?
4. Anthropic: what `extra_usage.is_enabled` means for serving past a full
   window (D9).
Procedure when approved: a local, untracked helper behind a build tag that
reads the account from the user's own smind store, makes one request per
provider, and prints only the HTTP status, top-level JSON key names, window
ids, and the booleans above. No headers, no values, no argv secrets. Redacted
responses then refresh the fixtures.

Mapping for the implementation PRs (fill in as each lands):

| AC | How it will be confirmed |
|---|---|
| AC1–AC4 | `internal/quota` report/versioning/accountkey/prune tests (`report_roundtrip_and_versioning`, `accountkey_stable`, `poller_prunes_old_snapshots`) |
| AC5–AC11 | Anthropic/OpenAI/dispatch/refresh table tests with fixtures, `ensurefresh_*` under `-race` |
| AC12–AC16 | `poller_*` tests with a fake clock, config tests, `no_secret_in_logs_report_event` |
| AC44–AC52 | `budget_*` tests (`internal/quota/budget_test.go`) with exact `httptest` request counts, restart-persistence tests, `-race` |
| AC53 | wsapi/CLI/MCP mode tests, `proxy_1000_requests_zero_usage_calls` |
| AC54 | web call-discipline tests with fake timers and visibility/focus |
| AC17–AC23 | `route_*` tests, proxy tests |
| AC24–AC27 | wsapi, CLI golden, MCP catalog tests |
| AC28–AC31 | PR-D2 vitest tests (`pinned_windows_show_in_footer…`, `summary_in_sidebar_switch…`, logic tests) + footer screenshots |
| AC32–AC40 | PR-D1 vitest tests mirroring Paseo's modal and settings specs + `pinned`/`model`/`preferences` logic tests |
| AC41 | light/dark × 1440×900 / 390×844 screenshot set attached to PR-D1 and PR-D2, viewed by the implementer and the supervisor |
| AC42, AC55–AC57 | PR-E hover-card tests (cache-only request assertions) + hover/touch/narrow screenshots |
| AC58 | `displayname_*` Go tests, wire/CLI/MCP tests, web label tests |
| AC59–AC66 | PR-F `passive_*` tests (conditional on D14) |
| AC67–AC68 | PR-D1 section-in-both-containers tests, palette test |
| AC43 | `task test`, `task lint`, ADR/plan/ROADMAP diffs |
| Real endpoints | the manual live probe above, after Human go-ahead (not automated; fixtures can drift from real responses) |

Unverified at spec time (the implementing PRs must confirm and record):

- That the Anthropic `/api/oauth/usage` endpoint accepts smind's stored
  OAuth token. smind's login requests the `user:profile` scope
  (`internal/accounts/refresh_providers.go`), which Paseo's reading of the
  endpoint suggests is needed, but the endpoint was not called.
- That an OpenAI token stored by smind's login carries the
  `chatgpt_account_id` claim the `ChatGPT-Account-Id` header needs, and that
  `chatgpt.com` accepts Go's uTLS Chrome fingerprint for this path (Paseo
  uses a plain `fetch` with a browser `User-Agent`).
- Whether Anthropic `extra_usage.is_enabled == true` really means requests
  continue past a full window (D9).
- Whether a plan label is available for Anthropic accounts without an extra
  call (`/api/oauth/profile`); v1 leaves it empty if not.
- How run-scoped callers are identified at the wsapi layer for the
  `refresh` denial (ADR-0027 describes the same need; PR-C must reuse the
  existing mechanism rather than invent one).
