package acp

// GLMCommand returns the command to spawn the GLM ACP agent
// (glm-acp-agent, see https://github.com/stefandevo/glm-acp-agent), for use
// with New. It requires npx (Node.js) to be available on $PATH; this
// package does not vendor or install Node itself.
func GLMCommand() []string {
	return []string{"npx", "-y", "glm-acp-agent@1.3.0"}
}
