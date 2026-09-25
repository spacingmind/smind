# 0014: Agent profiles

## Status

Accepted (2026-09-25)

## Context

smind has the individual knobs a "launch configuration" would bundle --
`internal/taskrunner.Provider` (`claude-native`/`glm`/`kimi`/`codex-native`),
`ApprovalPolicy` (`manual`/`auto-safe`/`full-access`), and `ThinkingLevel`
(Claude-only, `off`/`standard`/`extended`) -- but nothing that names and
persists a bundle of them. The composer (`composer.tsx`) holds all three as
per-submission React state, reset to the same hardcoded defaults
(`claude-native`/`manual`/unset) every time; there is no save/select-a-preset
UI, and no daemon-side concept of a "profile" at all
(`docs/research/local/paseo-skills-profiles-2026-09.md`'s gap analysis, §d).

Paseo's equivalent (`refs/paseo/packages/protocol/src/agent-profile.ts:12-30`)
is a named, per-daemon (`PASEO_HOME`-global, not per-workspace) record --
`id`, `name`, `provider`, `model?`, `modeId?`, `thinkingOptionId?`,
`featureValues?`, `notes?` -- deliberately carrying no `systemPrompt`, applied
by copying its fields into the client's per-launch settings, never referenced
by id at launch time.

**Decision already made by the user (2026-09-25):** smind gets agent
profiles, stored **in the daemon**, so web, desktop, mobile, and CLI clients
all share the same list, and so a future orchestrating agent can read them
over RPC (Paseo's `list_profiles` MCP tool has no smind equivalent yet, but
daemon-side storage is the prerequisite for one). This ADR records the data
model, storage, and wire shape for that addition; everything else about
smind's backend architecture is unchanged.

## Decision

### Data model

A new persisted record, `store.AgentProfile`:

| Field | Type | Required | Notes |
| --- | --- | --- | --- |
| `id` | `int64` | yes | store-assigned (`AUTOINCREMENT`) |
| `name` | `string` | yes | non-empty; a label, not a key -- uniqueness is not enforced, matching Paseo (profiles are referenced by `id`, never by name) |
| `provider` | `string` | yes | must be one of `taskrunner.SupportedProviders()`'s ids at write time (`create`/`update`); stored as a plain string, not `taskrunner.Provider`, for the same reason `store.Run.Provider`/`ApprovalPolicy` are plain strings -- `internal/store` does not import `internal/taskrunner` (see `internal/store/types.go:81-84`'s doc comment) |
| `approvalPolicy` | `string` | no, defaults to `""` (meaning "unset"; the composer's own default of `manual` applies when a profile leaves this unset) | when non-empty, must satisfy `taskrunner.ApprovalPolicy.IsValid()` |
| `thinkingLevel` | `string` | no, defaults to `""` (`taskrunner.ThinkingLevelUnspecified`) | when non-empty, must satisfy `taskrunner.ThinkingLevel.IsValid()`; meaningful only when `provider` is `claude-native`, same as today's per-run field |
| `notes` | `string` | no, defaults to `""` | free text, human-facing today; shaped to double as the LLM-legible field Paseo's `list_profiles` exposes to an orchestrating agent, if/when smind grows that RPC |
| `createdAt` / `updatedAt` | `time.Time` | yes | store-stamped, matching every other `store` record |

**Deferred: `model`.** The original draft of this ADR included an optional
`model` field. Dropped from v1 entirely (schema, RPC shapes, CLI, UI) after
review on 2026-09-25: `task.prompt` (`internal/wsapi/handlers.go:705-735`)
has no `model` parameter, and no runner backend (`claude-agent-sdk-go`,
ACP, Codex) is wired for per-run model selection today, so a stored `model`
value would be a field with nothing to apply it to or render it against --
an inert column, which this ADR's own "no inert fields" stance (see the
excluded fields below) argues against keeping. Add `model` back to this
schema in the same additive way (a new nullable column, a new optional RPC
field, a new CLI flag) once `task.prompt` -- or its successor -- gains a
real `model` parameter end to end; until then there is nothing for a
profile's `model` to mean.

