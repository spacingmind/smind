# Add Kimi as a second ACP-speaking provider

## Acceptance Criteria

- New `taskrunner.ProviderKimi` constant (`"kimi"`), alongside the existing
  `ProviderGLM`/`ProviderClaudeNative`.
- Kimi is driven through the exact same `internal/acp` client GLM already
  uses — Kimi CLI speaks ACP natively (confirmed: `kimi acp`, see
  Decisions), so this needs **no new protocol package**, unlike Codex.
- `internal/acp` gains `KimiCommand() []string` (mirrors `GLMCommand()`)
  returning `["kimi", "acp"]`.
- `taskrunner.Runner` currently has exactly one ACP command slot
  (`acpCommand []string`, one `WithACPCommand` option, hardcoded to GLM's
  default in `New`) — this only works because there's one ACP provider
  today. Refactor to a per-provider command lookup (e.g. a
  `map[Provider][]string` seeded with `{ProviderGLM: acp.GLMCommand(),
  ProviderKimi: acp.KimiCommand()}`) so a third ACP-speaking provider added
  later (Gemini CLI, per the earlier provider-scoping discussion) is
  another map entry, not another one-off field. `WithACPCommand` becomes
  provider-scoped (e.g. `WithACPCommand(provider Provider, command
  []string)`), preserving its existing use in tests (pointing `ProviderGLM`
  at `fakeagent`) unchanged in spirit.
- `Runner.RunPrompt`'s `switch provider` gains a `case ProviderKimi`,
  sharing `runGLM`'s body (rename to something provider-generic, e.g.
  `runACP(ctx, provider, worktreePath, prompt, decider, events)`, looking
  up its command from the new map) rather than duplicating the function —
  the two providers differ only in which command is spawned, nothing else
  about the ACP flow.
- No changes to `internal/wsapi`/web UI wiring needed: `provider` is
  already a free-form string on the wire (`run.start`'s `provider` param),
  so `"kimi"` works the moment `taskrunner` recognizes it — verify this is
  actually true by reading the wire handler, don't assume.
- Out of scope (explicitly, see Decisions): wiring Kimi's already-existing
  OAuth credential (`internal/accounts`'s `kimiClientID`/`kimiTokenURL`)
  into the spawned `kimi acp` subprocess's auth. Confirmed by reading the
  real Kimi CLI docs that `kimi acp` expects the CLI to already be
  logged in (`kimi` then `/login`, once, out of band) — this matches
  exactly how GLM and Claude Code today are **not** handed any
  credential from `internal/accounts` either (grepped: zero env/credential
  wiring in `internal/acp`/`internal/taskrunner`). Not a regression this
  task introduces; a real pre-existing gap between the accounts/routing
  subsystem and taskrunner, worth its own future task, not bundled here.

## Test Scenarios

- `internal/taskrunner`: adapt the existing GLM test suite's shape for
  Kimi — a test using `WithACPCommand(ProviderKimi, fakeAgentPath)`
  confirms `RunPrompt(..., ProviderKimi, ...)` drives a real fake ACP
  agent subprocess end to end (reuses `internal/taskrunner/fakeagent`,
  already ACP-speaking — no new fake agent needed).
- Confirm `WithACPCommand(ProviderGLM, ...)` and
  `WithACPCommand(ProviderKimi, ...)` are independent (setting one doesn't
  clobber the other) — a real behavior change from today's single-field
  design, worth its own explicit test.
- `internal/wsapi`: one `run.start` test with `provider: "kimi"` against a
  fake ACP agent, proving the wire path recognizes it end to end (not just
  `taskrunner` in isolation).
- `go build ./...`, `gofmt -l`, `go vet ./...`, `go test -race -count=3
  ./...` clean.
- Manual: `pip install kimi-cli`, real login, confirm `smind`'s
  `ProviderKimi` path actually drives the real Kimi CLI end to end. Flag
  plainly in Validation if this manual step isn't actually performed this
  session (no guarantee `kimi-cli` is installable/loggable-in in this
  sandbox) — same honesty standard as this project's browser-testing gaps.

## Decisions

- Kimi CLI (`github.com/MoonshotAI/kimi-cli`, PyPI `kimi-cli`) speaks ACP
  natively via `kimi acp` — confirmed from the real GitHub README (fetched
  live): install `pip install kimi-cli`, ACP invocation `kimi acp`, editor
  config example `"command": "kimi", "args": ["acp"]`. Not npx-self-installing
  like GLM's `npx -y glm-acp-agent@1.3.0` — Kimi requires a real prior
  `pip install` + one-time interactive `/login`, per the same README.
  Default command is `["kimi", "acp"]`, assuming that prerequisite; this is
  a genuine UX difference from GLM worth documenting in user-facing docs
  later, not a bug to route around here.
- Refactor `Runner`'s single `acpCommand` field to a per-provider map now
  rather than after Codex/Gemini CLI land, since Kimi is the second data
  point that proves it needs to be a map, not a one-off special case.
- Credential injection (`internal/accounts` → spawned subprocess env) is
  explicitly out of scope — see Acceptance Criteria.

## Progress

- [x] `internal/acp`: `KimiCommand()`
- [x] `internal/taskrunner`: `ProviderKimi`, per-provider ACP command map,
  `WithACPCommand` becomes provider-scoped, `runGLM` → `runACP`
- [x] Tests (taskrunner + wsapi)
- [x] Verification (race suite; manual real-CLI check not performed, see
  Validation)

## Validation

- `KimiCommand()` returns `["kimi", "acp"]`, matching the real
  `MoonshotAI/kimi-cli` README (fetched live) exactly.
- `TestRunner_RunPrompt_Kimi` (`internal/taskrunner/runner_test.go`):
  `RunPrompt(..., ProviderKimi, ...)` drives a real ACP-speaking fake agent
  subprocess through `runACP`, same event shape as the GLM test.
- `TestRunner_WithACPCommand_IsPerProviderIndependent`: overriding
  `ProviderGLM`'s command via the real `WithACPCommand` option leaves
  `ProviderKimi`'s default (`acp.KimiCommand()`) untouched, and vice versa
  by construction (map keyed by provider).
- `TestServer_RunStart_KimiProvider` (`internal/wsapi/run_test.go`): a real
  `run.start` wire call with `provider: "kimi"` drives a run to completion
  and `run.logs` returns its full history -- confirms `wsapi` needed zero
  changes (provider is decoded as a bare string with no allowlist, exactly
  as predicted in Acceptance Criteria).
- `go test -race -count=3 ./...` clean across every package. `gofmt -l`,
  `go vet ./...`, `task build` clean; `git status` after `task build`
  showed exactly the expected changed/new files.
- **Manual real-CLI check: not performed.** This sandbox has no working
  `pip` (only `pip3`, via linuxbrew) and no `kimi` binary installed;
  `kimi acp` also requires a one-time interactive `/login` against a real
  Moonshot AI account, which isn't something this session can do headless.
  The command string and ACP-mode flag are sourced from the real, current
  `MoonshotAI/kimi-cli` GitHub README (fetched live via WebFetch, not
  guessed), but an actual `kimi acp` handshake against `internal/acp`
  has not been empirically verified end to end.
