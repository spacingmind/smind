package taskrunner

import "context"

// RunContract is the per-run seat contract (ADR-0041), attached to a run's
// ctx so RunPrompt's signature stays unchanged. The runs registry builds it
// from the run's agent profile; system runs never get one. Later steps add
// fields.
type RunContract struct {
	// Instructions is the seat's role text. Empty delivers nothing. It is
	// policy, not content: never log it or put it in events or errors.
	Instructions string

	// Mcp is the seat's MCP policy (ADR-0041 S2b). Nil keeps the legacy
	// behaviour exactly: the workspace's registered servers, untouched.
	Mcp *McpPolicy

	// DisallowedTools is the provider-level deny list for this run (claude
	// only); policy, not content.
	DisallowedTools []string
}

// McpPolicy makes a run's MCP server set managed by the daemon: the name
// "smind" is reserved (a registered server of that name never reaches a
// managed run) and the built-in orchestration server is added only when
// Orchestration is true.
type McpPolicy struct {
	// Orchestration adds the built-in `smind mcp serve` stdio entry.
	Orchestration bool
	// Env is the built-in entry's environment (run identity, SMIND_HOME).
	// Ignored when Orchestration is false. Policy, not content: never log.
	Env map[string]string
	// Browser, when set, adds the reserved `smind-browser` stdio entry
	// (`smind mcp browser`, ADR-0061). Nothing sets it while the browser gate
	// is dark.
	Browser *BrowserPolicy
	// BrowserOnly marks a policy created only to carry the browser grant (the
	// run had no other MCP policy): it says nothing about orchestration.
	BrowserOnly bool
}

// BrowserPolicy grants a run the browser tools' MCP entry.
type BrowserPolicy struct {
	// Env is the entry's environment (run identity, SMIND_HOME). Policy, not
	// content: never log. It does not carry the capability token: that is
	// issued by Issue only when the row is actually handed to the agent.
	Env map[string]string
	// Issue mints the run's capability token (nil: none) and Revoke drops it.
	// The runner calls Issue only at a delivery point (a row that really goes
	// to the provider) and Revoke if the provider then did not take the row.
	Issue  func() string
	Revoke func()
}

type runContractKey struct{}

// WithRunContract returns ctx carrying c.
func WithRunContract(ctx context.Context, c RunContract) context.Context {
	return context.WithValue(ctx, runContractKey{}, c)
}

// RunContractFrom returns the contract attached to ctx (zero if none).
func RunContractFrom(ctx context.Context) RunContract {
	c, _ := ctx.Value(runContractKey{}).(RunContract)
	return c
}

// roleInstructionsPreface wraps text for the first prompt of a new ACP
// session.
func roleInstructionsPreface(text string) string {
	return "[Role instructions for this session]\n" + text + "\n[End of role instructions]\n\n"
}
