# Per-provider launch env (CLAUDE_CONFIG_DIR / CODEX_HOME)

Size: L (7) — scope 2, PRs 1, risk 1, decisions 0, verification 2 (UI screenshots + an isolated-daemon real check).

## Goal

The desktop-managed daemon (started from Finder) has no `CLAUDE_CONFIG_DIR`, so a user whose working Claude login lives outside `~/.claude`
gets "Failed to authenticate". Add a per-provider launch-env setting: `providers.<id>.env`, restricted to an allowlist, applied to every launch of
that provider, editable live from Settings → Providers. Additive and reversible: an absent setting changes nothing.

## Design

- Config: `ProviderConfig.Env map[string]string` (`yaml:"env"`). One Go table `config.ProviderEnvKeys` maps provider id → allowed keys
  (`claude-native: CLAUDE_CONFIG_DIR`, `codex-native: CODEX_HOME`); a new key is a one-line change. `config.ValidateProviderEnv(provider, key, value)`:
  key must be allowed for that provider (typed `*EnvKeyError` naming the allowed keys), value must be an absolute path after expanding a leading `~/` against the
  daemon user's home, no NUL/control characters, no `..` elements after cleaning is NOT required (the value is cleaned with `filepath.Clean`).
  The directory need not exist. Enforced at load (`validateProviders`) and in `config.SetProviderEnv`.
  Only paths: no tokens, PATH, API keys.
- Store: `config.SetProviderEnv(provider, key, value)` goes through the same `mutate` path as `SetProviderDefaultModel` (comment-preserving, atomic);
  `value == ""` removes the key (and an emptied `env` map).
- Runner: `Runner.SetProviderEnv(provider, key, value)` / `ProviderEnv(provider)` (RWMutex, live). `WithProviderEnv` option seeds it from config in `cmd/smind/serve.go`.
  Config wins over the daemon's own environment: entries are appended after the inherited/taskEnv ones and the SDK / `exec.Cmd` env resolve duplicates last-wins
  (claude-native: `claudecode.WithEnv`, which the SDK appends to `os.Environ()`; codex: new `codex.WithEnv` -> `cmd.Env = append(os.Environ(), env...)`).
  Read per launch, so the next launch sees a change; a RUNNING run keeps the env it started with.
- Launch sites: `runClaudeNative`, `RunPromptIsolated` (isolated.go), the codex-native run and the codex model probe. **The learning probe/sandbox is
  NOT changed**: its launcher runs the CLI under `env -i` with only `sandbox.AllowlistEnv` (which passes the daemon-process `CLAUDE_CONFIG_DIR` through; the CLI needs the login to
  authenticate, measured in ADR-0037), so the Runner-level env never reaches it and the setting is deliberately not added to the launcher file: a sandboxed probe never silently takes the
  smind setting. Limitation: a probe for a user whose login is only in the setting (daemon env unset) will not authenticate; revisit if probing is productised.
- RPC: `provider.setEnv {provider, key, value|null}` (human only: run-scoped callers get `codeForbidden`, no MCP tool). Writes config, updates the Runner,
  publishes `provider.updated` with the new payload. Serialised by the same mutex as setDefaultModel.
- Payload (additive; `provider.list` entries and the `provider.updated` payload): `env` (configured key→path), `envDirExists` (configured key→bool),
  `envEffective` (key→`{path, source: "settings"|"environment"|"default", exists}`) for providers that have allowlisted keys; effective path = config, else the daemon's
  process env, else the CLI default (`~/.claude` / `~/.codex`).
- `provider.test` for claude-native/codex-native appends `config dir: <path> (from smind settings|the daemon's environment|default ~/.claude)` and the
  directory-exists fact to its detail. It only stats the directory; it never opens a file under it. `smind account test` already prints the detail.
- Web: Providers page row gets a quiet "Claude config directory" / "Codex home" field (text input, validation, Save / Clear, "directory does not exist" note),
  `useMutation` + the existing `provider.updated` invalidation (ADR-0032).

## Acceptance criteria and named tests

