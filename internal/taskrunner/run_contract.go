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
	// content: never log. It carries no secret: the row's only credential is the
	// run's own process tree, checked by the daemon on the socket's peer.
	Env map[string]string
	// Socket opens the run's browser socket, bound to the agent process rooted
	// at rootPID, and returns its path (nil or an error: no row). The runner
	// calls it only at a delivery point (a row that really goes to the
	// provider); Revoke closes the socket if the provider then did not take the
	// row.
	Socket func(rootPID int) (string, error)
	// OpenPending opens the run's socket BEFORE the agent process exists (the
	// Claude CLI path) and returns its path and a bind function that attaches it
	// to the agent's pid once the process runs; a connection before bind is
	// refused. Nil: this host cannot do that, and then such an agent gets no browser.
	OpenPending func() (path string, bind func(rootPID int) error, err error)
	Revoke      func()
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
