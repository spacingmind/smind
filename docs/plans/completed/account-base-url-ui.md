# Wire base_url through the user-facing surfaces

## Goal

`accounts.Registry.AddAPIKeyWithBaseURL` / `ValidateBaseURL` (PR #140,
`feat/account-base-url`) added per-account upstream `base_url` override at
the Go API layer only. There is no way for a user to actually set one — no
CLI flag, no wsapi field, no UI field. Wire it through all three surfaces:
`account.add` wsapi handler, `smind account add` CLI, and the accounts
dialog's manual-add form.

## Design

- wsapi: `handleAccountAdd`'s params struct gains an optional `BaseURL
  string` (`json:"baseUrl,omitempty"`). Only applies on the api-key path
  (`registry.AddAPIKeyWithBaseURL`); the oauth path is untouched — base_url
  has no meaning for an OAuth credential.
- CLI: `smind account add` gains an optional `--base-url <url>` flag,
  parsed alongside the two existing positional args, passed through as
  `baseUrl` in the `account.add` RPC call.
- UI: `accounts-dialog.tsx`'s manual "Paste a credential instead" form
  gains an optional "Base URL (optional)" text input, behind its own
  collapsed disclosure nested in the manual form (mirroring the existing
  `showManual` disclosure pattern), shown only when the selected
  provider's `credentialKind === "api-key"` (base_url has no meaning for
  an oauth credential in this form either). Wired into `add()`'s request
  body as `baseUrl`, omitted when empty.

## Acceptance criteria

1. `account.add` wsapi RPC accepts an optional `baseUrl` param and passes
   it through `AddAPIKeyWithBaseURL` on the api-key path; a rejected
   base_url (`ValidateBaseURL` error, e.g. non-loopback `http://`)
   surfaces as a normal RPC error.
2. `smind account add --base-url <url> <provider> <label>` sends `baseUrl`
   in the RPC call; omitting the flag sends no `baseUrl` (existing
   behavior unchanged).
3. Accounts dialog: an api-key provider's manual-add form offers a
   collapsed "Base URL (optional)" field; submitting with it filled sends
   `baseUrl` in `account.add`'s params; leaving it empty omits the key
   entirely (existing tests' exact-params assertion keeps passing
   unchanged). The field is not shown for an oauth-kind provider.
4. `task build && task test && task lint` all green.

## Test scenarios

- wsapi: `TestServer_AccountAddWithBaseURL` — baseUrl passthrough,
  round-tripped via `registry.Get`.
- wsapi: `TestServer_AccountAddRejectsInvalidBaseURL` — `http://example.com`
  (non-loopback http) surfaces `ValidateBaseURL`'s error as the RPC error.
- CLI: extend `cmd/smind/account_test.go` with a case asserting
  `--base-url` is forwarded as `baseUrl` in the RPC params.
- UI: extend `accounts-dialog.test.tsx` with a case for the base_url field
  appearing/being sent for an api-key provider, and a case confirming it's
  absent for an oauth-kind provider.

## Out of scope

- Editing/removing base_url on an existing account (no `account.update`
  RPC exists at all yet, same gap noted in `accounts-dialog.tsx`'s doc
  comment).
- Any change to `AddAPIKeyWithBaseURL`/`ValidateBaseURL` themselves.

## Progress

- [x] wsapi handler + tests
- [x] CLI flag + tests
- [x] UI field + tests

## Validation

All 4 acceptance criteria met:
1. `handleAccountAdd` (`internal/wsapi/handlers.go`) takes optional
   `baseUrl`, routes it through `AddAPIKeyWithBaseURL` on the api-key path.
   `TestServer_AccountAddWithBaseURL` and
   `TestServer_AccountAddRejectsInvalidBaseURL` cover the round-trip and
   the rejection case.
2. `smind account add --base-url <url> <provider> <label>`
   (`cmd/smind/account.go`) forwards `baseUrl` in the RPC params;
   `TestRunAccountAddForwardsBaseURLFlag` covers it, existing
   `TestRunAccountAddDispatchesCredentialFromStdin` (no flag) unchanged.
3. `accounts-dialog.tsx`'s manual form gets a collapsed "Base URL
   (optional)" disclosure, shown only when the selected provider's
   `credentialKind === "api-key"`. Two new tests cover the api-key case
   (field present, `baseUrl` sent) and the oauth case (field absent);
   existing exact-params test for the no-base_url path is unchanged.
4. `task build && task test && task lint` all green (Go: 666 web tests +
   full Go suite pass; `go vet`/`gofmt` clean).
