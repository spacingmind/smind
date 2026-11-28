package workspace

import "context"

// initiatorKey is the context key carrying who started a git call: the
// Human (a direct click or a CLI command) or the daemon itself. Git the
// daemon starts on its own runs with gitsafe's stripped mode; git the
// Human starts runs with the repository's hooks and drivers (the gitsafe
// baseline). execGit, the choke point of every runGit* helper, reads the
// mark; a context without it means daemon-initiated, the default.
type initiatorKey struct{}

// WithHumanInitiated returns ctx marked as started by the Human. A nil ctx
// returns nil, like the context package's own With* functions refuse it.
func WithHumanInitiated(ctx context.Context) context.Context {
	if ctx == nil {
		return nil
	}
	return context.WithValue(ctx, initiatorKey{}, true)
}

// IsHumanInitiated reports whether ctx carries the Human-initiator mark.
func IsHumanInitiated(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	human, _ := ctx.Value(initiatorKey{}).(bool)
	return human
}
