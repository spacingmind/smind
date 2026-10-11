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
func GLMCommand(opts ...GLMOption) []string {
	var o glmOptions
	for _, opt := range opts {
		opt(&o)
	}
	cmd := []string{"npx", "-y", "glm-acp-agent@1.3.0"}
	if o.maxTurns > 0 {
		cmd = append(cmd, "--max-turns", strconv.Itoa(o.maxTurns))
	}
	return cmd
}
