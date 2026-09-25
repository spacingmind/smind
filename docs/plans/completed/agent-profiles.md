# Agent profiles

## Context

smind has the individual per-run knobs (`internal/taskrunner.Provider`,
`ApprovalPolicy`, `ThinkingLevel`) but no way to name and persist a bundle of
them for reuse. The composer (`composer.tsx`) resets to the same hardcoded
defaults every time; there is no daemon-side concept of a "profile" at all.
Paseo has one -- a named, per-daemon (`PASEO_HOME`-global) record applied by
copying its fields into the client's launch settings
(`docs/research/local/paseo-skills-profiles-2026-09.md`).

The user decided (2026-09-25) that smind gets agent profiles, stored **in
the daemon** so web, desktop, mobile, and CLI clients share one list and a
future orchestrating agent can read them over RPC. `docs/decisions/
0014-agent-profiles.md` (**Accepted, 2026-09-25**) records the data model,
storage, wsapi surface, apply mechanism, and CLI surface this plan
implements. This plan covers **Phase 2: implementation** -- Phase 1 (the
ADR plus this document) was research and design, no code.

Following the ADR's 2026-09-25 review, `model` is out of v1 entirely (not
in the schema, not on any RPC, not in the CLI, not in the UI) -- deferred
until `task.prompt` gains a real per-run model parameter end to end. `icon`,
`color`, `featureValues`, and `systemPrompt` remain excluded per the ADR's
original argument. No inert fields ship in this plan's implementation.

## Acceptance Criteria

**Store and migration**

1. `internal/store/schema.sql` gains an `agent_profiles` table (`id`,
   `name`, `provider`, `approval_policy`, `thinking_level`,
   `notes`, `created_at`, `updated_at` -- no `model` column, per the ADR's
   2026-09-25 review), created via
   `CREATE TABLE IF NOT EXISTS` -- no `internal/store/migrate.go` entry,
   since this is a new table, not a new column on an existing one.
2. `internal/store` exposes `CreateAgentProfile`, `GetAgentProfile`,
   `ListAgentProfiles`, `UpdateAgentProfile`, `DeleteAgentProfile`, with the
   same signature and error-wrapping conventions as `spaces.go`
   (`GetAgentProfile`/`DeleteAgentProfile` on a missing id return a clear
   wrapped not-found error, never a silent no-op or zero value).
3. `store.AgentProfile`'s `Provider`/`ApprovalPolicy`/`ThinkingLevel` fields
   are plain `string`, not `internal/taskrunner` types -- `internal/store`
   does not gain an import of `internal/taskrunner` (matches `store.Run`'s
   existing precedent, `internal/store/types.go:81-84`).

**`internal/profiles` package**

4. A new `internal/profiles.Registry` wraps `store.Store`'s agent-profile
   calls, validating: `name` non-empty; `provider` is one of
   `taskrunner.SupportedProviders()`'s ids; `approvalPolicy`, if non-empty,
   satisfies `taskrunner.ApprovalPolicy.IsValid()`; `thinkingLevel`, if
   non-empty, satisfies `taskrunner.ThinkingLevel.IsValid()`.
5. `Registry` exposes a nil-safe `Notifier`/`SetNotifier` hook mirroring
   `workspace.Manager`'s (`internal/workspace/workspace.go:30-68`), firing
   on create/update/delete.

**wsapi methods and events**

6. `internal/wsapi/handlers.go`'s `methodHandlers` registers `profile.create`,
   `profile.list`, `profile.get`, `profile.update`, `profile.delete`, with
   request/response shapes and error wrapping exactly as ADR-0014 specifies.
7. `internal/wsapi/events.go`'s `knownTopics` gains `profile.created`,
   `profile.updated`, `profile.deleted`, published via a new
   `busProfileNotifier` in `internal/wsapi/server.go` (mirroring
   `busWorkspaceNotifier`), with the payload shapes ADR-0014 specifies
   (`{profile: store.AgentProfile}` for created/updated, `{id}` for
   deleted).
