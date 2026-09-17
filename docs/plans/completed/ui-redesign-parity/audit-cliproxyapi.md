# Reference audit: CLIProxyAPI management UI (`refs/cliproxyapi`)

## Scope caveat — read this first

**The management UI's source is not in `refs/`.** `refs/cliproxyapi` ships
the *server*; the control panel is a single `management.html` asset
downloaded at runtime from a separate repository
(`router-for-me/Cli-Proxy-API-Management-Center`, "CPAMC"), with a CDN
fallback at `https://cpamc.router-for.me/`. See
`refs/cliproxyapi/internal/managementasset/updater.go:29-36`:

```go
defaultManagementReleaseURL  = ".../Cli-Proxy-API-Management-Center/releases/latest"
defaultManagementFallbackURL = "https://cpamc.router-for.me/"
managementAssetName          = "management.html"
updateCheckInterval          = 3 * time.Hour
```

`refs/cliproxyapi/docs/` contains only SDK docs; the management API
reference is an external site (`README.md:143` →
`https://help.router-for.me/management/api`).

So this audit is derived from **the API surface the panel is built
against** (`refs/cliproxyapi/internal/api/server_management.go` and
`refs/cliproxyapi/internal/api/handlers/management/`), which is the honest
available source of truth for what the UX must express. Nothing here
describes pixels, and nothing should be cited as "CPAMC does X visually".

That is not a large loss: cliproxyapi is not an agent IDE. Its relevance to
smind is **one dimension only — the accounts/providers/routing control
surface** — and that dimension is legible from the API.

---

## 1. Information architecture (as implied by the API)

Everything lives under `/v0/management/*`, gated by
`managementAvailabilityMiddleware` (404 when disabled or when the "home"
mode is on). The route groups map cleanly to panel sections:

| Section | Routes |
| --- | --- |
| **Credentials / accounts** | `GET/POST/DELETE /auth-files`, `/auth-files/models`, `/auth-files/download`, `PATCH /auth-files/status`, `PATCH /auth-files/fields`, `POST /vertex/import` |
| **OAuth login flows** | `/anthropic-auth-url`, `/codex-auth-url`, `/antigravity-auth-url`, `/kimi-auth-url`, `/xai-auth-url`, `/get-auth-status`, `DELETE /oauth-session` |
| **API keys (per provider)** | `/api-keys`, `/gemini-api-key`, `/claude-api-key`, `/codex-api-key`, `/xai-api-key`, `/vertex-api-key`, `/interactions-api-key`, `/openai-compatibility` |
| **Routing** | `/routing/strategy`, `/force-model-prefix`, `/oauth-model-alias`, `/oauth-excluded-models` |
| **Reliability** | `/request-retry`, `/max-retry-credentials`, `/max-retry-interval`, `/oauth-request-scoped-errors`, `/quota-exceeded/switch-project`, `/quota-exceeded/switch-preview-model`, `POST /reset-quota` |
| **Observability** | `/logs`, `DELETE /logs`, `/request-error-logs`, `/request-error-logs/:name` (download), `/request-log-by-id/:id`, `/request-log`, `/api-key-usage`, `/usage-queue`, `/usage-statistics-enabled` |
| **Config** | `/config`, `GET/PUT /config.yaml` (raw YAML editing), `/debug`, `/logging-to-file`, `/logs-max-total-size-mb`, `/error-logs-max-files`, `/proxy-url`, `/ws-auth` |
| **Plugins** | `/plugins`, `/plugin-store`, `POST /plugin-store/:id/install`, `DELETE /plugins/:id`, `PATCH /plugins/:id/enabled`, `GET/PUT/PATCH /plugins/:id/config` |
| **Diagnostics** | `POST /api-call` (send a request through the proxy from the panel), `/latest-version` |

---

## 2. The credential/account row — the part smind should copy

`buildAuthFileEntry` (`refs/cliproxyapi/internal/api/handlers/management/auth_files.go:325-430`)
is the richest account model in any of the three references. Every field
below is one thing the panel can show per credential:

