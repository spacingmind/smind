# Browser for agents: acceptance criteria for the implementation PRs

Governing ADR: `docs/decisions/0061-browser-for-agents.md` (DRAFT; nothing here starts until the Human has answered its decisions). This file is the spec the later PRs are written against: acceptance criteria and named tests first. One PR at a time, each from a fresh base, never stacked. Order (recommendation 3 of the ADR): B1 broker and policy, B2 headless host, B3 settings toggle and allowlist, B4 desktop host.

## Provider reach (evidence, read from the daemon code)

| Provider | Gets `browser_*`? | Mechanism | Evidence | Caveat |
|---|---|---|---|---|
| claude-native | Yes | `--mcp-config` temp file, strict config for managed runs | `internal/taskrunner/runner.go:974-1004`, `internal/taskrunner/mcp.go:166,213` | Tool names `mcp__<server>__<tool>`; calls reach the provider's own permission mode (ADR-0019) |
| codex-native | No | No per-thread MCP; every row marked dropped | `internal/taskrunner/runner.go:1456-1471`, ADR-0018 | A config-file approach is not implemented |
| ACP: OpenCode, Copilot, Cline, Goose, Cursor | Yes if the agent advertises MCP | `session/new` `mcpServers`, filtered by advertised transports | `internal/acp/mcp.go`, `internal/taskrunner/mcp.go:47-110`, `internal/acp/client.go:255-300` | Several are marked unverified; tool-name prefixing is agent-defined |
| GLM, Kimi (ACP) | Yes (text tools only for GLM) | same ACP path | `internal/providers/providers.go:275-300` | GLM `Images: "no"`: no screenshots |
| Compatible recipes extending the Claude harness | Offered, not guaranteed | same `--mcp-config` path; `NoMCPServers` inherited | `internal/providers/recipes.yaml`, `providers.go:494` | At least one recipe is documented to ignore MCP servers |
| Plugin ACP providers | Only with manifest MCP support and consent | `NoMCPServers` unless declared; `withoutSmindMcp` strips smind rows | `internal/plugins/providers.go:159-161`, `internal/taskrunner/plugin_provider_launch.go:53` | `smind-browser` must follow the same strip rule (decided in B1) |

Extension points: a separate reserved row appended in `Runner.mcpRowsFor` (`internal/taskrunner/mcp.go:270`), gated by a new `McpPolicy` field (`internal/taskrunner/run_contract.go:27`, set in `internal/runs/contract.go`); exact-name permission rules `mcp:smind-browser.<tool>` (`internal/permpolicy/load.go:307`). Images in tool results do not exist yet anywhere in `cmd/smind`.

## PR B1: broker, policy, limits, human-only enable (daemon only)

- AC1 A reserved MCP row `smind-browser` (stdio child of the smind binary, run identity in env) is appended in `mcpRowsFor` only when ALL hold: the workspace switch is on, the daemon-wide kill switch is off, the run contract allows it, and a host is available. Otherwise the row is absent (not present-and-refusing).
- AC2 The tool set is exactly `browser_open`, `browser_tabs`, `browser_close`, `browser_snapshot`, `browser_screenshot`, `browser_console`, `browser_wait`, `browser_click`, `browser_fill`, `browser_type`, `browser_press`, `browser_scroll`, `browser_back`, `browser_forward`, `browser_reload`; a pin test fails on any other tool name (no evaluate, upload, download).
- AC3 The navigation policy is one pure function `checkURL(policy, rawURL) -> allow | refuse(reason)`: http(s) only; loopback by default; exact added hosts; private ranges, link-local and metadata addresses refused unless the exact host was added; the daemon's own listen address, the desktop proxy origin and listed daemon connections refused whatever the allowlist; userinfo refused; decimal/octal/hex/IPv4-mapped forms normalised before comparing. It is applied to the first URL and every redirect hop, and a named host's resolved addresses are checked as well.
- AC4 Every tool result is wrapped by the broker in the untrusted-content envelope naming the origin; limits are enforced by the broker regardless of the host: snapshot 40,000 characters and 1,200 nodes, console 100 lines of 500 characters, wait 15 s, screenshot 1,568 px and 1.5 MB, each truncated with a marker. Password input values never appear.
- AC5 Rate limits: 30 calls per minute and 500 per run, 10 navigations per minute, one call in flight per run, 20 s per call; one session per workspace, 5-minute idle close, 3-tab pool per run; at most 2 concurrent headless sessions daemon-wide. A refusal names the limit.
- AC6 Enabling is human-only: the `browser.enable` RPC (per workspace: switch, allowlist, profile mode) is callable only by the human principal (ADR-0056); the MCP child, hooks, plugins and agent runs get a refusal. The daemon-wide kill switch is a config value an RPC cannot change.
- AC7 Images: tool results can carry image content; the broker returns an image block only when the run's provider catalogue entry says images are read, and a text note otherwise.
- AC8 Provider gating: no row for codex-native; the row follows the existing strip rule for plugin providers; the Settings text (B3) lists the providers that cannot use it.
- AC9 Every call is a run-timeline event with URL and outcome (no page content in the event).
- Named tests: `checkurl_table` (every form in AC3, redirect chain, resolved-address case), `browser_row_present_only_when_all_gates_hold`, `browser_tool_set_is_pinned`, `envelope_wraps_every_result`, `limits_truncate_with_marker`, `password_values_never_returned`, `rate_limit_table`, `enable_is_human_only`, `image_only_for_image_providers`, `codex_native_gets_no_row`.
- Mutations: allowlist widened to `*`, the daemon port not denied, redirect not re-checked, envelope removed, a limit raised, the enable RPC callable by the MCP principal, the row appended without the switch.

