# GitHub token login on a remote daemon (ADR-0039 §8, research item G12)

Size: **M (5)** — scope 2 (daemon + web + a Docker line), PRs 1 (two PRs: daemon, web; built in parallel against the wire below), risk 2 (a credential crosses the wire and
a subprocess), decisions 0 (H4 and ADR §8 settle it; one verification choice is listed below), verification 1 (UI screenshots; a real `gh` against a throwaway config dir).
One Lead; every PR non-stacked off `develop`.

Spec: [ADR-0039 §8 and §11](../../decisions/0039-git-pr-state.md) (the contract, accepted 2026-10-22 with H4). Status: **completed 2026-10-23** (daemon #547, web #548; every acceptance criterion is confirmed in Validation, with the success path against real GitHub unverified by decision).

## Decisions (fixed)

- **H4 (Human, 2026-10-22):** the user **pastes a token, sent over the E2EE channel**, and the daemon runs `gh auth login --with-token`. The token never appears in logs, events, argv, env
  or errors; it goes on stdin. Device flow with smind's own OAuth app is a later ADR, only if built.
- **O9 (accepted):** push and fetch to **https** remotes use `-c credential.helper='!gh auth git-credential'` **per invocation**, only when the forge is authenticated; the daemon user's
  global gitconfig is never edited; ssh remotes are untouched.
- **What "secure channel" means** (decided here, implementing §8's `channel` tag): `loopback` (the `/ws` request comes from a loopback address: a local client, an ssh tunnel, a
  TLS-terminating reverse proxy on the same host), `tls` (the request arrived over TLS), `e2ee` (a relay-served session, ADR-0035); everything else is `plain` and gets
  `insecure_channel`. `forge.setToken`/`forge.logout` are allowed on the first three only.

## Wire (frozen; the daemon and web PRs build to it)

- `forge.status {refresh?: bool}` → `{provider:"github", canSetToken: bool, cli:{installed, version}, hosts:[{host, authenticated, login, scopes:[string], protocol, source:"keyring"|"file"|"env"|"none"}]}`.
  Built from `gh auth status --json hosts` (never `--show-token`; the field `tokenSource` is mapped to `keyring | file | env`, a path is never returned); cached 60 s, single-flight;
  `canSetToken` is whether **this connection's** channel is secure (loopback, TLS or E2EE) and the caller is not run-scoped: it lets the UI disable the field without a probe call.
  `refresh` is bounded by a 5 s floor and 12 per hour. Topic **`forge.status`** `{status}` is published when the status changes (after setToken/logout and on a poll that sees a change), so a
  second tab updates. `daemon.info.forge` stays `{provider, cli}` (a `LookPath`, no spawn).
- `forge.setToken {host?: string, token: string}` → `{status}`. `host` defaults to `origin`'s host and must match a hostname regex; `token` is `[\x21-\x7e]{1,255}`. The daemon runs
  `gh auth login --hostname <host> --with-token --git-protocol https` with the token **on stdin and nowhere else**, then re-reads the status.
- `forge.logout {host}` → `{status}`: `gh auth logout --hostname <host>`.
- Errors (code in `rpcError.code`, fixed sentence in `message`, scrubbed detail in `data.detail`): `insecure_channel`, `token_invalid` (gh rejected the token), `token_scopes` (a classic token
  without `repo`/`read:org`; fine-grained tokens are not scope-checked), `forge_cli_missing`, `forge_timeout`, `forge_error`. Run-scoped (agent) callers are refused for both mutating RPCs; no
  MCP tool exists for either.
- Channel tag: the transport tags each connection `loopback | tls | e2ee | plain`; `requestContext` carries it. `/ws` derives it from the request (`RemoteAddr`, `r.TLS`); `ServeTransport`/
  `ServeDevice` are `e2ee`.

## Acceptance criteria

### Daemon

- AC1. `requestContext` carries a channel tag; `/ws` from a loopback address is `loopback`, a TLS request is `tls`, a non-loopback plain request is `plain`, a relay session is `e2ee`.
- AC2. `forge.status` returns the shape above from a stubbed `gh auth status --json hosts` (authenticated, not authenticated, several hosts, `gh` missing), maps `tokenSource` without a path,
  is cached 60 s and single-flight, and `refresh` respects its floor/hourly cap; the `forge.status` topic fires only when the status changed.
- AC3. `forge.setToken` runs `gh auth login … --with-token` with the token on **stdin only**: the argv and the environment passed to the subprocess contain no part of the token, and a failing
  login maps to `token_invalid`/`token_scopes`/`forge_timeout` with a scrubbed detail.
- AC4. `forge.setToken`/`forge.logout` on a `plain` channel return `insecure_channel` **before** reading the token or spawning anything; run-scoped callers are refused; no MCP tool is registered.
- AC5. **Never logged.** A sentinel token passed to `forge.setToken` appears in none of: daemon log output, the event bus (including `forge.status`), `request_log`, `run_events`, `run.logs`, any
  RPC result, any error message or `data.detail`, the subprocess argv/env, and the learning transcripts; success and every failure path are covered.
- AC6. Git over https: push and fetch calls built by the daemon for a workspace whose `origin` is an https URL and whose forge is authenticated carry the per-invocation credential helper
  override; an ssh origin or an unauthenticated forge gets none; `~/.gitconfig` is untouched; a failed credential surfaces as the existing `push_auth` code.
- AC7. After a successful `setToken`, `forge.status` is authenticated, the poller's host suspension (ADR-0039 §4 rule 4) is lifted at once, and `actions.createPr`'s `forge_unauthenticated`
  blocker clears on the next `task.git`.
- AC8. The Docker images install `gh`; the image test (existing `docker` CI lane or a build-time `gh --version` check) proves it.

### Web

- AC9. **Settings → GitHub** is a new section (registry entry, icon from `lib/icons.ts`): it shows the daemon host's `gh` version, each host's login, scopes and protocol, "Not signed in" with
  the next step, or "`gh` isn't installed on the daemon host"; it follows the `forge.status` topic live (TanStack query + event bridge, ADR-0032).
- AC10. The **token field** is masked, `autocomplete="off"`, never kept in zustand, `localStorage`, a query cache or the DOM after submit, cleared on submit and on unmount; submit calls
  `forge.setToken` through a mutation and clears the field whatever the outcome; typed errors render as one sentence (`token_invalid`, `token_scopes`, `insecure_channel`).
- AC11. On an insecure connection the field is disabled with a sentence ("This connection isn't encrypted, so smind won't send a token over it. Use the relay, an ssh tunnel or TLS."), decided
  from the daemon's `insecure_channel` answer to a probe-free capability read (`forge.status` carries `canSetToken: boolean`); a local daemon also offers "Run `gh auth login` on this machine".
- AC12. Every `forge_cli_missing` / `forge_unauthenticated` error and disabled-action sentence in the app (Create PR, the PR picker, the PR chip's stale state) links to Settings → GitHub.
- AC13. **Visuals.** Light and dark screenshots of the card in each state (signed in, signed out, `gh` missing, insecure channel, error, success) and the transition frames (field clears on submit,
  status changes live); no mid-animation glitch.

### Cross-cutting

- AC14. `task test`, `task lint` and `cd web && bunx tsc -b packages/ui` pass on every PR and are recorded in its body. Live checks use an isolated daemon and a throwaway `GH_CONFIG_DIR`;
  nothing writes to GitHub.

## Test scenarios (names are the contract)

**daemon** (`internal/wsapi`, `internal/ghctx`, `internal/workspace`): `TestChannel_Tagging`, `TestForgeStatus_Shapes`, `TestForgeStatus_TokenSourceMapped`, `TestForgeStatus_CachedAndSingleFlight`,
`TestForgeStatus_RefreshFloor`, `TestForgeStatusTopic_OnlyOnChange`, `TestForgeSetToken_StdinOnly`, `TestForgeSetToken_ErrorsMapped`, `TestForgeSetToken_InsecureChannelBeforeRead`,
`TestForgeSetToken_RunScopedRefused`, `TestForgeNoMcpTool`, `TestForgeLogout`, `TestForgeToken_NeverLogged` (sentinel over every sink), `TestForgeSetToken_LiftsHostSuspension`,
`TestGitNetwork_CredentialHelperOnlyForHttpsAndAuth`, `TestGitNetwork_GlobalConfigUntouched`, `TestDockerfile_InstallsGh`.

**web** (`web/packages/ui`): `github-section.test.tsx` (`it shows each status state`, `it follows forge.status events`, `it disables the field on an insecure channel`, `it clears the token on submit and
unmount`, `it never stores the token`, `it shows a typed error in one sentence`), `forge-error-link.test.tsx` (`it links forge_unauthenticated to Settings → GitHub`).

**live** (isolated daemon, throwaway `GH_CONFIG_DIR` — never the user's real one): a **bogus** token → real `gh` rejects it → `token_invalid` and the sentinel appears nowhere (one read-only
validation call to GitHub); `forge.status` before/after; `plain` refusal over a non-loopback listener if reproducible locally; the settings card in light and dark.

## PR split (non-stacked, built in parallel against the frozen wire)

| PR | Content | Peers |
|---|---|---|
| A | **Daemon**: channel tag, `forge.status` + topic, `forge.setToken`/`forge.logout`, per-invocation credential helper for https, host-suspension lift, Dockerfile `gh`, the sentinel test | peer-sonnet |
| B | **Web**: Settings → GitHub section, token field, status live, insecure-channel state, error links; screenshots | peer-sonnet (GLM when its quota allows, step-by-step briefs) |

## Human decision needed (only this one; everything else is §8 as accepted) — resolved 2026-10-23: option (3)

- **Success-path verification with a real token.** A bogus token proves the rejection path against real GitHub, but accepting a *valid* token can only be checked with a real one. Options:
  (1) the Lead uses `gh auth token` from the user's own login, **only inside a throwaway `GH_CONFIG_DIR`**, for one validation call, then deletes the directory; (2) the user supplies a
  throwaway fine-grained token; (3) the success path stays stub-only and is reported as unverified. **Recommendation: (3) until the Human chooses (1) or (2)**; it changes nothing else.

## Out of scope

Device flow / a smind OAuth app; storing the token in smind; GitHub Enterprise beyond the `host` field; the mobile app; per-workspace accounts.

## Implementation notes

Decided while implementing (2026-10-23):

- `forge.setToken` without `host` defaults to `github.com` (the wire has no workspace parameter, so no origin to read). `forge.setToken`/`forge.logout` return `{status:{provider, canSetToken, cli, hosts}}`; the `forge.status` **topic** payload is `{status:{provider, cli, hosts}}` without `canSetToken`, which is a property of one connection's channel: the web keeps the value the RPC answered with when an event arrives (a locked field after any event was found in review and fixed).
- A `forge.status {refresh}` over the 5 s floor or the 12/hour cap returns the cached status without an error. A malformed token (empty, space, over 255 bytes, non-ASCII) is `token_invalid` and never reaches `gh`; malformed params or a bad `host` are `invalid_params` with a fixed sentence that never quotes the input.
- A loopback peer sending `X-Forwarded-Proto: http` is tagged `plain` (stricter than the plan: a same-host proxy in front of a plain listener must not count as secure).
- The network-git hook is `NetworkGitConfig func(ctx, dir, remote) []string`, installed through `workspace.SetNetworkGitConfig` and gitstate's `Options.NetworkGit`; it reads the remote with `git remote get-url`, so `url.insteadOf` applies, and a global https→ssh rewrite correctly yields no helper. `Provider.Status(ctx)` returns all hosts; `forge.AuthStatus` is `{CLI, Hosts}`.
- The Docker image pins the `gh` release binary with a sha256 check (the distro's `gh` is too old for `auth status --json`).
- No run-scoped identity exists yet, so the agent refusal is enforced and tested at handler level only; an agent holding the daemon token could call `forge.setToken` over a raw `/ws` connection like any RPC (there is no MCP tool, and an agent on the host can run `gh auth login` itself). `GH_TOKEN`/`GITHUB_TOKEN` in the daemon's own environment makes `gh auth login` refuse (`forge_error`). Go strings cannot be zeroed, so the token lives in daemon memory until collected.


## Progress

- 2026-10-23: daemon (#547) and web (#548) halves implemented, verified live together (bogus token against real `gh` in a throwaway config dir) and merged. Plan written from ADR-0039 §8/§11 and H4; `gh auth status --json hosts` confirmed available in gh 2.101 (fields: host, active, login, scopes, state, tokenSource, gitProtocol).


## Validation

How each AC was satisfied (daemon #547; web #548). The two branches were merged in a scratch worktree for the live run.

- **Daemon ACs 1–8:** every name in the plan's daemon group exists and passes. `TestForgeToken_NeverLogged` runs the real `ghctx` subprocess against a fake `gh` that echoes the token to stderr on failure and scans log output, stdout/stderr, the event bus, every SQLite table (`request_log` and `run_events` stay at 0 rows), every RPC result and `detail`, and the subprocess argv and env: the token appears only on gh's stdin. The Peer proved the test by mutation (dropped redaction, an added `slog.Info` of the token, a skipped poller lift each failed the matching test). The Dockerfile change was built for real: `gh --version` in the image printed 2.101.0.
- **Live, real `gh` 2.101, throwaway `GH_CONFIG_DIR` (never `~/.config/gh`), nothing written to GitHub:** `forge.status` on an empty config → no hosts; pasting a **bogus** token through the real Settings → GitHub card made real `gh` validate it against GitHub once → `HTTP 401: Bad credentials` → `token_invalid` shown as "GitHub rejected that token. Check that it is correct, current and for the right host."; the field was empty afterwards; the sentinel string was in none of: the page DOM, localStorage/sessionStorage, the daemon log, `smind.db` and its WAL, the gh config dir.
- **Live with a scripted `gh` (signed-in and logout states) and with `gh` missing from PATH:** the card showed "Signed in to github.com as octocat" with scopes, protocol and token source, the Log out confirm, and the signed-out card after logout; with `gh` missing it showed the warning and the disabled field. Light and dark screenshots of each state.
- **Unverified (by decision, recommendation (3) of this plan):** accepting a *valid* token against GitHub (stubs and the fake `gh` only), and a real non-loopback plain connection (`plain` is covered by httptest remote-address tests, not reproduced live because direct non-loopback sessions need the password flow). Neither was exercised with the Human's own credentials.
- **ACs 9–13 (web):** named web tests, the source-scan test that no token reaches storage/zustand/`console`/mutation variables, and the screenshots above. **AC14:** `task test`, `task lint` and tsc are recorded in each PR body.

### Acceptance-criteria coverage (2026-10-23 close-out)

| ACs | Where | Evidence |
|---|---|---|
| AC1–AC8 (channel tag, `forge.status`, stdin-only `setToken`, secure-channel and agent refusals, never logged, https credential helper, host-suspension lift, Docker `gh`) | #547 | the named daemon tests incl. `TestForgeToken_NeverLogged` (proved by mutation); live: real `gh` 2.101 in a throwaway `GH_CONFIG_DIR`, a bogus token → `token_invalid`, the sentinel in no DOM, storage, log, database or gh config; the image built with `gh --version` 2.101.0 |
| AC9–AC13 (Settings → GitHub card, token field handling, insecure state, error links, visuals) | #548 | `github-section`, `forge-error-link` and source-scan tests; light/dark screenshots of each state; a review fix so a `forge.status` event keeps the connection's `canSetToken` |
| AC14 | both | each PR body records `task test`/`task lint`/tsc; nothing written to GitHub |

**Decision resolved:** the success path was verified with stubs and a scripted `gh` only (option 3); using the Human's own token, even in a throwaway config dir, needs their explicit OK, so accepting a valid token against real GitHub stays **unverified** until they choose option (1) or (2). Other residuals: no run-scoped identity exists yet, so the agent refusal is enforced at handler level only; a real non-loopback `plain` connection was covered by tests, not reproduced live.