8. A connection subscribed to `profile.*` topics receives the corresponding
   event after each mutation made on any connection (including the CLI's),
   matching ADR 0009's cross-client behavior for workspace/space/task.

**CLI**

9. `cmd/smind` gains `smind profile add|ls|rm`, dialing the daemon the same
   way `cmd/smind/account.go`/`space.go` do, per ADR-0014's CLI surface.

**Web Settings → Profiles**

10. A new "Profiles" settings section is registered via
    `settings-registry.ts` (alongside Appearance/Connections/etc.),
    supporting create, edit, and delete of profiles, styled per
    `docs/design.md`'s token vocabulary and `text-ui-*` scale (no
    hardcoded colors -- must pass `src/test/no-hardcoded-colors.test.ts`).
11. The Profiles section reflects `profile.created`/`updated`/`deleted`
    events live (a profile created from a second tab/the CLI appears
    without a manual reload), matching how other lifecycle-event-backed
    lists behave post-ADR-0009.

**Composer "Profiles" picker**

12. The composer (`composer.tsx`) gains a "Profiles" control. Selecting a
    profile sets `provider`/`approvalPolicy`/`thinkingLevel` state from the
    profile's fields in one click (client-side copy per ADR-0014's "How a
    profile is applied" -- no `task.prompt` wire change).
13. After selecting a profile, every field remains independently editable
    (selecting a profile is a one-time seed, not a locked mode) -- matches
    Paseo's "still editable before sending" behavior.

**Regression**

14. With zero profiles in the store, the composer's Profiles control
    renders nothing (or an inert empty affordance) and every other
    composer behavior -- default provider (`claude-native`), default
    approval policy (`manual`), default thinking level (unset) -- is
    byte-for-byte unchanged from before this feature.
15. The existing composer test suite (`composer.test.tsx`) passes
    unmodified (only new tests added, no existing assertions changed).
16. `task.prompt`'s wire shape and existing tests (`internal/wsapi`'s
    `run_test.go`/`task_test.go` and equivalents) pass unmodified --
    confirms the feature is additive-only, per ADR-0014's Compatibility
    section.

## Test Scenarios

**Store (`internal/store/agent_profiles_test.go`, new)**

- Create then get round-trips every field exactly.
- List returns profiles ordered by `id`, empty slice (not nil-panic) when
  none exist.
- Update replaces every field (full-record replace, not a partial patch).
- Delete then get returns a not-found error.
- Delete of a nonexistent id returns an error (not a silent no-op),
  matching `DeleteSpace`'s convention.

**`internal/profiles` (new package, its own `_test.go`)**