| Field | Meaning |
| --- | --- |
| `id`, `auth_index`, `name` | identity; `auth_index` disambiguates multiple auths from one file |
| `provider` / `type` | provider vocabulary |
| `label` | user-facing name |
| `status`, `status_message` | health + the reason string |
| `disabled` | user-toggled off (`PATCH /auth-files/status`) |
| `unavailable` | runtime-observed unusable |
| `runtime_only`, `source` (`file` \| `memory`) | is it on disk or only in memory |
| `success`, `failed` | lifetime request counters |
| `recent_requests` | a **sparkline-shaped bucket series** (`RecentRequestsSnapshot`) |
| `quota` | per-credential quota observation |
| `model_quotas` | per-model quota observation |
| `email`, `account`, `account_type`, `project_id` | who this credential is |
| `created_at`, `updated_at`/`modtime`, `last_refresh`, `next_retry_after` | lifecycle timestamps, including **when a cooling-off credential becomes usable again** |
| `priority`, `weight` | routing inputs, editable per credential (`PATCH /auth-files/fields`) |
| `note` | free-text operator note |
| `id_token` | decoded Codex ID-token claims |
| `size`, `path` | file facts |

Two behaviours worth noting:
- A credential deleted from disk but lingering in memory is **hidden**
  rather than shown as a ghost row (`auth_files.go:386-392`).
- Plugin-virtual auths are read-only and say so
  (`errPluginVirtualAuth`: "plugin virtual auth cannot be modified
  directly; edit or delete the source auth file").

`GET /auth-files/models` + `GET /model-definitions/:channel` let the panel
show *which models a given credential can actually serve*.

---

## 3. OAuth UX

`GET /<provider>-auth-url` starts a flow and returns a URL;
`GET /get-auth-status` polls it; `DELETE /oauth-session` cancels a
half-finished one. There is an explicit callback-forwarder mechanism
(`callbackForwarders` in `auth_files.go:28-31`,
`auth_files_oauth_callback.go`) so a browser-side login can complete
against a headless daemon. Codex has dedicated concurrency handling
(`oauth_codex_concurrency_test.go`).

**The cancellable, pollable OAuth session is the transferable idea**:
"start → show URL → poll status → cancel" is a complete state machine, not
a fire-and-forget link.

---

## 4. Observability surface

- `GET /logs` (+ `DELETE`) for live log tailing.
- `GET /request-error-logs` → a list, `/:name` → download a single one.
- `GET /request-log-by-id/:id` → one request's full trace, which implies
  request ids are surfaced next to errors so an operator can jump from a
  failure to its trace.
- `GET /api-key-usage` → `{ success, failed, recent_requests[] }` per key.
- `GET /usage-queue` → pending usage records.
- `POST /api-call` → **send a test request through the proxy from the
  panel**, i.e. a built-in diagnostic console.

Since v6.10.0 the project deliberately *removed* built-in usage statistics
from both server and panel (`README.md:147`), delegating to external
dashboards. That is a scoping signal, not a gap: deep analytics is
explicitly out of the control panel's job.

---

## 5. Config editing

`GET /config` (structured) and `GET/PUT /config.yaml` (raw) coexist, plus
~25 single-field `GET/PUT/PATCH` endpoints. The pattern is: **structured
rows for the common knobs, raw YAML as the escape hatch**, with the same
document behind both. Every knob is individually addressable so a row can
save just itself.

---

## 6. What smind should take, and what it shouldn't

**Take:**
1. **The per-credential row model.** smind's `accounts-dialog.tsx` today
   shows presence/absence plus a health dot from `provider.test`. The
   fields worth adding, in rough order of dogfood value: `status` +
   `status_message`, `last_refresh` / token expiry, `next_retry_after`
   (when a rate-limited account recovers), `success`/`failed` counters,
   `disabled` toggle, `label`, and `note`.
2. **A cancellable, pollable OAuth session state machine** rather than a
   one-shot "Connect" button.
3. **"Which models can this credential serve"** as a per-account
   affordance.
4. **A request-error log list with per-request trace lookup**, reachable
   from the failure itself.
5. **Structured rows + raw escape hatch** for config, if smind ever exposes
   `~/.spacingmind/config.yaml` in the UI.
6. **A built-in "send a test request" diagnostic** — smind has
   `provider.test`; the fuller version is a console.

**Don't take:**
- The single-file-HTML, runtime-downloaded, auto-updating asset delivery
  model. smind embeds its UI in the binary (`internal/server/dist`), which
  is the right call for a single-binary product (ADR 0001) and is not up
  for renegotiation here.
- Deep usage analytics — upstream removed it on purpose.
- The plugin store.
- Routing-strategy UI beyond what smind's own routing engine needs;
  cliproxyapi is a proxy first and its config surface is far wider than
  smind's.
