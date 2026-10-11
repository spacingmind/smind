package acp

import "strconv"

// GLMCommand returns the command to spawn the GLM ACP agent
// (glm-acp-agent, see https://github.com/stefandevo/glm-acp-agent), for use
// with New. It requires npx (Node.js) to be available on $PATH; this
// package does not vendor or install Node itself.
//
// The pinned version matters for two measured reasons. 1.3.0 ignored
// --max-turns and ACP_GLM_MAX_TURNS (a hard 20-turn cap), and reported only
// the LAST model call's usage in PromptResponse.usage, so every run was
// under-metered. 1.6.1+ accepts --max-turns; 1.11+ reports the prompt's
// summed usage across all its model calls. Never pin below 1.11.0 (see
// docs/plans/active/dogfood-gap-fixes.md).
func GLMCommand() []string {
	return []string{"npx", "-y", "glm-acp-agent@1.14.0"}
}

// GLMCommandWithMaxTurns is GLMCommand plus `--max-turns <n>` when n > 0;
// otherwise (0 = unset, or negative) it is plain GLMCommand() and the agent
// keeps its own default turn cap.
func GLMCommandWithMaxTurns(maxTurns int) []string {
	cmd := GLMCommand()
	if maxTurns > 0 {
		cmd = append(cmd, "--max-turns", strconv.Itoa(maxTurns))
	}
	return cmd
}