- Reject empty `name`.
- Reject an unknown `provider` id (not in `taskrunner.SupportedProviders()`).
- Reject an invalid `approvalPolicy` string; accept an omitted one.
- Reject an invalid `thinkingLevel` string; accept an omitted one.
- `Notifier` fires exactly once per create/update/delete; nil `Notifier` is
  a safe no-op (mirrors `runs.Registry`'s own notifier tests).

**wsapi (new `internal/wsapi/profile_test.go`, plus additions to
`lifecycle_events_test.go`)**

- `profile.create` -> `profile.get` round-trip over the wire.
- `profile.list` ordering after multiple creates.
- `profile.update` from one connection is visible via `profile.get` from a
  second connection.
- `profile.delete` then `profile.get` errors.
- `profile.create` with a missing `name` is a clear invalid-params error
  (not a 500-shaped generic error).
- `profile.create` with an unknown `provider` errors with a message naming
  the bad provider.
- A connection subscribed to `profile.created`/`profile.updated`/
  `profile.deleted` receives each event, with the documented payload shape,
  after a mutation made on a *different* connection -- mirrors
  `lifecycle_events_test.go`'s existing workspace/space/task coverage.

**CLI (`cmd/smind/profile_test.go`, new, or extending `account_test.go`'s
pattern)**

- `smind profile add <name> claude-native` prints the created row and exits
  0.
- `smind profile add <name> not-a-real-provider` exits nonzero with a clear
  message (matches `account.go`'s exit-code convention for a bad provider).
- `smind profile ls` lists a previously-added profile.
- `smind profile rm <id>` removes it; a subsequent `ls` no longer shows it.

**Web Settings (`settings/profiles-section.test.tsx`, new)**

- Creating a profile through the form makes it appear in the list.
- Editing a profile's fields persists and re-renders with the new values.
- Deleting a profile removes it from the list.
- Zero profiles renders an empty state (not a blank pane or an error).
- A `profile.created` event from outside this component's own mutation
  (simulated via the test's `WsClient` fake) updates the list without a
  manual refresh.

**Composer (`composer.test.tsx`, additions only)**

- Selecting a profile updates the provider/approval-policy/thinking-level
  `Select`s to match the profile's stored values.
- Changing a `Select` after selecting a profile is possible and the
  composer does not revert it back to the profile's value.
- With `profile.list` returning `[]`, the Profiles control does not render
  (or renders disabled/empty), and submitting a prompt behaves exactly as
  it does today (regression -- covers AC14).

**Regression**

- Full existing `composer.test.tsx` suite passes with zero modifications
  to existing test bodies (only additions).
- Full existing `internal/wsapi` test suite passes unmodified.

## Decisions

- Data model, storage, wsapi surface, event topics, apply mechanism, and
  CLI surface: see `docs/decisions/0014-agent-profiles.md` in full --  not
  duplicated here.
- Package name `internal/profiles.Registry`: chosen to mirror
  `internal/accounts.Registry`'s shape (thin validation + store wrapper +
  notifier hook), since ADR-0014 explicitly rejected putting the
  `taskrunner`-dependent validation directly on `store.Store` (import-cycle
  reasons) or inline in the wsapi handlers (duplication reasons).
- CLI verbs `add`/`ls`/`rm` (not `create`/`ls`/`delete`): matches
  `internal/accounts`' CLI precedent per ADR-0014's CLI surface section.
  No `edit` subcommand in v1 (web Settings covers it).
- `model` is dropped from v1 entirely (schema, RPC, CLI, UI) per the ADR's
  2026-09-25 review -- ADR-0014's "Deferred: `model`" section. This plan's
  implementation must not carry a `model` field anywhere (no inert column,
  no inert RPC field, no inert form input); add it back only alongside a
  real end-to-end `task.prompt` model parameter, as its own future plan.

## Progress

- 2026-09-25: ADR-0014 and this plan authored (Phase 1).
- 2026-09-25: ADR-0014 reviewed and accepted; `model` dropped from v1 scope
  entirely. ADR status set to Accepted. Implementation (Phase 2) starting.
- 2026-09-25: Phase 2 complete -- store table + `internal/profiles.Registry`,
  `profile.*` wsapi methods + lifecycle events, `smind profile add|ls|rm`,
  Settings → Profiles, and the composer Profiles picker all landed (one
  commit per AC group on `feat/agent-profiles`). `task test`, `task lint`,
  the web test suite, and `tsc -b` all green.

## Validation

All 16 acceptance criteria confirmed:

1. `internal/store/schema.sql`'s `agent_profiles` table, no `migrate.go`
   entry -- confirmed by reading the diff; `TestOpen`/`TestStore_Reopen
   ExistingDatabase` (unmodified) still pass against a schema that includes
   it.
2. `internal/store/agent_profiles.go`'s five methods -- `TestStore_Agent
   Profiles`, `TestStore_ListAgentProfilesOrdering`, `TestStore_ListAgent
   ProfilesEmpty`, `TestStore_UpdateAgentProfile`, `TestStore_UpdateAgent
   ProfileMissing`, `TestStore_DeleteAgentProfile`, `TestStore_DeleteAgent
   ProfileMissing` (`internal/store/agent_profiles_test.go`).
3. `store.AgentProfile`'s plain-`string` fields, no `internal/taskrunner`
   import -- confirmed by `go build ./...`/`go vet ./...` passing with
   `internal/store`'s import graph unchanged (only `internal/profiles`
   imports `taskrunner`).
4. `internal/profiles.Registry`'s validation -- `TestRegistry_CreateRejects
   EmptyName`, `...UnknownProvider`, `...InvalidApprovalPolicy`,
   `...InvalidThinkingLevel`, `...AcceptsOmittedApprovalPolicyAndThinking
   Level`, `TestRegistry_UpdateAppliesSameValidation`
   (`internal/profiles/registry_test.go`).
5. `Registry`'s `Notifier`/`SetNotifier` -- `TestRegistry_NotifierFiresOn
   Mutation`, `TestRegistry_NilNotifierIsNoop`,
   `TestRegistry_NilRegistrySetNotifierIsNoop`.
6. `profile.create/list/get/update/delete` wsapi methods -- `TestProfile_
   CreateGetRoundTrip`, `TestProfile_ListOrdering`, `TestProfile_UpdateVisi
   bleFromSecondConnection`, `TestProfile_DeleteThenGetErrors`, `TestProfile_
   CreateMissingNameIsInvalidParamsError`, `TestProfile_CreateUnknownProvid
   erErrors` (`internal/wsapi/profile_test.go`).
7. `profile.created`/`updated`/`deleted` topics + `busProfileNotifier` --
   `TestEvents_ProfileCreatedSubscribeAndReceive`, `...ReachesASecondClient`,
   `TestEvents_ProfileUpdatedSubscribeAndReceive`, `TestEvents_ProfileDeleted
   SubscribeAndReceive` (`internal/wsapi/lifecycle_events_test.go`).
8. Cross-client delivery -- `TestProfile_UpdateVisibleFromSecondConnection`
   (two connections, one mutates, the other reads) and `TestEvents_Profile
   CreatedReachesASecondClient` (two connections, one mutates, the other's
   *subscription* receives the event) together cover both the RPC and event
   paths.
9. `smind profile add|ls|rm` -- `TestRunProfileAddPrintsCreatedRow`,
   `TestRunProfileAddInvalidProviderExitsNonzero`, `TestRunProfileLsLists
   AddedProfile`, `TestRunProfileRmRemovesProfile`
   (`cmd/smind/profile_test.go`), against a real `wsapi.Handler`-backed test
   daemon, matching `account_test.go`'s own pattern.
10. Settings → Profiles CRUD -- `profiles-section.test.tsx`'s create/edit/
    delete/empty-state tests; `src/test/no-hardcoded-colors.test.ts` and
    `token-presence.test.ts` pass with the new file included (ran directly
    to confirm, not just via the full suite).
11. Live event reflection -- `profiles-section.test.tsx`'s "reflects a
    profile.created event..." and "...profile.deleted event..." tests, using
    a fake `DaemonEvents` that fires a topic without going through the
    component's own mutation path.
12. Composer Profiles picker, one-click seed -- `"selecting a profile seeds
    provider/approvalPolicy/thinkingLevel in one click"`
    (`composer.test.tsx`).
13. Fields stay editable after applying -- `"the Profiles trigger always
    shows its placeholder..."` and `"a field changed after applying a
    profile is not reverted by anything"` (`composer.test.tsx`).
14. Zero-profiles regression -- `"does not render when profile.list resolves
    empty"` and `"does not render while profile.list is still pending, and
    every other control works as before"` (`composer.test.tsx`); the latter
    submits a prompt and asserts the exact pre-feature payload shape.
15. Existing `composer.test.tsx` suite -- all 25 pre-existing tests pass
    with zero modifications to their bodies (only 5 new tests appended);
    confirmed by running the file before and after the composer.tsx change.
16. `task.prompt` wire shape unchanged -- full `internal/wsapi` suite
    (including `run_test.go`/`task_test.go`) passes unmodified; `task test`
    (Go + web) and `task lint` both green on the final tree.