## PR B2: headless host on the daemon

- AC10 An optional host that drives a system headless Chromium (driver per ADR decision 2) with a per-workspace user-data-dir (ephemeral by default: removed at session end) and host-resolver rules mapping everything outside the allowlist to nothing. Missing browser: the daemon reports "no browser installed" with the install step and the tools are not offered.
- AC11 Popups open as tabs in the 3-tab pool under the same policy; `window.open` never creates an OS window; downloads are blocked and reported; `confirm`/`prompt`/`beforeunload` are cancelled and reported; `alert` dismissed.
- AC12 Screenshots are of the viewport only, with password inputs blurred.
- AC13 Resource limits: a memory cap on the browser process, a kill on run end, idle close, the concurrent-session cap.
- Named tests (against fixture pages served on loopback): `popup_pool_cap`, `download_blocked_and_reported`, `confirm_is_cancelled`, `file_scheme_redirect_refused`, `two_workspaces_never_share_cookies`, `ephemeral_profile_removed`, `password_field_blurred_in_screenshot`, `prompt_injection_fixture_stays_inside_the_envelope`, `oversized_page_truncated`, `session_killed_on_run_end`. They run without a model call and skip with a clear message when no Chromium is installed.
- Mutations: a popup beyond the pool allowed, a download written, `confirm` accepted, the profile persisted when ephemeral, the resolver rules removed.

## PR B3: settings toggle, allowlist, indicator (web)

- AC14 Settings has a per-workspace Browser section: the switch (default off) with a plain description and the provider list that can and cannot use it, the allowlist editor (exact hosts), the profile mode (ephemeral or persistent), and "clear browser data". Changing any of it uses the human-only RPC; there is no way to change it from a run.
- AC15 A run that is using the browser shows it in the timeline and in the task header (current URL, a stop control that ends the session).
- AC16 Light and dark frames at 1280 and 390 of: off, on with the allowlist, the provider list, a run using the browser, the "no browser installed" state.
- Named tests: component tests for the section (default off, enabling calls the human RPC, allowlist validation rejects wildcards and schemes), the provider list text pinned against the registry, the header indicator and stop control.

## PR B4: desktop host (Tauri child webview)

- AC17 The desktop app registers as a browser host over its daemon connection and is used only when the daemon is local, the host is registered and idle; otherwise the headless host is used. A host that disconnects mid-call fails the call.
- AC18 The host webview is created in Rust with a label no capability grants (exact-list test) and no invoke bridge; it has a data store per workspace; its navigation and new-window handlers apply the same `checkURL` (the policy is shared code or a shared table, tested against the same fixtures); the proxy origin is not navigable.
- AC19 A visible "agent is controlling the browser" indicator and a stop button in the app window; the person's own interaction with the webview is allowed and reported to the agent on the next call.
- AC20 The origin guard still holds: a fixture page in the host webview cannot call any command.
- Named tests: `host_label_matches_no_capability`, `host_page_cannot_invoke`, `proxy_origin_not_navigable`, `remote_daemon_never_uses_desktop_host`, `data_store_per_workspace`, `stop_ends_the_session`.
- Mutations: the host label added to a capability pattern, the proxy origin allowed, the desktop host used for a remote daemon.

## Not in these PRs

`browser_evaluate`, uploads, downloads, a mobile browser view, a persistent shared profile, wildcard allowlists, codex-native support.