**Deliberately excluded**, each for a reason specific to smind's current
surface (not carried over just because Paseo has them):

- **`systemPrompt`.** Omitted for the exact reason Paseo's own schema omits
  it (`agent-profile.ts`'s doc comment): every backend `taskrunner` drives
  (`claude-agent-sdk-go`, ACP's `NewSession`, Codex's app-server) treats its
  initial-session configuration as creation-only. A stored `systemPrompt`
  applied to `task.prompt` against an already-running task would silently
  no-op -- indistinguishable from working. Revisit only alongside a real
  "start a new task from this profile" flow that has a creation-time hook to
  honor it.
- **`icon` / `color`.** Paseo's schema carries both for its host's
  agent-badge theming. smind's task list and composer have no per-agent
  badge/color concept today -- both fields would round-trip through the API
  with nothing ever rendering them. Add alongside whatever UI grows that
  need.
- **`featureValues`.** Paseo's provider-specific feature flags map to
  smind's ACP `ConfigOption`s (`internal/taskrunner/config_options.go`),
  which are explicitly **live-session-scoped**: the option list is only
  known after ACP's `NewSession` responds (`run-config-options.tsx`), unlike
  `thinkingLevel`, which is a genuine pre-run field. A profile applied
  before a run starts has nothing to copy `featureValues` into yet. Out of
  scope until a pre-run feature-default surface exists.

### Scope: global per daemon, not per workspace

Matches Paseo. A profile describes *how* to run an agent (which provider,
how cautious, how much it thinks), not *what codebase* to run it against --
scoping it to a workspace would mean re-creating "UI work: claude-native,
manual, standard thinking" once per workspace, defeating the entire point of
naming it once. This also matches smind's own precedent for
daemon-wide-but-workspace-restrictable resources: `accounts` is a global
table (`internal/store/schema.sql`'s `accounts`), and `workspace_accounts`
is a separate join table restricting which accounts a given workspace may
use -- accounts themselves are never workspace-owned rows. Agent profiles
need no join-table equivalent for v1 (no product need has surfaced for
restricting a profile to specific workspaces); add one additively, the same
way `workspace_accounts` was added, if that need appears.

### Storage: a new SQLite table via `internal/store`, not `config.yaml`

New `agent_profiles` table in `internal/store/schema.sql`
(`CREATE TABLE IF NOT EXISTS`, needing **no** `internal/store/migrate.go`
entry -- migrations only exist for a column *added to an existing table*, per
`migrate.go`'s own doc comment; a brand-new table is covered by
`schema.sql`'s `CREATE TABLE IF NOT EXISTS` for every database, fresh or
pre-existing, exactly like `workspaces`/`spaces`/`tasks` needed no migration
when they were introduced).

Recommended over `config.yaml`:

- `internal/store` is already the daemon's system of record for every other
  *growing list of named CRUD records* (`accounts`, `workspaces`, `spaces`,
  `tasks`) -- agent profiles are the same shape. `config.yaml`
  (`internal/config/config.go`, 62 lines, holding only `ServerConfig.Port`)
  is shaped for a handful of singleton startup knobs, not a list a user adds
  to and removes from at runtime.
- `config.yaml` is loaded once at startup (`internal/config.Load`), with no
  update-in-place or change-notification story. Wiring live create/update/
  delete through it means either restarting the daemon on every profile
  edit, or building a second concurrent-write layer duplicating what
  `*sql.DB` already gives the store for free.
- The store already pairs naturally with the wsapi CRUD + ADR-0009
  lifecycle-event pattern every comparable resource uses; `config.yaml` has
  no notifier/event story at all.
- Precedent: `accounts` is also conceptually "settings" (credentials, one
  per provider connection) and is a store table, not `config.yaml`.

### wsapi surface

Five new methods in `internal/wsapi/handlers.go`'s `methodHandlers` map,
following the `space.*`/`task.*` naming and error-wrapping convention
(`fmt.Errorf("profile.X: %w", err)`, matching every existing handler):

| Method | Params | Result | Notes |
| --- | --- | --- | --- |
| `profile.create` | `{name, provider, approvalPolicy?, thinkingLevel?, notes?}` | the created `store.AgentProfile`, marshalled verbatim (no `json:` tags, same convention as `task.get`/`space.get`) | errors: invalid params (unmarshal failure), empty `name`, unknown `provider`, invalid `approvalPolicy`/`thinkingLevel` |
| `profile.list` | none | `[]store.AgentProfile`, ordered by `id` | matches `ListTasks`/`ListSpacesByWorkspace`'s ordering convention |
| `profile.get` | `{id}` | the profile | not-found is a clear wrapped error, matching `GetSpace`/`GetWorkspace` |
| `profile.update` | `{id, name, provider, approvalPolicy?, thinkingLevel?, notes?}` | the updated profile | full-record replace, not a partial patch -- no existing smind mutation RPC does field-level PATCH semantics (`task.move`/`task.archive` are each a dedicated single-purpose operation, not a generic patch) |
| `profile.delete` | `{id}` | `{}` | no `deleteSummaryResult`-shaped body: nothing references a profile row from another table, so there is nothing to cascade or report |

Validation is shared by `create` and `update`, reusing the exact validators
`task.prompt` already calls -- `taskrunner.ApprovalPolicy.IsValid()`,
`taskrunner.ThinkingLevel.IsValid()`, and a provider-id membership check
against `taskrunner.SupportedProviders()` -- so a stored profile can never
hold a combination the wire would reject if sent directly to `task.prompt`.

This validation, plus the store calls, lives in a new `internal/profiles`
package (`Registry`, mirroring `internal/accounts.Registry`'s shape: a thin
layer wrapping `store.Store` with input validation and a notifier hook),
rather than inline in the wsapi handlers or on `store.Store` itself --
`internal/store` must not import `internal/taskrunner` (see the data-model
table above), so the `IsValid()` checks cannot live there, and a dedicated
package gives the CLI (via a later in-process call, if ever needed) and the
wsapi handlers one shared validation path instead of duplicating it.

### Lifecycle events (ADR 0009 shape)

Three new topics added to `internal/wsapi/events.go`'s `knownTopics`:

```
profile.created  {"profile": store.AgentProfile}
profile.updated  {"profile": store.AgentProfile}
profile.deleted  {"id": 7}
```

`created`/`updated` carry the full entity snapshot, exactly like
`task.created`/`task.updated` (ADR 0009's rationale applies unchanged: a
client splices the payload straight into its `profile.list` result, no
second mapping layer, no risk of drift). `deleted` carries only `id` --
unlike `space.deleted`/`task.deleted`, there is no parent-scope field to
include, since a profile has no parent entity in this schema.

Publish site: `internal/profiles.Registry` grows the same nil-safe
`Notifier`/`SetNotifier` shape as `workspace.Manager`
(`internal/workspace/workspace.go:30-68`) and `runs.Registry`, adapted onto
the shared bus by a new `busProfileNotifier` in `internal/wsapi/server.go`,
following `busWorkspaceNotifier`'s exact shape (`internal/wsapi/server.go:
115-154`). Every profile mutation has exactly one call path today (the wsapi
handler, which the CLI also reaches through `dialDaemon`) with no cascading
store calls, so the "avoid duplicate emission across multiple store calls"
half of ADR 0009's rationale doesn't independently apply here -- but the
Notifier indirection is kept anyway for shape-consistency with every other
mutating resource, and so a future non-wsapi caller (e.g. an orchestrating
agent's own profile-materializing tool, mirroring Paseo's `list_profiles`)
does not have to duplicate the emit.

### How a profile is applied

**The client copies the profile's fields into its own per-run settings**,
the same way Paseo's model picker applies a profile -- not a `profileId`
param on `task.prompt`.

- `task.prompt`'s existing fields (`provider`, `approvalPolicy`,
  `thinkingLevel`) are already exactly the three values the composer holds
  as local React state (`composer.tsx`'s `provider`/`approvalPolicy`/
  `thinkingLevel` `useState` triples). Selecting a profile is "seed these
  three `useState` values from the profile's fields" -- a pure client-side
  copy, with **zero wire change to `task.prompt`**.
- A `profileId` param would require the daemon to resolve and apply the
  profile server-side on every run, and would then need a second mechanism
  to let the user tweak a field after picking a profile (Paseo's docs are
  explicit that a profile's settings stay "editable before sending" --
  `docs/research/local/paseo-skills-profiles-2026-09.md`'s §b). The
  copy-once approach gets "editable after applying" for free: it's just
  ordinary Select state once copied.
### CLI surface

Cheap; add `smind profile add|ls|rm`, matching `internal/accounts`'
`add`/`ls` verbs (`cmd/smind/account.go`) rather than `space`/`task`'s
`create`/`ls`, since a profile is a labeled, addable "thing referenced by
id" like an account, not an editable container like a workspace:

- `smind profile add <name> <provider> [--approval-policy=] [--thinking-level=] [--notes=]` -- prints the created row (tabwriter, matching `cmdSpaceCreate`).
- `smind profile ls` -- tabwriter table: `ID NAME PROVIDER APPROVAL THINKING`.
- `smind profile rm <id>` -- calls `profile.delete`.

No `smind profile edit` in v1: web Settings covers editing, and a CLI edit
path needs its own partial-vs-full-update flag design that isn't a "cheap"
addition the way `add`/`ls`/`rm` are. Not required by the plan's acceptance
criteria.

### Compatibility

Purely additive: a new table, five new wsapi methods, three new event
topics, one new CLI subcommand group. `task.prompt`'s wire shape is
untouched -- no existing field's meaning changes. A daemon with zero
profiles behaves identically to today for every existing client: the
composer's own `provider`/`approvalPolicy`/`thinkingLevel` defaults
(`composer.tsx:201-215`) are unchanged and remain the fallback whenever no
profile is selected.

## Alternatives considered

- **Client-side storage** (e.g. `localStorage`, matching how the Settings
  screen's Appearance/General preferences persist today per
  `settings-screen.tsx`'s own doc comment: *"Preferences persist client-side
  ... unless and until a daemon-side settings API exists"*). Rejected by the
  user, 2026-09-25: invisible from a second browser tab, the desktop app,
  mobile, or the CLI, and gives a future orchestrating agent nothing to read
  over RPC.
- **`config.yaml` persistence.** Rejected: wrong shape (singleton startup
  knobs vs. a growing list of CRUD records), no live update-in-place story,
  no natural pairing with the wsapi CRUD + lifecycle-event pattern already
  established. See Storage above.
- **`profileId` param on `task.prompt`**, server-side resolve-and-apply.
  Rejected in favor of client-side field copying -- see "How a profile is
  applied" above.
- **Carrying Paseo's full schema verbatim** (`icon`, `color`,
  `featureValues`, `systemPrompt`), and keeping `model` in the v1 schema as
  an inert field. Both rejected for the same reason: each needs a
  smind-side consumer that doesn't exist yet ("Deliberately excluded" and
  "Deferred: `model`" above); adding a field without one is dead weight on
  the wire, not future-proofing.

## Rationale

smind already has every knob a profile would bundle (`provider`,
`approvalPolicy`, `thinkingLevel`) and already has a working pattern for
"named, daemon-stored, CRUD-able, event-notifying resource shared by every
client" (`accounts`, `workspaces`, `spaces`, `tasks`, each in `internal/store`
with a wsapi method set and, since ADR 0009, lifecycle events). Agent
profiles are the same shape as those, not a new architectural category --
the smallest coherent addition is a new table plus the same CRUD + event
pattern, applied by the client the same way Paseo's profile picker does,
rather than inventing a new persistence layer (`config.yaml`) or a new
apply mechanism (`profileId`) neither of which any existing smind resource
uses.
