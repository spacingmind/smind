# GitHub token login on a remote daemon (ADR-0039 §8, research item G12)

Size: **M (5)** — scope 2 (daemon + web + a Docker line), PRs 1 (two PRs: daemon, web; built in parallel against the wire below), risk 2 (a credential crosses the wire and
a subprocess), decisions 0 (H4 and ADR §8 settle it; one verification choice is listed below), verification 1 (UI screenshots; a real `gh` against a throwaway config dir).
One Lead; every PR non-stacked off `develop`.

Spec: [ADR-0039 §8 and §11](../../decisions/0039-git-pr-state.md) (the contract, accepted 2026-10-22 with H4). Status: **plan written 2026-10-23; implementation not started.**

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

## Human decision needed (only this one; everything else is §8 as accepted)

- **Success-path verification with a real token.** A bogus token proves the rejection path against real GitHub, but accepting a *valid* token can only be checked with a real one. Options:
  (1) the Lead uses `gh auth token` from the user's own login, **only inside a throwaway `GH_CONFIG_DIR`**, for one validation call, then deletes the directory; (2) the user supplies a
  throwaway fine-grained token; (3) the success path stays stub-only and is reported as unverified. **Recommendation: (3) until the Human chooses (1) or (2)**; it changes nothing else.

## Out of scope

Device flow / a smind OAuth app; storing the token in smind; GitHub Enterprise beyond the `host` field; the mobile app; per-workspace accounts.

## Implementation notes

(none yet)

## Progress

- 2026-10-23: plan written from ADR-0039 §8/§11 and H4; `gh auth status --json hosts` confirmed available in gh 2.101 (fields: host, active, login, scopes, state, tokenSource, gitProtocol).
  Nothing implemented.

## Validation

How each AC gets satisfied is recorded here per PR as it lands.
