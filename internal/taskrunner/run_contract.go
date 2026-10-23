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
