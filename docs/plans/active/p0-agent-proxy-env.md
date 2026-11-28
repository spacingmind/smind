# P0 A3: spawned agents get a safe proxy environment

Status: active. Size S/M. Go daemon only (`internal/netenv` plus its call sites in `internal/taskrunner`). No wire, store, UI or permission change. Context: the outbound-proxy work (#983, `docs/plans/*/outbound-proxy.md`) made the daemon's own provider calls honour `HTTPS_PROXY` / `HTTP_PROXY` / `NO_PROXY`. The agents the daemon launches inherit the daemon's environment, so the same variables reach them, but two things go wrong there.

## Problems

1. **A bare proxy value.** Claude Code before 2.1.327 refuses `HTTP_PROXY=host:port` (no scheme) in a headless run; the daemon's own transport reads a bare value as `http://host:port` (`netenv.RedactProxy`), so a setup that works for the daemon breaks every claude-native turn.
2. **Loopback goes through the proxy.** The agents talk to loopback services of the daemon (the MCP bridge, the browser proxy, the run socket). With a proxy set and no `NO_PROXY` loopback entry, an agent's own HTTP client sends those calls to the corporate proxy, which cannot reach them.

## Where the agent launch environments are built (verified in the code)

`Runner.launchEnvEntries` (`internal/taskrunner/provider_secrets.go`) is "everything a launch appends after the daemon's own environment, last entry wins". Every agent launch goes through it: ACP agents (`runner.go:568`, `acp_modes.go:223`, `acp_replay.go:50`), claude-native (`runner.go:1137`) and Codex (`runner.go:1495`). The claude-native isolated launch (`isolated.go:259`, probe / reflect / curate) builds its env from `providerEnvEntries` directly. **Not covered, on purpose:** the terminal shell (`internal/terminal/registry.go`: the user's own interactive shell), workspace setup/teardown scripts, the agent browser, plugin providers (a plugin's launch is a scrubbed, confined environment of its own: `pluginLaunch`, and `launchEnvEntries` refuses to build for them).

## Decisions

- **D1 One function.** `netenv.ProxyLaunchEntries(effective []string) []string` takes the environment the child will see (the daemon's `os.Environ()` followed by the entries a launch appends, last entry wins) and returns the extra `NAME=VALUE` entries to append so that:
  - **Only when a proxy is set**: any of `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY` in either case with a non-empty value. Otherwise it returns nil (the environment is untouched).
  - **Bare value -> `http://` value**: for `HTTP_PROXY`, `http_proxy`, `HTTPS_PROXY`, `https_proxy`, a non-empty value without `://` becomes `http://<value>` (userinfo, port and an IPv6 literal such as `[::1]:8080` kept as written). A value that already has a scheme, an empty value, and `ALL_PROXY` are left alone.
  - **Loopback in `NO_PROXY` and `no_proxy`**: `127.0.0.1`, `::1`, `localhost` are appended to each of the two names (to its existing value, comma-separated, in that order, skipping any entry already present, compared case-insensitively and ignoring surrounding spaces; an empty or absent value becomes exactly `127.0.0.1,::1,localhost`). Existing entries keep their order.
- **D2 No credentials anywhere but the child's environment.** The function returns entries only and writes no log, event or error text (the values may carry `user:pass@`). Tests assert a userinfo value never appears in anything but the returned entries.
- **D3 Applied at the two choke points**: the end of `launchEnvEntries` (covers ACP, claude-native, Codex) and the isolated claude launch env. The effective environment passed in is `os.Environ()` plus the entries built so far, so a proxy a provider configures in its own env (`providers.<id>.env`) is seen and normalised too.
- **D4 Reported, not changed**: terminals, setup scripts, the agent browser and plugin providers (above).

## Acceptance criteria

- AC1 Table tests of `ProxyLaunchEntries`: bare `host:port`, bare with userinfo, scheme'd value (`http://`, `https://`, `socks5://`) untouched, IPv6 `[::1]:8080` bare, empty values untouched, an existing `NO_PROXY` extended without duplicates and in order, mixed-case names handled independently, only `ALL_PROXY` set (loopback added, no scheme change), no proxy set (nil).
- AC2 `launchEnvEntries` and the isolated claude env carry the result: with `HTTPS_PROXY=proxy:3128` in the daemon's environment the child entries contain `HTTPS_PROXY=http://proxy:3128` and a `NO_PROXY` ending in the loopback names; with no proxy set the entries are exactly what they were.
- AC3 A userinfo proxy value is never written to a log, an event, an error or argv by this change.
- AC4 `task test`, `task lint`, `go test ./internal/hygiene/`, `sh scripts/verify.sh`.

## Test scenarios (named)

- `internal/netenv/launch_test.go`: `proxy_launch_bare_host_port_gets_http_scheme`, `proxy_launch_schemed_value_is_untouched`, `proxy_launch_userinfo_is_kept_and_never_logged`, `proxy_launch_ipv6_bare_value`, `proxy_launch_empty_values_are_untouched`, `proxy_launch_no_proxy_gets_loopbacks_without_duplicates`, `proxy_launch_mixed_case_names_are_independent`, `proxy_launch_all_proxy_alone_adds_loopback_only`, `proxy_launch_nothing_set_means_nothing_returned`.
- `internal/taskrunner/provider_secrets_proxy_test.go`: `launch_env_entries_carry_the_proxy_defaults`, `launch_env_entries_are_unchanged_without_a_proxy`, `isolated_claude_env_carries_the_proxy_defaults`.

## Staffing

One GLM peer from a step-by-step brief, one commit per step. Lead: spec, review, mutation checks, checks, PR.

## Progress

- Spec first, then one GLM peer (two commits), reviewed and cherry-picked by the Lead.
- Launch paths covered (all through `launchEnvEntries` or the isolated env): ACP agents (built-in and custom), claude-native turns, Codex turns, the claude isolated launch (probe / reflect / curate), and the ACP model probes. **Not covered**: the Codex model probe (`acp_modes.go` `probeCodexModels` builds its env from `providerEnvEntries` directly: a short-lived `model/list` call, not an agent run), terminals, setup scripts, the agent browser, plugin providers.

## Validation

- Mutations by the Lead on committed code: no-scheme branch removed fails `Test_proxy_launch_bare_host_port_gets_http_scheme` and `Test_launch_env_entries_carry_the_proxy_defaults`; loopback block removed fails `Test_proxy_launch_no_proxy_gets_loopbacks_without_duplicates`; `launchEnvEntries` without `withProxyLaunchDefaults` fails `Test_launch_env_entries_carry_the_proxy_defaults`.
- `go test ./internal/netenv/ ./internal/taskrunner/` pass with no proxy in the environment AND with `HTTPS_PROXY=proxy.corp:3128` set ambiently (one existing test, `TestPluginProvider_CoreProviderLaunchUnchanged`, clears the proxy names itself now).
- No contract: S/M, daemon env only; no wsapi, store, permission, relay or UI change (ADR-0045 asks a contract for those).