1. Config accepts allowlisted keys, rejects others / relative / NUL — `TestProviderEnvLoadAccepts`, `TestProviderEnvRejects` (config).
2. `~/` expansion — `TestProviderEnvExpandsHome`.
3. claude-native launch env carries the setting; setting beats the daemon env; absent setting changes nothing — `TestClaudeLaunchEnvFromProviderEnv`,
   `TestClaudeLaunchEnvSettingBeatsProcessEnv`, `TestClaudeLaunchEnvAbsentSettingUnchanged`; isolated runs — `TestIsolatedLaunchEnvFromProviderEnv`;
   codex — `TestCodexLaunchEnvFromProviderEnv`, `TestCodexEnvSettingBeatsProcessEnv` (codex).
4. Probe sandbox untouched — `TestProbeLaunchIgnoresProviderEnvSetting`.
5. RPC — `TestProviderSetEnvRefusesAgentCaller`, `TestProviderSetEnvValidatesKey`, `TestProviderSetEnvNullClears`, `TestProviderSetEnvPublishesUpdated`,
   `TestProviderSetEnvAppliesToNextLaunch` (wsapi/taskrunner).
6. `account test` names dir + source, prints no file contents — `TestProviderTestNamesConfigDirAndSource`, `TestProviderTestNeverReadsDirContents` (planted marker never appears).
7. Web — field states none / effective-from-env / set, validation error, missing-dir note, save, clear (vitest).
8. Mutation checks (revert -> red): config no longer wins over process env; allowlist bypass; sandbox probe takes the real dir.
9. Real check on an isolated daemon (own SMIND_HOME + port, CLAUDE_CONFIG_DIR unset): `smind account test claude-native` before / after `provider.setEnv`, and a
   fakeclaude run whose child env carries the dir. Screenshots light + dark, 1280 + 390: empty, effective-from-env, set, invalid, missing dir, saved, save transition.

## Decisions

- Allowlist table keyed by provider (one Go table); keys limited to the two config-dir variables.
- Config wins over the process env; empty setting = today's behaviour.
- Sandbox probe unchanged (see Design).
- Later ADR question (out of scope): mapping several Claude logins / config dirs to smind accounts for routing (per-account env).

## Progress

- [x] spec committed
- [x] backend (Lead)
- [x] web (Peer task 93, peer-sonnet; candidate aead335e..4ac014ab ACCEPTED: diff read against the contract, tests/tsc re-run by the Lead, screenshots viewed)
- [x] review, checks, evidence, PR
- Merged in #603 (`feat: per-provider launch env (CLAUDE_CONFIG_DIR / CODEX_HOME) in Settings -> Providers`). Plan closed in the 2026-10-25 plans hygiene pass.

## Validation

AC to PR map: AC1-AC9 all shipped in #603. Test names that changed from the spec while building (confirmed on `origin/develop`, 2026-10-25): `TestProviderSetEnvNullClears` and
`TestProviderSetEnvPublishesUpdated` became `TestProviderSetEnvSetAndNullClears` (which also asserts the `provider.updated` publish); `TestProviderSetEnvAppliesToNextLaunch` is
`TestProviderEnvAppliesToNextLaunch` (taskrunner); `TestCodexEnvSettingBeatsProcessEnv` is folded into `TestCodexLaunchEnvFromProviderEnv` (it sets a conflicting `CODEX_HOME` in the daemon env and asserts the setting wins).

Go: `go test ./...` all packages ok; `go test -race` on config, codex, wsapi, taskrunner ok; `go vet ./...` and gofmt clean. Web: vitest 324 files / 3636 tests pass;
`bunx tsc -b packages/ui` clean. ACs 1-6 by the named Go tests (internal/config/provider_env_test.go, internal/taskrunner/provider_env_test.go, internal/wsapi/provider_env_test.go);
AC7 by web/packages/ui/src/components/providers/provider-env.test.tsx. AC8 mutations (all went red, then reverted): dropping the append in `runClaudeNative` -> `TestClaudeLaunchEnvFromProviderEnv`,
`...SettingBeatsProcessEnv`, `TestProviderEnvAppliesToNextLaunch`; `checkProviderEnvKey` always nil -> `TestProviderEnvRejects`, `TestSetProviderEnv`, `TestProviderSetEnvValidatesKey`;
`AllowlistEnv` swapping in another CLAUDE_CONFIG_DIR -> `TestProbeLaunchIgnoresProviderEnvSetting`. AC9: /tmp/provider-env-evidence/{before,after,run}.txt and /tmp/provider-env-shots/ (light+dark, 1280+390).
Not verified: a real authenticated Claude/Codex login through a changed dir (no throwaway login exists; the Human's was not touched), the desktop app started from Finder, Windows paths in the daemon.
