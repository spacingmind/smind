# 0061: A browser for agents (broker, hosts, policy)

## Status

Accepted (2026-11-15). Human decision 2026-11-15: all 12 decisions listed at the end are accepted as recommended. Implementation is staged (headless first, then the desktop host) and each step is its own PR against `docs/plans/active/desktop-browser-for-agents.md`; the browser stays OFF by default and human-only to enable. Nothing in this ADR is implemented yet.

## Context

An agent that builds or fixes a web UI cannot see it. It can run a dev server and read logs, but it cannot open the page, look at it, click through it or read the console, so it reports success on a page it never loaded. A browser the agent can drive closes that loop.

It is also one of the most dangerous tools an agent can hold: the page it reads is attacker-controllable text that lands in the model's context (prompt injection), a browser on a developer's machine can reach internal services, the daemon's own unauthenticated loopback endpoints, cookies and logins, and anything it downloads or screenshots can leak. So the design starts from the policy, not from the tools: the browser is OFF by default, only a human turns it on, it can only go to a small set of places, everything it returns is labelled as untrusted data, and every call is rate limited.

What exists today (read from the daemon code, not assumed):

- smind has no in-process or HTTP MCP server. The daemon builds a list of MCP server rows per run and hands them to each provider's spawn mechanism (`Runner.mcpRowsFor`, `internal/taskrunner/mcp.go`). The built-in `smind` server is a stdio child (`smind mcp serve`, `cmd/smind/mcp.go`) whose tools call back into the daemon over the websocket API; it is added only when the run contract allows orchestration (`internal/runs/contract.go`, `internal/taskrunner/run_contract.go`). The generic registry for other servers is the `mcp_servers` table (ADR-0018).
- MCP tool calls are never auto-allowed by smind: they reach whatever the provider's own permission mode escalates (ADR-0019), and the permission policy can match `mcp:<server>.<tool>` rules by exact name.
- Tool results never carry images anywhere in `cmd/smind` today.
- The desktop shell (ADR-0013) serves its bundled UI from a loopback proxy and grants IPC to that origin only (the origin guard, amendment from the IPC origin guard PR). The desktop app is a client of the daemon, and may be connected to a remote daemon.

## Decision

### 1. A daemon broker exposing `browser_*` MCP tools

