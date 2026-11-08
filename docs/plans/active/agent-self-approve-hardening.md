# Agents must not answer their own requests: CLI refusal and a compiled-in floor rule

Status: active (2026-11-09). Defence in depth, not a fix: see Limits. Size: M (4): scope 2 (CLI, policy engine, run launch env), risk 2 (permissions).

## The hole

The human-only RPCs (`run.respondPermission`, `run.answerQuestion`, `inbox.resolve`, the heredoc reveals) refuse an identified run-scoped connection, but a connection that never calls `session.identify` counts as the human's. An agent with a shell can run `smind task approve|deny|answer`, `smind permission ...`, or speak the socket itself, and answer its own request. The CLI identifies only inside `smind mcp serve`, and only from `SMIND_RUN_ID`, which the daemon sets for the MCP entry's environment and not for the agent's shell.

## Acceptance criteria

- AC1: every harness launch of an agent run carries `SMIND_AGENT_RUN=1` in its environment, so the agent's shell inherits it (the one place all launches read: the per-task launch env in `RunPrompt`).
- AC2: the human-only CLI commands (`task approve|deny|answer`, `permission ...`, `host approve|reject`, `relay devices approve|revoke`) refuse, with exit 1 and a one-line message, when `SMIND_RUN_ID` or `SMIND_AGENT_RUN` is set: `task approve|deny|answer`, `permission ...`, and the other verbs of the one shared table (`permpolicy.HumanOnlyCLI`). `smind mcp serve` and the read-only commands are unaffected.
- AC3: a compiled-in floor rule `no-self-approve` denies, in any agent Bash request, a command that runs one of those CLI commands however it is wrapped (`env`, `command`, `exec`, `xargs`, `nohup`, `sh -c`, `bash -lc`, a quoted or absolute-path binary, `$(which smind)`, `env -u SMIND_RUN_ID`), and one that names a human-only RPC method anywhere in its raw text, heredoc bodies included, when the command also runs a network client or an interpreter (curl, nc, python, node, websocat, `go run` ...): a search, a commit message or an edit that only names a method is no call. No `policy.yaml` rule can allow it.
- AC4: the CLI table, the floor rule and the daemon's handler map cannot drift: a test walks the handler map and fails when a method the table names is run-scoped or does not exist.
- AC5: the limits below are written in the docs.

## Test scenarios

- `TestFloor_SelfApproveCLIDenied`: the plain commands, every wrapper listed in AC3, a quoted and an absolute binary, `$(which smind)`, a renamed binary for `task approve`, a bodyless and a heredoc-body RPC name; near misses allowed (`smind task ls`, `echo smind task approve`, a commit message, `smind task permissions`).
- `TestFloor_SelfApproveRPCNameDenied`, `TestFloor_SelfApproveHeredocBodyDenied`.
- `TestDecide_SelfApproveCannotBeAllowedByPolicy`: an `allow` rule for `smind *` does not win over the floor.
- `TestHumanOnlyCLI_TableMatchesHandlers` (wsapi): every named RPC exists and none is run-scoped.
- cmd/smind: `TestHumanOnlyCommandsRefuseInAgentRun` (both markers, each command), `TestHumanOnlyCommandsRunForHumans`, `TestMcpServeStillRuns`.
- taskrunner: `TestRunPromptLaunchEnvCarriesAgentMarker`.
- Each rule and branch has a mutation that fails a test (recorded in Validation).

## Limits (honest)

An agent running as the same OS user can still reach the daemon by means this cannot see: a script file it wrote earlier and runs (the floor judges the command text, not the file), a name assembled at run time, another language's socket client, `bypassPermissions` runs (no request is ever judged), an environment it edits before launching the CLI, a copy of the CLI renamed to something the matcher does not know (only `task approve|deny|answer` is matched without the name), a program that starts the CLI from another language with the arguments as separate list items (`subprocess.run(['smind','task','approve'])`), a program that runs a command from its own program text (`git -c alias.x='!smind ...'`, `awk 'BEGIN{system(...)}'`, GNU `sed` `e`), or `--auto-accept`/`--mode` flags and `smind task budget`, which let an agent loosen its own run or cap through the same unidentified connection and are not in the table (budget has a read form agents use). A command runner (`su -c`, `ssh`, `script -c`, `find -exec`, `eval` ...) or a shell fed text (`echo '...' | sh`) is matched on its text, and `task approve|deny|answer` is matched as adjacent words after any other binary (`go run ./cmd/smind task approve`), so a prose argument written as separate unquoted words (`smind task send 1 claude never task approve`) is refused too. In an agent run the CLI also refuses `--help` of these commands. Plugin providers are launched with a scrubbed environment, so they carry no marker: the floor rule is their only layer. These two layers narrow the hole; they do not close it. The principal question (an unidentified local connection counts as the human) is ADR-0056.

## Decisions

- One marker, `SMIND_AGENT_RUN`, in the launch env; `SMIND_RUN_ID` (the MCP entry's) is honoured too.
- The raw-text RPC scan is paired with a transport (found by the second read: agents working on this repo grep for these names all day).
- A shared table in `permpolicy` (`HumanOnlyCLI`, `HumanOnlyRPCs`) is the single source for the CLI refusal and the floor rule.

## Progress

Built: `permpolicy.HumanOnlyCLI` and `HumanOnlyRPCs` (one table), the `no-self-approve` floor rule (per segment after unwrapping, plus a raw-text scan for the RPC names so heredoc bodies count), the guard in `run()`, `SMIND_AGENT_RUN` in the per-task launch env of every harness (Claude native, Codex, ACP, GLM), the drift test in wsapi, docs (public permission-policy table, ADR-0047 floor table, the policy template comment). `run.answerQuestion`/`run.answerPermission` are named ahead of their handlers (the agent-questions work); the drift test lets them be absent but never run-scoped.

## Validation

Mutations (each fails the named test when applied to the committed file): no raw-RPC scan, no per-segment CLI rule, no renamed-binary case, no harmless-reader exemption, no `permission` in the table, no `task answer` verb, no `run.respondPermission` in the RPC table, no computed-name case, the guard in `run()` off, `SMIND_RUN_ID` ignored, `SMIND_AGENT_RUN` ignored, the launch env marker dropped. Two survived on the first run (the harmless-reader and computed-name branches); cases were added and both are killed.
