# Relay Origin policy for browser clients (ADR-0052 H10)

Status: active. Size S-M, **high-risk** (a relay change on a listener that faces the internet): validation contract, a validator on the exact head, two fresh reviewers, the Lead's own mutations. Decided by the Human (2026-11-29, ADR-0052 H10): **the relay accepts browser connections only from Origin `https://app.spacingmind.com` or from operator-configured origins.** Part of `docs/plans/active/web-relay-client.md` but its own pull request, because it changes the relay.

## What the code does today

`internal/relay/server/grpcweb.go` wraps the gRPC server with `grpcweb.WrapServer(..., WithWebsocketOriginFunc(always true), WithOriginFunc(always true))`: any `Origin` is accepted for CORS and for the websocket upgrade. Its comment says why: the relay's access control is the SCRAM-style admission handshake, not the same-origin policy, and a non-browser client (mobile, desktop, the daemon's own bridge) sends no browser-style `Origin`. A **browser** now reaches this listener (the hosted web client), so a hostile web page on any origin could otherwise talk to a relay a person's browser can reach (admission still needs the host secret, but the secret travels in the pairing link a person may paste into the wrong page; and a permissive CORS answer is wrong by default).

## Decisions

- **D1 The rule.** A request that carries an `Origin` header (a browser) is served only if the origin is on the allowlist; a request with **no** `Origin` header (native gRPC, mobile, desktop, the daemon bridge, `curl`) is **unchanged**. The literal origin `null` is refused.
- **D2 The list.** Default `["https://app.spacingmind.com"]`, plus operator-configured origins: `Config.AllowedOrigins []string` (nil means the default; a non-nil list REPLACES the default only through an explicit `--no-default-web-origin`; otherwise the operator's values are ADDED to the default) set by `smind relay --allowed-origin <origin>` (repeatable) and `SMIND_RELAY_ALLOWED_ORIGINS` (comma separated, for containers).
- **D3 Matching.** Exact: scheme, host (case-insensitive) and port, parsed from the `Origin` header; no wildcards, no suffix or subdomain match, no path, no userinfo. `app.spacingmind.com.evil.org`, `https://app.spacingmind.com:8443`, `http://app.spacingmind.com`, `https://evilapp.spacingmind.com` are all refused.
- **D4 Validation of the operator's values** at startup: `https://host[:port]`, or `http://` only for a loopback host (`localhost`, `127.0.0.1`, `[::1]`, for development); no path, query, fragment, userinfo or wildcard; otherwise the relay refuses to start with a message that names the value.
- **D5 Enforcement is on the server, not left to the browser's CORS.** An HTTP middleware in front of the grpc-web handler answers **403** to ANY request (unary POST, CORS preflight, websocket upgrade) whose `Origin` is present and not allowed; the grpc-web library's own `OriginFunc` and `WebsocketOriginFunc` use the same function so the CORS headers match.
- **D6 Logging.** A refused origin is logged once per distinct origin per minute with the origin string only (never a header dump), so an operator sees what to add.
- **D7 Out of scope.** The native gRPC listener (a browser cannot speak it), admission, the hosted relay, rate limits.

## Acceptance criteria

- AC1 No `Origin` header: unchanged (existing relay tests, the mobile and relay-web integration tests, the devharness pass).
- AC2 `Origin: https://app.spacingmind.com`: a unary call and a websocket bidi call succeed and the CORS headers name that origin.
- AC3 Any other origin (including `null`, a lookalike, another port, `http`): unary POST, preflight and websocket upgrade are all refused with 403 before reaching admission.
- AC4 Operator origins are accepted exactly and nothing else; invalid values stop the relay at startup.
- AC5 The CLI flag (repeatable) and the environment variable are parsed, merged with the default, and documented in `smind relay --help` and `public-docs/mobile-and-relay.md`.

## Test scenarios (named)

`internal/relay/server`: `origin_policy_allows_no_origin_and_listed_origins_only`, `origin_policy_rejects_null_and_lookalikes`, `operator_origins_are_validated`, `grpcweb_unary_from_the_default_origin_works_with_cors_headers`, `grpcweb_websocket_from_the_default_origin_works`, `grpcweb_from_a_disallowed_origin_is_403_for_unary_preflight_and_websocket`, `grpcweb_without_an_origin_is_unchanged`, `grpcweb_operator_origin_is_accepted`. `cmd/smind`: `relay_allowed_origin_flag_and_env_are_merged_with_the_default`, `relay_allowed_origin_invalid_stops_startup`.

## Mutations (Lead on committed code; validator and reviewers repeat)

M1 the allow function always returns true -> `grpcweb_from_a_disallowed_origin_is_403_...`. M2 the middleware is removed (only the library's origin funcs remain) -> the same test (unary POST gets 200). M3 an empty `Origin` is refused -> `grpcweb_without_an_origin_is_unchanged`. M4 suffix match instead of exact -> `origin_policy_rejects_null_and_lookalikes`.

## Staffing

Codex (profile 10) writes the relay server change; GLM (profile 8) the CLI flag, environment variable and docs (independent once the `Config.AllowedOrigins` and `ParseAllowedOrigins` signatures are fixed); peer-sonnet only with a logged reason. Two fresh reviewers, then the validator.

## Progress

- Spec and contract written first. Server (allowlist, parser, 403 middleware, logging) by Codex (profile 10); CLI flag/env/docs by GLM (profile 8); the API stub, the log-wording fix and the hygiene fix by the Lead.
- Lead mutations M1-M4 on the committed code: each makes the named tests fail (M1 with 200/101 and CORS headers on a refused origin; M2 fails the 403 test; M3 fails the no-Origin tests incl. the pre-existing grpc-web tests; M4 fails the lookalike test).

## Validation

- Pending.