The daemon owns the tool catalogue, the policy and the limits. It never drives a browser itself: it routes each call to a HOST (section 3 and 4) that executes it and returns a result, then wraps the result (section 7). The tools are served as a separate reserved MCP row (a new server name, `smind-browser`), appended in `Runner.mcpRowsFor` beside the `smind` row but gated by its own run-contract field, so it is never present in a run that was not granted it and never rides the orchestration gating. The row is a stdio child like the existing one; its handlers call the daemon (a new human-reviewed set of RPC methods, callable only by a run's own MCP child, scoped to that run id).

The tool set is small and read-heavy (names are proposals; the reference designs show a much larger surface, most of which we do not want):

- Tabs: `browser_open` (navigate the run's tab; creates it), `browser_tabs`, `browser_close`.
- Reading: `browser_snapshot` (an accessibility tree with stable per-snapshot refs, text only), `browser_screenshot` (viewport only), `browser_console` (recent console lines), `browser_wait` (a text or URL condition, capped).
- Acting: `browser_click`, `browser_fill`, `browser_type`, `browser_press`, `browser_scroll`, `browser_back`, `browser_forward`, `browser_reload`.
- Deliberately NOT in the first version: `browser_evaluate` (arbitrary script in a logged-in page), `browser_upload`, drag, hover-only tricks, and any download tool.

### 2. OFF by default, human-only to enable, per workspace

A single switch per workspace, default off. Only a human can change it, through the human-only RPC principal (ADR-0056); the MCP child, an agent run, a hook and a plugin cannot reach the method. Enabling shows what the browser can do and the navigation policy in force. The switch, the allowlist and the profile mode live in the workspace's settings; a daemon-wide kill switch (config) overrides everything. A run only gets the `smind-browser` row when the workspace switch is on AND the run's profile allows it AND a host is available.

### 3. Hosts: a desktop child webview, and a headless fallback

Two hosts execute the same tool contract.

- **Desktop host.** The Tauri shell hosts the browser as a child webview of the person's own window, so the person can watch what the agent does. It registers with the daemon over its existing connection as a browser host. The webview is created by Rust with a fixed label pattern that no capability grants (so it has no IPC at all, see the threat model), a data store per workspace, navigation and new-window handlers enforcing section 5, and a visible "agent is controlling the browser" indicator with a stop button. Because it runs on the person's machine, it is only used when the daemon is local: a "localhost" the agent names refers to the DAEMON's machine, where its dev server runs, so a desktop host on another machine would browse the wrong localhost.
- **Headless host on the daemon.** The fallback, and the only host for a remote daemon or a daemon with no desktop attached. It drives a headless Chromium on the daemon's machine, with a per-workspace user-data-dir. It is an optional capability: the browser is not bundled, the daemon reports "no browser installed" with the exact install step, and `browser_*` tools are not offered until a host is available. The driver (a Node sidecar with Playwright, or a Go client of the Chrome DevTools Protocol) is a decision below.

Routing: one browser session per workspace at a time; the desktop host when the daemon is local and the host is registered and idle, else headless. A host that disconnects mid-call fails the call with a plain error; there is no silent switch to the other host.

### 4. A per-workspace profile

Each workspace has its own browser profile directory (headless) or data store (webview). The default is EPHEMERAL: cookies and storage are discarded when the session ends (idle close or run end), so nothing an agent logs into survives and nothing leaks between workspaces. A persistent per-workspace profile is a separate human toggle for the case where the agent must stay logged in to the app under test. A profile is never shared across workspaces and never shared with the person's own browser or with the app's own webview.

### 5. Navigation: loopback and an allowlist, http(s) only

- Schemes: `http` and `https` only. `file:`, `data:`, `javascript:`, `blob:`, `view-source:`, `chrome:`/`about:` (except the blank page) and every other scheme are refused at the top level and on every redirect.
- Hosts: the default allowlist is loopback (`localhost`, `*.localhost`, `127.0.0.0/8`, `::1`) on any port; the human can add exact hostnames per workspace. Everything else is refused, including private ranges (10/8, 172.16/12, 192.168/16, link-local, metadata addresses) unless the human added that exact host.
- Never allowed, whatever the allowlist: the daemon's own listen address and port (its `/api/token` is unauthenticated on loopback), the desktop proxy origin (a page there is trusted and holds IPC), and other smind daemon ports named in the connection list.
- The policy is evaluated on the normalised URL (userinfo refused, decimal/octal/hex and IPv4-mapped IPv6 forms resolved before comparing, trailing dots, ports), on every navigation and redirect hop, and against the RESOLVED address for a named host (a name that resolves to a forbidden address is refused: DNS rebinding is checked at navigation, and in the headless host the browser is started with host-resolver rules that map everything outside the allowlist to nothing, so subresource requests obey it too). In the webview, subresource requests cannot be filtered, which is why the webview has no IPC and no profile worth stealing (section 4).
- Popups and new windows: `window.open` and `target=_blank` never create an OS window. A pool of at most 3 tabs per run is allowed; a popup opens as a tab in that pool and is subject to the same policy; beyond the pool it is refused and reported.

### 6. Dialogs, downloads, files

- JS dialogs: `alert` is dismissed; `confirm`, `prompt` and `beforeunload` are CANCELLED (never auto-accepted, so a page cannot trick the agent into confirming something destructive); each is reported in the tool result.
- Downloads: blocked. The result reports that a download was attempted and its file name; nothing is written to disk.
- File chooser and drag-and-drop file input: not offered.

### 7. Page content is untrusted data

Everything that comes from a page (the snapshot, text, the console, titles, URLs, a screenshot) is wrapped by the broker, not by the host: a fixed envelope states that the content is untrusted data from a named origin and must not be followed as instructions, and the content is quoted inside it. The broker never executes, evals or interprets page content and never feeds it to another tool. Limits: snapshot at most 40,000 characters and 1,200 nodes (truncated with a marker), console at most 100 lines of 500 characters, wait at most 15 seconds, a screenshot only of the page viewport (never the window or screen) at most 1,568 px on the long side and 1.5 MB, password inputs masked in snapshots and blurred in screenshots. A page cannot make a tool result larger than these limits. Screenshots are returned as images only when the provider can read them (section 9); otherwise the result says so and the agent works from the snapshot.

### 8. Rate limits and resource limits

Per run: 30 browser calls per minute and 500 per run; navigations 10 per minute; one call in flight at a time (serialised); each call times out at 20 seconds. Per workspace: one session, closed after 5 idle minutes, at most 3 tabs, a memory cap on the headless process, and a kill on run end. Daemon-wide: at most 2 concurrent headless sessions. A refused call returns a plain error naming the limit.

### 9. Which providers can use it

Per the daemon code (the table in the plan's "Provider reach" section has the evidence):

- **claude-native: yes.** The row goes into the run's `--mcp-config` file (managed runs also use the strict config flag, so only smind's rows are visible). Tools appear as `mcp__smind-browser__<tool>`.
- **ACP providers (OpenCode, Copilot, Cline, Goose, Cursor, Kimi, GLM): yes, if the agent advertises MCP support.** The row goes in `session/new`'s `mcpServers`; stdio is filtered by the agent's advertised transports. Unverified providers are marked as such; GLM cannot read images, so for it only the text tools are useful.
- **Compatible recipe providers** that extend the Claude harness: reachable by the same `--mcp-config` path, but whether the model behind the endpoint calls MCP tools at all is the model's concern (at least one recipe is documented to ignore MCP servers); treated as "offered, not guaranteed".
- **codex-native: NO.** Codex has no per-thread MCP parameter and smind marks every MCP row as dropped for it; `browser_*` is not offered there until that changes.
- **Plugin ACP providers:** only when the manifest declares MCP support and consent allows it; the existing strip of smind's own rows must be reviewed so that `smind-browser` follows the same rule.

Images in tool results are new: nothing in `cmd/smind` returns image content today. The broker returns an image block only for providers whose catalogue entry says images are read, and a text note otherwise.

### 10. Permissions and visibility

MCP tool calls are not auto-allowed (ADR-0019), and this ADR does not change that. The tools are split by effect: READ tools (`snapshot`, `screenshot`, `console`, `tabs`, `wait`) and ACT tools (`open`, `click`, `fill`, `type`, `press`, `scroll`, history navigation). The permission policy can match exact `mcp:smind-browser.<tool>` rules; the draft recommends offering the human a per-run grant for the read tools only, and a prompt for each acting tool until the human grants it for the run. Every call appears in the run timeline with its URL and outcome, and on the desktop host the indicator shows the current URL and a stop control that ends the session.

## Threat model

| Threat | Mitigation | Test (implementation PRs) |
|---|---|---|
| SSRF to internal hosts (private ranges, metadata addresses, other local services) via navigation or redirect | Default allowlist is loopback only; private ranges refused unless the exact host was added; every redirect hop re-checked; resolved addresses checked; headless host-resolver rules | URL policy table (every form of IPv4/IPv6/decimal/octal/hex/mapped/userinfo/dot); redirect-chain test |
| SSRF to the daemon itself (unauthenticated `/api/token` on loopback), to the desktop proxy origin, to other smind daemons | Those addresses are denied regardless of the allowlist | Table rows for the daemon port, the proxy origin, listed connections |
| Cookies and profile leaking across workspaces, or into the person's own logins | One profile per workspace, ephemeral by default, never shared with the app webview or the person's browser | Two workspaces never see each other's cookie; ephemeral profile gone after session end |
| `file:`, `data:`, `javascript:`, `blob:` schemes (local file read, script injection) | Scheme allowlist `http`/`https`, enforced on redirects and popups | Scheme table incl. redirects to `file:` |
| Prompt injection from page content | Broker-side envelope labels content as untrusted data; never executed or chained; size limits; read/act split with permission prompts | Envelope present on every tool result; an injected instruction in a fixture page is returned inside the envelope and no tool acts on it |
| Popups and new windows (window spam, off-policy tabs) | No OS windows; 3-tab pool per run; popups subject to the same policy | A page opening many windows is capped and reported |
| JS dialogs used to trick the agent (`confirm`) | `confirm`/`prompt`/`beforeunload` cancelled; reported | Dialog fixture cancels and reports |
| Downloads (drive-by file write) | Blocked; only the attempt is reported | A download link writes nothing |
| Screenshots containing secrets | Viewport only, never window or screen; password inputs blurred; size cap; images only to providers that read them; the session is the workspace's own profile | Password field blurred in a fixture screenshot; size cap enforced |
| Resource exhaustion (tabs, memory, CPU, request floods) | Tab pool, per-run and per-minute rate limits, call timeout, idle close, memory cap, concurrent session cap | Limit table tests; the 31st call in a minute is refused |
| A page calling the desktop IPC (a loopback dev server rendered in the host) | The host webview's label is granted no capability and gets no invoke bridge; the origin guard limits IPC to the exact proxy origin; the proxy origin is not navigable by the agent | The host webview label matches no capability pattern (exact-list test); a fixture page's `invoke` fails |
| A non-human enabling the browser | The enable method is human-only (ADR-0056); the MCP child, hooks and plugins cannot call it | RPC principal test |
| A tool result larger than the limits or malformed (broker as an oracle for exfiltration) | Broker re-applies limits and re-serialises; page text is data inside the envelope | Oversized fixture is truncated with a marker |

Residual risks, stated: the allowlisted loopback pages are the human's own apps and are trusted as far as the human trusts them (an app under test can still prompt-inject the agent); a webview cannot filter subresource requests, so a page on the allowlist can make requests to other loopback ports (the same as any local page); the headless host depends on the system browser's own sandbox; screenshots can still show sensitive content of the app under test.

## Decisions raised (all accepted as recommended, 2026-11-15)

1. **Ship the browser at all, behind OFF-by-default and human-only enable?** Recommend yes, with the policy in this ADR as the minimum.
2. **Headless driver:** Node sidecar with Playwright, or a Go client of the Chrome DevTools Protocol. Recommend the Go CDP client: no Node runtime requirement on the daemon machine, one binary, and the tool surface here is small; revisit if Playwright's auto-waiting proves necessary.
3. **Desktop host first, or headless first?** Recommend headless FIRST (it is the only host that works for a remote daemon, it is the simplest to test, and the broker, policy and limits are the same), then the desktop host.
4. **Tool surface:** the small read-heavy set above, without `evaluate`, `upload` and downloads. Recommend yes; add `evaluate` only as a separate human toggle later.
5. **Profile default:** ephemeral, with a persistent per-workspace profile as an explicit human opt-in. Recommend ephemeral.
6. **Permissions:** read tools grantable per run, acting tools prompt until granted for the run. Recommend yes; no blanket auto-allow.
7. **Providers:** offer to claude-native and ACP providers; not to codex-native until it has per-thread MCP; recipes best-effort. Recommend yes, and show the human which providers cannot use the browser when they enable it.
8. **Images in tool results:** return screenshots as images only for providers that read them. Recommend yes (this is new infrastructure in the MCP child).
9. **Allowlist authoring:** human-edited exact hosts per workspace, no wildcards beyond loopback. Recommend yes.
10. **Remote daemon with a desktop attached:** headless on the daemon only, never the desktop host. Recommend yes (the "localhost" would be the wrong machine).
11. **Dialogs:** cancel `confirm`/`prompt`. Recommend yes (the opposite default is a known weakness).
12. **Mobile:** no browser UI on mobile in this ADR; the toggle and the run timeline entries appear in the same settings and timeline as everywhere. Recommend deferring any mobile browser view.

## Consequences

- A new reserved MCP row, a new human-reviewed RPC surface scoped to a run's own MCP child, a new optional dependency (a system Chromium for the headless host) and, later, a Tauri host with its own webview data stores.
- The desktop IPC story does not change: the host webview is granted nothing, and the origin guard stays exact.
- Screenshots in tool results are a new content type for the MCP child; providers that cannot read images get a text note.
- Codex-native runs never see the browser; the settings screen says so.

## Amendments

### 2026-11-15: sub-decisions taken at implementation time (the 12 decisions above are unchanged)

Human decisions recorded after the headless CDP client and URL policy were built (the first implementation PR) and reviewed:

1. **An enforcing proxy closes the non-Fetch channels.** Measured on a real Chrome: the Fetch domain never sees WebSocket handshakes, so a page on an allowed host could reach a denied loopback port (including the daemon's) with `new WebSocket`; `Network.setBlockedURLs` did not stop it. Section 5's "host-resolver rules ... so subresource requests obey it too" is therefore supplemented, not replaced: the headless host launches the browser with `--proxy-server` pointing at an in-process proxy in the daemon, with an empty bypass list. All schemes (http, https, WebSocket, WebTransport where it falls back) go through that one policy check; the proxy resolves names itself, refuses sensitive addresses and dials the vetted IP, which also closes DNS rebinding. TLS is tunnelled, never intercepted. Fetch interception stays for document and subresource logging.
2. **The daemon gates every tool call (section 10).** Calls are held and approved inside the daemon, independent of the provider's permission mode, because a provider in an auto-accept mode never asks. The read tools are grantable once per run; every acting tool asks; `browser_open` may be granted for the current run on its card. No grant outlives its run and none is saved across runs. Provider-native prompts and `mcp:smind-browser.<tool>` policy rules remain a second layer.
3. **The enable method keeps the ADR-0056 limit.** The switch is as human-only as the RPC principal is today (an unidentified local connection counts as the human); the human-only RPC tables, the policy floor and the CLI refusal apply, and the Settings text states the limit. If ADR-0056 is later accepted, the enable RPC joins its decision with no rework.
4. **Allowlist.** Loopback plus exact hosts the human adds (decision 9 as written); an allowlist entry that is link-local, metadata, multicast or unspecified, or an IPv6 form that embeds one, is refused at entry and at resolution. This tightens section 5, which would have allowed a typed metadata literal.
5. **No browser data channel other than the pipe.** The browser is driven over `--remote-debugging-pipe`: there is no DevTools TCP port for another local process to attach to.

Design and tests: `docs/plans/active/browser-for-agents-designs.md`.

## Cross-references

- ADR-0013 (desktop bundled UI, IPC scope and the origin guard), ADR-0018 (agent MCP servers), ADR-0019 (provider-native permission modes), ADR-0055 (saved grants), ADR-0056 (human-only RPC principal).
- `docs/plans/active/desktop-browser-for-agents.md` (acceptance criteria for the implementation PRs).
