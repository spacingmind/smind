---
v: 1
milestone: "Agent self-approval hardening: CLI refusal in agent runs + no-self-approve floor rule"
base: origin/develop
planRef: docs/plans/active/agent-self-approve-hardening.md
acs:
  - id: SA1
    text: "Every harness launch of an agent run carries SMIND_AGENT_RUN=1 in its environment."
    covers: { tests: [agent_marker_claude, agent_marker_codex, agent_marker_acp, agent_marker_glm], frames: [] }
  - id: SA2
    text: "smind task approve|deny|answer and smind permission refuse with exit 1 and a one-line message when SMIND_AGENT_RUN or SMIND_RUN_ID is set, and run normally without; the read and orchestration commands are untouched."
    covers: { tests: [cli_refuses_in_agent_run, cli_runs_for_humans, cli_leaves_agent_commands_alone], frames: [] }
  - id: SA3
    text: "The floor rule no-self-approve denies those commands however they are wrapped or spelled (env -u, sh -c, bash -lc, xargs, command, exec, quoted or absolute binary, $(which smind), a renamed copy for the task verbs) and any command whose raw text, heredoc bodies included, names a human-only RPC; near misses stay allowed; a policy allow rule cannot beat it."
    covers: { tests: [floor_cli_denied, floor_near_misses_allowed, floor_rpc_name_denied, floor_heredoc_body_denied, floor_not_allowable_by_policy], frames: [] }
  - id: SA4
    text: "The human-only tables cannot drift from the handler map: no named method is run-scoped, and the ones that exist stay handlers."
    covers: { tests: [table_matches_handlers, cli_table], frames: [] }
tests:
  - { name: agent_marker_claude, run: "go test ./internal/taskrunner/ -run TestLaunchEnv_AgentRunMarker_Claude", timeoutSec: 900 }
  - { name: agent_marker_codex, run: "go test ./internal/taskrunner/ -run TestLaunchEnv_AgentRunMarker_Codex", timeoutSec: 900 }
  - { name: agent_marker_acp, run: "go test ./internal/taskrunner/ -run TestLaunchEnv_AgentRunMarker_ACP", timeoutSec: 900 }
  - { name: agent_marker_glm, run: "go test ./internal/taskrunner/ -run TestLaunchEnv_AgentRunMarker_GLM", timeoutSec: 900 }
  - { name: cli_refuses_in_agent_run, run: "go test ./cmd/smind/ -run TestHumanOnlyCommandsRefuseInAgentRun", timeoutSec: 900 }
  - { name: cli_runs_for_humans, run: "go test ./cmd/smind/ -run TestHumanOnlyCommandsRunForHumans", timeoutSec: 900 }
  - { name: cli_leaves_agent_commands_alone, run: "go test ./cmd/smind/ -run TestHumanOnlyGuardLeavesAgentCommandsAlone", timeoutSec: 900 }
  - { name: floor_cli_denied, run: "go test ./internal/permpolicy/ -run TestFloor_SelfApproveCLIDenied", timeoutSec: 900 }
  - { name: floor_near_misses_allowed, run: "go test ./internal/permpolicy/ -run TestFloor_SelfApproveNearMissesAllowed", timeoutSec: 900 }
  - { name: floor_rpc_name_denied, run: "go test ./internal/permpolicy/ -run TestFloor_SelfApproveRPCNameDenied", timeoutSec: 900 }
  - { name: floor_heredoc_body_denied, run: "go test ./internal/permpolicy/ -run TestFloor_SelfApproveHeredocBodyDenied", timeoutSec: 900 }
  - { name: floor_not_allowable_by_policy, run: "go test ./internal/permpolicy/ -run TestDecide_SelfApproveCannotBeAllowedByPolicy", timeoutSec: 900 }
  - { name: cli_table, run: "go test ./internal/permpolicy/ -run TestHumanOnlyCLI_Table", timeoutSec: 900 }
  - { name: table_matches_handlers, run: "go test ./internal/wsapi/ -run TestHumanOnlyRPCTable_MatchesHandlers", timeoutSec: 900 }
mutations:
  - { id: M1, file: internal/permpolicy/floor.go, change: "CheckFloor no longer checks the raw command for a human-only RPC name", expectFail: [floor_rpc_name_denied, floor_heredoc_body_denied, floor_not_allowable_by_policy] }
  - { id: M2, file: internal/permpolicy/floor.go, change: "CheckFloor no longer calls selfApproveRules", expectFail: [floor_cli_denied, floor_not_allowable_by_policy] }
  - { id: M3, file: internal/permpolicy/humanonly.go, change: "the renamed-binary case for task approve|deny|answer is removed", expectFail: [floor_cli_denied] }
  - { id: M4, file: internal/permpolicy/humanonly.go, change: "the harmless-reader exemption (echo, grep, printf ...) is removed", expectFail: [floor_near_misses_allowed] }
  - { id: M5, file: internal/permpolicy/humanonly.go, change: "the permission command is removed from the human-only table", expectFail: [floor_cli_denied, cli_table] }
  - { id: M6, file: cmd/smind/main.go, change: "the agent-run guard in run() is disabled", expectFail: [cli_refuses_in_agent_run] }
  - { id: M7, file: cmd/smind/main.go, change: "inAgentRun ignores SMIND_RUN_ID", expectFail: [cli_refuses_in_agent_run] }
  - { id: M8, file: internal/taskrunner/runner.go, change: "RunPrompt no longer adds SMIND_AGENT_RUN to the launch env", expectFail: [agent_marker_claude, agent_marker_acp] }
commands:
  - { id: C1, run: "go vet ./internal/permpolicy/ ./cmd/smind/ ./internal/taskrunner/ ./internal/wsapi/", timeoutSec: 900 }
  - { id: C2, run: "go test ./internal/permpolicy/ ./cmd/smind/ ./internal/taskrunner/", timeoutSec: 1800 }
limits: { budgetUsd: 2, maxMinutes: 40 }
---

# Agent self-approval hardening validation contract

Defence in depth: the CLI refusal and the floor rule narrow the hole and do not close it; the limits are in the plan. The validator should also try commands the matcher might miss (and report them as limits, not failures, when they fall under the plan's Limits).
