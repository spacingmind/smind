package acp

import "strconv"

// GLMOption customises the command GLMCommand returns.
type GLMOption func(*glmOptions)

type glmOptions struct{ maxTurns int }

// WithMaxTurns passes --max-turns n to the GLM agent, capping the turns of
// one prompt (n <= 0 leaves the agent's own default).
func WithMaxTurns(n int) GLMOption {
	return func(o *glmOptions) { o.maxTurns = n }
}

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
func GLMCommand(opts ...GLMOption) []string {
	var o glmOptions
	for _, opt := range opts {
		opt(&o)
	}
	cmd := []string{"npx", "-y", "glm-acp-agent@1.14.0"}
	if o.maxTurns > 0 {
		cmd = append(cmd, "--max-turns", strconv.Itoa(o.maxTurns))
	}
	return cmd
}
