# Desktop IPC: only the proxy origin may call the shell's commands

Status: active (2026-11-15). HIGH-RISK (a security boundary between the webview and the OS). Size: M (5): scope 1 (one Rust crate), PRs 1, risk 2, decisions 1 (below), verification 1 (unit and mock-runtime tests, a real-launch check).

## Problem

Both capabilities (`capabilities/proxy.json` for `main`, `capabilities/task-window.json` for `task-*`) grant their commands to `remote.urls: ["http://127.0.0.1:*"]`: any page on any loopback port. `on_navigation` pins only the top-level navigation to the proxy origin, so a page served from another loopback port that is rendered inside a window (a dev-server preview, another local service shown in the UI as an iframe) matches the scope and can call `open_path_default`, `open_in_editor`, `open_external`, `window_open_task` and the rest. The Human decided: a sensitive command must run only when the caller's origin equals the current proxy origin exactly (scheme, host and port).

## What Tauri 2.11 gives us (read from the source of the version in `Cargo.lock`)

- `InvokeRequest.url` is the URL of the frame that made the call. It is used in one place: the ACL (`resolve_access`) matches it against the capability's `remote.urls` patterns, and a request with no matching capability never reaches the command.
- It is **not** passed to the command: `tauri::ipc::Request` exposes only the body and headers, `CommandItem` and `InvokeMessage` carry the webview but not the request URL. A command can read `webview.url()`, which is the URL of the webview's top-level frame, never the calling subframe's.
- Capabilities can be added at runtime (`Manager::add_capability`, feature `dynamic-acl`, a default feature we already build with) and are resolved with the same ACL machinery.
- The loopback proxy is bound once per process, in `setup`, before any window exists (`serve_preferring`, called once), and the port is kept across launches. Switching the daemon connection changes where the proxy forwards to, not the proxy's origin. So "the current proxy origin" is fixed for the life of the process and is known before the first window is built.

