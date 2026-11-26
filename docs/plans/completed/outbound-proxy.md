# E4 R4: outbound HTTP(S)_PROXY / NO_PROXY for provider calls

Status: completed (2026-11-26; spec 2026-11-26).

Size M (6): scope 2 (`internal/transport`, doctor and a few call sites), PRs 1, risk 3 (a network boundary: a wrong rule sends credentials to a proxy or around it), decisions 1 (env only, below), verify 1. HIGH-RISK-lite: two fresh reviewers (standards, attack), then the Steward.

## What exists (read from the code, 2026-11-26)

Every outbound HTTP client of the daemon, and whether it honours the environment proxy today:

| Site | Client | Honours `*_PROXY` today |
|---|---|---|
| `internal/transport` (`New`, `Client`, `StreamClient`): the uTLS client the LLM request proxy (`internal/server/proxy.go`), OAuth login and token refresh (`internal/accounts`) and usage fetch (`internal/quota/fetcher.go`) use | custom `http.Transport` with `DialTLSContext` only | **NO**: this is the gap: provider calls ignore the proxy |
| refreshers `Kimi/XAI/Antigravity` (`&http.Client{}`), `quota/zaiinstance.go`, `plugins/source.go`, `push/expo.go` | default transport | yes (Go's `DefaultTransport` uses `ProxyFromEnvironment`) |
| `selfupdate/release.go` | clone of the default transport, `Proxy` set | yes |
| `wsclient` (relay websocket) | `ProxyFromEnvironment` | yes (unchanged here) |
| `fleet/link/direct.go` (login to a fleet peer) | `ProxyFromEnvironment` | yes: **changes**: fleet traffic must never use the proxy (below) |
| forge calls (`gh`, `glab`) | child processes | inherit the environment, as before |
| `smind` CLI to the daemon (`status`, `stop`, `update`), `service` health checks | `http.DefaultClient` to `127.0.0.1` | Go skips loopback |
| `internal/browser/proxy.go` | its own enforcing transport | no, by design (stays separate) |

The uTLS transport cannot simply set `Transport.Proxy`: with a custom TLS dialer and an `http://` proxy, `net/http` does its own CONNECT and then a stdlib TLS handshake, which would drop the uTLS fingerprint (the point of the package). So the transport must do the CONNECT itself and run the uTLS handshake over the tunnel.

## Decision (taken, no Human decision needed)

D1 Environment only: `HTTPS_PROXY`, `HTTP_PROXY`, `NO_PROXY` and their lowercase forms, read once when a transport is built, with Go's `golang.org/x/net/http/httpproxy` semantics (host, `.suffix`, `*.suffix`, IP, CIDR, port; `*` for everything). No `config.yaml` setting: it would be a new public API shape; if the Human wants one it is a separate decision. `ALL_PROXY` is not used (Go does not).

## Design

- `internal/netenv` (new, tiny): `RedactProxy(raw string) string` returns `scheme://host[:port]` with no userinfo, path or query (`"(unparsable)"` for what does not parse, and the credentials of an unparsable value are never echoed); `ProxyEnv(getenv func(string) string) []ProxyVar` returns the set variables in a fixed order (`HTTPS_PROXY`, `https_proxy`, `HTTP_PROXY`, `http_proxy`, `NO_PROXY`, `no_proxy`) with `*_PROXY` values redacted and `NO_PROXY` values truncated to 120 characters. Used by the transport's debug log and by doctor.
- `internal/transport`: an option `WithProxyConfig(*httpproxy.Config)` (tests; default: `httpproxy.FromEnvironment()` read in `New`). The proxy decision for a target URL is `cfg.ProxyFunc()(u)` plus a hard rule that is applied FIRST and cannot be configured away: a target whose host is `localhost`, `*.localhost`, a loopback IP, the unspecified address, or a unix path is NEVER proxied (even with `HTTP_PROXY` set and `NO_PROXY` empty).
  - `https://` target through an `http://` proxy: `dialTLS` dials the PROXY (plain TCP, the same dial timeout), sends `CONNECT host:port HTTP/1.1` with `Host`, and `Proxy-Authorization: Basic ...` when the proxy URL has userinfo, requires a `200` status line (anything else: an error naming only the status code, never the URL or credentials), then runs the existing uTLS handshake over that connection with `ServerName` of the TARGET host (TLS verification unchanged, the proxy only sees the tunnel). The handshake and `ConnectionState` recording are the existing code.
  - `http://` target: `inner.Proxy` returns the proxy URL for plain-HTTP requests (net/http then sends the absolute-URI request and the credentials itself) and `nil` for `https://` requests (handled by `dialTLS`).
  - A proxy URL whose scheme is not `http` (`https://`, `socks5://`) is refused: the request fails with `transport: proxy scheme "<scheme>" is not supported for provider calls` (scheme only). It never falls back to a direct connection: a configured proxy that applies is never silently bypassed.
  - Debug log (`slog.Debug`) when a request goes through a proxy: `transport: via proxy` with `proxy` = `netenv.RedactProxy(...)` and `target` = host only (no path, query or userinfo). Proxy credentials are never logged anywhere, including error strings.
- `fleet/link/direct.go`: `Proxy: nil` (the login to a fleet peer is never proxied), pinned by a test with `HTTPS_PROXY` set.
- Browser: `browserEnv` is an allowlist and already omits `*_PROXY`; a new test pins that every `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`, `NO_PROXY` (upper and lower case) is dropped and that the launcher's args still carry the browser's own `--proxy-server=http://127.0.0.1:<port>`.
- Child processes (agent CLIs, `gh`): environment unchanged (untouched).
- Doctor: a finding `network.proxy` (level `info`, provider `network`) when any of the variables is set: `outbound proxy from the environment: HTTPS_PROXY=http://proxy.corp:3128; NO_PROXY=...; used for provider and usage calls; never for loopback, the browser or fleet peers`. No finding when none is set. Host only, never credentials; the `--json` output carries the same finding. If a set `*_PROXY` has a scheme other than `http`, the finding is level `warn` saying provider calls through it are refused, with `Fix: use an http:// proxy URL`.

## Acceptance criteria

- AC1 With `HTTPS_PROXY` set, a `transport.Client` GET of an `https://` URL goes through an `http://` proxy via CONNECT (the proxy sees `CONNECT host:port`, with `Proxy-Authorization` when the URL has userinfo), the uTLS connection state is recorded (fingerprint kept), the certificate is verified against the target host, and the response arrives.
- AC2 A plain `http://` request goes through the proxy as an absolute-URI request; `StreamClient` behaves the same.
- AC3 `NO_PROXY` (a host, a `.suffix`, a CIDR) bypasses: the proxy sees nothing.
- AC4 Loopback is never proxied: with `HTTP_PROXY`/`HTTPS_PROXY` set and `NO_PROXY` empty, requests to `127.0.0.1`, `[::1]`, `localhost` and `x.localhost` reach their server directly and the proxy sees nothing.
- AC5 A proxy URL with scheme `socks5` or `https` makes the request fail with the fixed message and no direct connection is made (the target server sees no connection).
- AC6 A CONNECT answered by a non-200 fails with an error naming the status code only; neither the error nor any log line contains the proxy credentials (a test greps the error text and the captured debug log for the password).
- AC7 With no proxy variable set, behaviour is byte-for-byte the previous one (the existing transport tests pass unchanged).
- AC8 The login to a fleet peer is never proxied (`HTTPS_PROXY` set, the peer sees the request, the proxy none).
- AC9 `browserEnv` drops every `*_PROXY` / `NO_PROXY` variable (both cases); launcher args keep `--proxy-server=http://127.0.0.1:...`.
- AC10 `smind doctor` prints the `network.proxy` line (info or warn) with the redacted host and no credentials; nothing when no variable is set; `--json` includes it.
- AC11 `netenv.RedactProxy` strips userinfo, path and query and never echoes credentials from an unparsable value.
- AC12 No config.yaml change; no change to child process environments; `GOOS=windows go vet` is clean.

## Named tests

- `internal/netenv/netenv_test.go`: `TestRedactProxy` (table: userinfo, path, query, bare host:port, IPv6, unparsable with a password), `TestProxyEnvOrderAndRedaction`.
- `internal/transport/proxy_test.go`: `TestProxy_HTTPSViaConnectKeepsUTLS`, `TestProxy_ConnectSendsBasicAuth`, `TestProxy_PlainHTTPViaAbsoluteURI`, `TestProxy_StreamClientToo`, `TestProxy_NoProxyBypasses` (host, suffix, CIDR), `TestProxy_LoopbackNeverProxied` (127.0.0.1, ::1, localhost, a.localhost, with NO_PROXY empty), `TestProxy_UnsupportedSchemeFailsClosed` (socks5, https; the target sees no connection), `TestProxy_ConnectRefusedNamesStatusOnly`, `TestProxy_CredentialsNeverInErrorsOrLogs`, `TestProxy_UnsetIsUnchanged`. The proxy is a test CONNECT proxy on a loopback listener that records what it saw; the proxied target is `example.com:<port>` (non-loopback by name, the httptest certificate is valid for it) and the test proxy connects to the real httptest server.
- `internal/fleet/link`: `TestDirectLoginIgnoresProxyEnv`.
- `internal/browser/launch_test.go`: `TestBrowserEnvDropsProxyVariables`.
- `internal/doctor/proxy_test.go`: `TestOutboundProxyFinding` (none set, https set with credentials, scheme warn, NO_PROXY truncation); `cmd/smind` doctor test for the text and JSON.

## Mutations (committed code; each must fail a named test)

- Loopback guard removed -> `TestProxy_LoopbackNeverProxied`.
- `NO_PROXY` ignored (config without it) -> `TestProxy_NoProxyBypasses`.
- Unsupported scheme falls back to direct -> `TestProxy_UnsupportedSchemeFailsClosed`.
- CONNECT without `Proxy-Authorization` -> `TestProxy_ConnectSendsBasicAuth`.
- Error or log includes the raw proxy URL -> `TestProxy_CredentialsNeverInErrorsOrLogs`.
- Handshake through the tunnel uses the proxy host as `ServerName` -> `TestProxy_HTTPSViaConnectKeepsUTLS`.
- Fleet login back to `ProxyFromEnvironment` -> `TestDirectLoginIgnoresProxyEnv`.
- `*_PROXY` added to the browser allowlist -> `TestBrowserEnvDropsProxyVariables`.
- Doctor prints the userinfo -> `TestOutboundProxyFinding`.

## Not in this PR

A `config.yaml` setting, SOCKS or HTTPS proxies for provider calls, proxy auto-config (PAC), proxy support inside the browser, changing the relay websocket client or the default-transport clients (they already honour the environment).

## Progress

- [x] spec (this file)
- [x] netenv, doctor, fleet, browser env test (GLM peer 1)
- [x] transport (GLM peer 2), hardening and docs from the reviews (GLM peer 3)
- [x] two fresh reviews, mutations, validation

## Validation

- AC1 to AC7: `internal/transport/proxy_test.go` (`TestProxy_*`, 14 tests, `-race -count=20` clean): HTTPS through CONNECT keeps the uTLS state and verifies the target host, Basic auth reaches the proxy, plain HTTP goes as an absolute URI, `StreamClient` too, `NO_PROXY` host/suffix/CIDR bypass, loopback (127.0.0.1, localhost, a.localhost, [::1], zoned and numeric spellings) never proxied with a proxy set and `NO_PROXY` empty, `socks5://` and `https://` proxies fail closed with no direct connection, a 407 names the status only, credentials never in errors or the debug log, an empty config is unchanged, the environment is read at construction. The existing transport tests pass unchanged.
- AC8: `TestDirectLoginIgnoresProxyEnv` (`directTransport` has no Proxy). AC9: `TestBrowserEnvDropsProxyVariables`. AC10: `TestOutboundProxyFinding` (info, warn for a non-http scheme, warn for an unparsable value, truncation, no credentials) and `TestDoctorShowsOutboundProxy` (text and `--json`, nothing when unset). AC11: `TestRedactProxy`, `TestProxyEnvOrderAndRedaction`. AC12: no config change; `GOOS=windows go vet` on the touched packages is clean.
- Mutations on committed code, each failing its named test: loopback guard removed (`TestProxy_LoopbackNeverProxied`, after the dial hook was fixed to let the proxy address through: the first version of the test could not see a wrongly proxied plain-HTTP request), `NO_PROXY` ignored, a non-http scheme falling through to direct, no `Proxy-Authorization`, the proxy URL in the status error, the proxy host as `ServerName` (`TestProxy_HTTPSViaConnectKeepsUTLS`), target validation removed (`TestProxy_ConnectTargetWithControlBytesRefused`), `context.AfterFunc` removed (`TestProxy_ConnectHonoursContext`), the 16 KiB cap widened (`TestProxy_ConnectHeaderIsCapped`, rewritten after the first version passed without the cap), the numeric-host rule removed, plus peer-run mutations on doctor, fleet, browser env and netenv.
- Reviews (two fresh, standards and attack): no leak found. Accepted and fixed: CONNECT target not validated (CR/LF), ctx not honoured during CONNECT and no cap on the reply, zoned and numeric loopback spellings, missing public docs (`public-docs/troubleshooting.md`, "Behind a proxy"), the doctor message for an unparsable value. Accepted and documented, not changed: the default-transport clients (Kimi, xAI and Antigravity refreshers, z.ai usage, plugin fetch, push) use Go's `ProxyFromEnvironment`, which also accepts `socks5://` and has only Go's own loopback rule; with `REQUEST_METHOD` set, Go ignores `HTTP_PROXY` for http targets (a CGI rule), so such a daemon goes direct for plain-HTTP targets.
- Not verified: a real corporate proxy (only an in-test CONNECT proxy); Windows runtime; a real provider call through a proxy (no model call made).
