# 0015: Account management RPCs

## Status

Accepted (2026-09-25)

## Context

Accounts are global `store` rows (`internal/store/schema.sql`'s `accounts`)
managed today through exactly four wire methods (`internal/wsapi/handlers.go`'s
`methodHandlers`): `account.add`, `account.oauthStart`, `account.list`, and
the adjacent `provider.list`/`provider.test` diagnostics. (The plan doc calls
these `account.create`/`.test`; the real names are `add`/`oauthStart`/`.test`
as above — this ADR uses the real names.) There is no way to rename an
account, replace a rotated credential in place, or remove an account at all:
a mislabeled or stale account today means delete-by-hand in SQLite or living
with it. The Providers settings section
(`docs/plans/active/providers-settings.md`) needs all three, which per
AGENTS.md rule (d) is a wire-surface decision needing this ADR first.

Two facts of the existing code dominate the design:

1. **Routing is per-request, not per-run.** `runs` rows carry no `account_id`;
   the only run↔account linkage is `routing_decisions` (session key →
   account id, 24h affinity TTL), where the session key is a SHA-256 of
   whatever credential the *caller* sent (`internal/server/proxy.go`'s
   `sessionKey`). Every proxied request re-lists accounts, consults the
   affinity decision, and then re-reads the account's live
   `credential_data` (`router.Route` → `registry.Get`, plus
   `EnsureFresh`'s refresh-and-persist for OAuth). There is no daemon-side
   "this run owns this account" association to consult or preserve.
2. **Foreign keys are enforced** (`_pragma=foreign_keys(1)`, see
   `DeleteTask`'s doc comment): `routing_decisions`, `quota_snapshots`, and
   `workspace_accounts` all FK-reference `accounts(id)`, so an accounts-row
   delete is impossible without deleting those children first — the cascade
   shape is forced by the schema, not a free choice.

The plan's originally-proposed default — "removing an account never affects
an in-flight run" — is therefore wrong as stated: a hard-deleted row makes
the very next proxied request for a session affinity-pinned to it fail inside
`router.Route` (`registry.Get` → `sql.ErrNoRows` → 503). This ADR replaces
that default with an explicit statement (see Removal semantics).

## Decision

### New RPCs

| Method | Params | Result | Notes |
| --- | --- | --- | --- |
| `account.rename` | `{id, label}` | the updated `accountResult` | label must be non-empty; unknown `id` is a clear not-found error, matching `UpdateAgentProfile`'s convention |
| `account.updateCredential` | `{id, credential, baseUrl?}` | the updated `accountResult` | credential parsed exactly the way `account.add` parses it (JSON with `refresh_token` → oauth; otherwise api_key, with `ValidateBaseURL` on `baseUrl`); a swap may change `credential_type` — both `credential_data` and `credential_type` are replaced |
| `account.remove` | `{id}` | `{}` | unknown `id` is a clear not-found error; no `deleteSummaryResult`-shaped body, since the cascaded child counts (routing decisions, quota snapshots, workspace links) aren't user-meaningful |

**Two single-purpose RPCs instead of the plan's single `account.update`.**
ADR-0014 already documented this repo's convention: "no existing smind
mutation RPC does field-level PATCH semantics — `task.move`/`task.archive`
are each a dedicated single-purpose operation". A generic `account.update`
with optional fields would introduce smind's first PATCH. It also can't be a
full-record replace the way `profile.update` is: the credential is
write-only (never returned by `account.add`/`account.list`), so a client
could never echo the old value back. Splitting sidesteps both problems and
costs one extra method. **Confirmed by the user, 2026-09-25** (open
question 1).

The store grows `RenameAccount(id, label)` and a credential-swap call;
today's `UpdateAccountCredential(id, data)` (written for `EnsureFresh`'s
refresh path) replaces only `credential_data`, so the swap path either gains
a `credentialType` parameter or a sibling method — an implementation detail
for the plan's Step 1, not a wire concern.

### Removal semantics: hard delete with explicit cascade, no soft-delete

`store.DeleteAccount(id)` deletes, in child-before-parent order:
`routing_decisions` rows for the account, `quota_snapshots` rows,
`workspace_accounts` links, then the `accounts` row — the same explicit
cascade style as `DeleteTask`/`DeleteWorkspace`, and in any case forced by
the enforced FKs. The cascade explicitly includes the routing
session-affinity rows, so no session stays pinned to the deleted account.

**Effect on in-flight runs, stated explicitly (this replaces the plan's
"never affects an in-flight run" default):** removing an account takes
effect immediately on the next proxied request for any session whose
affinity pointed at it. Because the delete also removes the session's
`routing_decisions` row, `router.Route` no longer finds the affinity
decision and re-routes: a sibling account of the same provider (one that
isn't quota-exhausted) picks the run up mid-flight; only when the removed
account was the provider's last one does the next proxied request fail
(503, "route request" error). This is the same recovery story an
OAuth-refresh failure or a quota-exhausted account already produces today —
removal does not add a new failure mode, it reuses routing's existing
failover. **Accepted by the user, 2026-09-25** (open question 2), with two
conditions on the implementation:

1. The Remove confirmation in the UI must warn when runs are currently
   using the account — "N running tasks will switch to another account" /
   "will fail: this is the last `<provider>` account" — not a generic
   confirmation.
2. The cascade must clear routing session-affinity rows (already the
   design above), so no session stays pinned to the deleted account.

**Why not soft-delete/deactivate** (an `accounts.status` or `removed_at`
column keeping the row alive until no routing decision references it):
nothing in `internal/store` works that way. Every existing delete is a hard
delete with an explicit child-before-parent cascade
(`DeleteTask`/`DeleteSpace`/`DeleteWorkspace`/`DeleteAgentProfile`), and the
one status-flag precedent, `tasks.archived_at`, is a user-visible lifecycle
state with its own `task.archive` RPC and `task.archived` event — not a
referential-integrity mechanism. A soft-deleted account would additionally
need filtering out of `account.list`, the proxy's candidate enumeration
(`proxy.serve` lists *all* accounts of a provider), `provider.test`'s
credential check, and `workspace_accounts` joins, plus a reaper for "until
no run references it" — but the daemon has no run→account reference to
reap against (fact 1 above: `runs` has no `account_id`), so the tombstone
would be kept alive by nothing more definite than the 24h affinity TTL. A
deferred hard delete keyed on routing-decision expiry would keep the
credential (the thing removal is meant to revoke) live and usable by the
proxy for up to 24h after the user clicked Remove. Soft-delete is the
wrong fit twice over: no precedent to match, and no reference to wait on.

**Routing/quota state:** deleted with the row, per the cascade above — no
orphaned quota rows (the plan's proposed default, confirmed: nothing in
`internal/routing`/`internal/quota`/`internal/store` assumes an account's
snapshots outlive it; `quota.Poller` is store-backed with a TTL, no
in-memory cache keyed by account, so nothing stale survives in memory
either).

### Lifecycle events (ADR 0009 shape)

Two new topics in `internal/wsapi/events.go`'s `knownTopics`:

```
account.updated  {"account": accountResult}
account.removed  {"id": 7}
```

`updated` carries the full credential-free `accountResult` snapshot (same
shape `account.list` returns — note it is the wsapi struct, not a bare
`store.Account`, precisely so no `credential_data` can ever ride along);
`removed` carries just `id`, matching `profile.deleted`'s shape (an account
has no parent entity). `rename` and `updateCredential` both publish
`account.updated`; there is no separate `account.renamed` topic, matching
how `task.move` publishes plain `task.updated`.

### CLI surface

`smind account rm <id>` — calls `account.remove`, prints nothing on
success beyond exit 0 (matching `profile rm`'s terseness). The usage line in
`cmd/smind/account.go` grows the verb. **No `account edit`/`account
rename` in v1** (confirmed by the user, 2026-09-25, open question 3): web
Settings covers renaming, and the CLI rename path would want interactive
editing that isn't worth the flags; deferred the same way ADR-0014 deferred
`profile edit` — explicitly, not silently. (If wanted later, it is a purely
additive `account.rename` CLI wrapper.)

### Compatibility

Purely additive: three new wsapi methods plus one store delete, two new
event topics, one new CLI verb. No existing method's params, result, or
behavior changes. Clients that only know `account.add`/`account.oauthStart`/
`account.list`/`provider.test` keep working unchanged; they simply never
learn about removals unless they subscribe to `account.removed`. Old
daemons reject the new method names with the standard unknown-method error,
which clients already handle for any method newer than their daemon.

## Alternatives considered

- **Soft-delete / deactivation** (`accounts.status` or `removed_at`, keep
  the credential row alive until no run references it). Rejected: no
  soft-delete precedent anywhere in `internal/store` (every delete is a
  hard cascade), and there is no run→account reference to wait on — the
  daemon's only linkage is a 24h-TTL routing decision, so "until no run
  references it" degenerates to "for up to 24 hours after Remove", keeping
  the credential the user asked to revoke live and proxy-usable. See
  Removal semantics.
- **A single generic `account.update`** covering label and credential.
  Rejected: introduces smind's first PATCH-semantics RPC, and cannot be a
  full-record replace because credentials never round-trip. See New RPCs.
- **`account.remove` also emitting `provider.test`-style readiness
  information or returning a cascade summary.** Rejected: `workspace.delete`
  returns a summary because tasks/spaces are user-visible things the user
  may not know they're deleting; an account's routing decisions and quota
  snapshots are internal bookkeeping, and the UI already re-tests providers
  on the detailed remove warning (see Removal semantics condition 1).
- **Refusing removal while any routing decision references the account.**
  Rejected: routing decisions are per-request, not per-run, and expire on
  their own; blocking Remove on them would make the common case (no run in
  flight, stale affinity rows) impossible for no user-visible benefit.

## Consequences

- Removing an account immediately revokes its credential from the proxy:
  the next proxied request for a session pinned to it re-routes to a
  sibling account, or fails if it was the provider's last account. The
  Providers UI's remove confirmation must state this rather than implying
  in-flight runs are guaranteed to finish on the old account — and, per
  the accepted condition, must warn with the current-run count and the
  last-account case. This contradicts the plan's originally-proposed
  default; the plan doc should be read with this section as its
  replacement.
- A credential *swap* (`account.updateCredential`) on an account that
  routing decisions still point at changes the credential those sessions
  use on their very next request — the desired behavior for a rotated key,
  and worth knowing for a "swap to a different account entirely" use
  (Remove + Add is the correct flow there).
- `internal/store` grows its first account-delete cascade; the
  child-before-parent order is already the established pattern, so this is
  pattern-following, not pattern-making.
- The plan's test scenario 1 ("a run already attached to that account's
  session keeps running to completion") must be read as: keeps running
  *only if* a sibling account of the same provider exists to fail over to;
  with the removed account as the provider's sole account, the next
  proxied request 503s. The Step 1 test suite should cover both branches.

## Open questions

None remaining — all three were answered by the user on 2026-09-25 and
folded into the sections above:

1. RPC split: `account.rename` + `account.updateCredential` (confirmed).
2. In-flight semantics: hard delete with re-route-or-fail on the next
   proxied request, with the two UI/cascade conditions (confirmed).
3. CLI rename: deferred from v1; editing is web Settings only (confirmed).