Consequence: a command-level check alone cannot stop a subframe on another loopback port (the top-level URL is still the proxy's). The frame check has to live in the ACL, which does see the frame URL. So the design has two layers that do different jobs.

## Design

1. **Exact-origin capabilities (frame level, enforced by Tauri).** `capabilities/proxy.json` and `capabilities/task-window.json` move to `capability-templates/` with `remote.urls: ["__PROXY_ORIGIN__/*"]`, and `setup` adds them with `add_capability` after the proxy port is known, replacing the placeholder with `http://127.0.0.1:<port>`. The static `capabilities/default.json` (no permissions, `main`) stays. Nothing is widened: the permission lists, the window scopes and `local: false` are unchanged; only the URL pattern gets exact. If a capability cannot be added, the failure is logged and the app starts with no IPC (fails closed: the bundled UI loses its native features, no sensitive command is reachable).
2. **One guard on the invoke handler (command level, cannot be forgotten).** `ipc_guard::guarded(handler)` wraps the existing `generate_handler![...]` list: before any command runs it reads the calling webview's current URL and the managed `ProxyOrigin`, and rejects with a clear error and no side effect unless the URL's scheme is `http`, its host is `127.0.0.1`, its port is the proxy's and it carries no userinfo. A missing managed state, an unreadable URL or a command not on the guarded set's complement is refused. Every command in the handler is guarded by default; a command can only be exempted by naming it, with a reason, in `ipc_guard::EXEMPT` (empty today), and a drift test forces that decision.
3. **Core commands** (`core:window:*`, `core:event:*`) go through Tauri's own plugins, not our handler: only layer 1 covers them (they act on the calling window).

## Command inventory and classification

Source of truth: `capabilities/*.json` permissions, `generate_handler!` in `lib.rs`, `build.rs` (app commands). Classified from each command's effect; **all sensitive**, because a rule that exempts a read gains nothing (the ACL layer already gives the same protection) and "unsure means sensitive".

| Command | Class | Why |
|---|---|---|
| `connections_list`, `connections_get_current`, `connections_unsupported`, `connections_relay_status`, `connection_version` | sensitive | read the saved connections and their state (URLs, relay pairing state) |
| `connections_add`, `connections_add_relay`, `connections_preview_relay`, `connections_probe`, `connections_add_ssh`, `connections_remove`, `connections_select`, `connections_relay_set_password` | sensitive | change or probe where the app connects; accept a password |
| `app_settings_get`, `app_settings_set` | sensitive | app settings (daemon management, onboarding) |
| `open_external`, `open_path_default`, `open_in_editor`, `editors_list` | sensitive | launch the OS browser, an application or an editor; reveal or open a path |
| `window_open_task`, `window_ready`, `window_set_theme` | sensitive | create or change a window, show or theme it |
| `context_menu_show` | sensitive | pops a native menu and emits an event |
| `notify_test` | sensitive | posts an OS notification |
| `app_menu_action`, `app_quit` | sensitive | run an app-menu action; quit the app |
| `daemon_status`, `daemon_install`, `daemon_update`, `daemon_restart`, `take_over_daemon` | sensitive | inspect, install, update, restart or take over the local daemon |
| `app_update_check`, `app_update_install` | sensitive | check for, download and install an app update |
| `core:window:*`, `core:event:*` | not our handler | ACL layer only (exact-origin capability) |

The exempt list (`EXEMPT`) is empty. Every plugin permission is absent from both capabilities (no `plugin:*` command is reachable from the webview).

## Decisions

- The Human's rule is implemented as the two layers above, and the limit is stated: the command-level check sees the top-level frame; the frame-level protection is the exact-origin ACL.
- The capabilities are no longer static files in `capabilities/`: they are templates added at launch. This is the one way to pin a port that is only known at launch; a build-time capability cannot name it.
- `ProxyOrigin` is read at call time from managed state, not captured by the closure at startup.
- Residual, written in ADR-0013: any page served at the proxy origin itself (the daemon's own UI, anything the daemon serves) is the app and is trusted; the guard does not separate pages within one origin.

## Acceptance criteria

- AC1: for each of `http://127.0.0.1:<proxy>/x` accepted; another port, another host (`localhost`, `127.0.0.2`, `[::1]`), another scheme (`https`, `tauri`), userinfo (`http://u@127.0.0.1:<port>/`), a URL with no host and an unreadable URL are refused with a clear error and no side effect.
- AC2: every command in the handler list is refused from a webview whose URL is not the proxy origin and runs from one that is, except names in `EXEMPT` (none).
- AC3: a webview with no `ProxyOrigin` managed is refused (fails closed).
- AC4: the capability templates carry exactly the previous permission lists, window scopes and `local: false`, with `remote.urls == ["__PROXY_ORIGIN__/*"]` and no wildcard port; the runtime JSON built from them parses as capabilities and its URL pattern matches `http://127.0.0.1:<port>/anything` and nothing on another port, host, scheme or with userinfo.
- AC5: a new command in `generate_handler!` is guarded without any other edit; `EXEMPT` entries need a reason and must exist in the handler; a command in the allowlist that is missing from the handler or from `build.rs` fails the drift test.
- AC6: ADR-0013's task-window amendment says "proxy origin only (enforced in commands and in the ACL)" and states the residual.

## Test scenarios

`origin-table` (AC1, a Rust table test over the pure matcher), `guard-refuses-every-command-from-another-origin` and `guard-allows-every-command-from-the-proxy-origin` (AC2: parsed from the `generate_handler!` text, run through the guard in the mock runtime), `guard-fails-closed-without-proxy-origin-state` (AC3), `capability-template-url-pattern-is-exact` (AC4: the Tauri URL pattern from the built capability JSON tested against the same table), `capability-templates-keep-the-allowlists` (the existing exact-list tests, now reading the templates), `exempt-list-needs-a-reason-and-must-exist`, `every-handler-command-has-an-allowlist-entry-and-a-build-rs-entry` (AC5). Mutations (each must fail a named test): drop the guard from the handler; compare host only; compare scheme and host but not the port; allow userinfo; read the origin once at startup; put the wildcard port back in a template; exempt a command with no reason.

## Progress

- [x] spec (this file, first commit)
- [x] `ipc_guard.rs`: matcher, state, pure verdict, wrapper, runtime capabilities
- [x] templates and the capability tests
- [x] drift and table tests, mutations
- [x] ADR-0013 amendment text
- [x] two-axis review (HIGH-RISK), findings below
- [ ] Supervisor second read, merge

## Validation

- Tests: `cargo test` (266 passed, default features and `--features audit-guard`, real macOS TMPDIR), `cargo clippy --all-targets -- -D warnings` for both feature sets, `go test ./internal/hygiene/...`. `ipc_guard` and `capability_` tests: 30.
- Mutations (each killed by a named test): guard removed from the handler, `verdict` always Ok, fail-open on a missing origin, fail-open on an unreadable URL, a fixed origin instead of call-time, a different port/host/scheme/userinfo accepted, `install` not adding the capabilities, `install` commented out in `run()`, the capability URL widened back to `127.0.0.1:*`, a command registered without a grant or a `build.rs` entry, an exemption without a reason.
- Review (code-review skill, two fresh-context agents). Standards: no hard violations; fixed the dead `let _ = parsed`, two tautological asserts, the duplicate exempt test, added a refusal log (command name only). Spec/attack: no bypass of the exact-origin rule found (URL normalisation, subframes, ordering, fail-open paths, races tried). Fixed: `catch_unwind` removed (a caught panic left the authority lock poisoned), a pin that `lib.rs` has exactly one `invoke_handler`, ADR overclaim about `core:*` tests corrected, stale comments.
- Not fixed here, follow-ups: `on_navigation` (main and task windows) hands any non-proxy URL to the OS opener without a scheme check (pre-existing; `open_external` restricts to http/https); the main window lacks `on_new_window` deny; `on_navigation` compares origins without the userinfo rule (self-lockout only).
- Not verified: a real launch of the app (mock runtime with the real compiled ACL only); Linux/Windows runtime behaviour.
