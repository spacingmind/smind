package workspace

import (
	"context"
	"sync/atomic"
)

// NetworkGitConfig returns extra per-invocation git options for a network
// call (fetch, push) to remote in dir -- the `-c credential.helper=...` pair
// that lets git use the forge CLI's login over https (ADR-0039 section 8,
// O9). It returns nil when the remote is not https or the forge is not signed
// in. The options go on the one command line; no git config file is edited.
type NetworkGitConfig func(ctx context.Context, dir, remote string) []string

var networkGit atomic.Pointer[NetworkGitConfig]

// SetNetworkGitConfig installs the hook the package's network git calls
// (the PR push and fetch, and the task-creation fetches) consult; nil removes
// it. It is process-wide because the fetch helpers are package functions.
func SetNetworkGitConfig(fn NetworkGitConfig) {
	if fn == nil {
		networkGit.Store(nil)
		return
	}
	networkGit.Store(&fn)
}

// netGitArgs returns args with the hook's options in front, or args itself
// when there are none.
func netGitArgs(ctx context.Context, dir, remote string, args ...string) []string {
	fn := networkGit.Load()
	if fn == nil {
		return args
	}
	extra := (*fn)(ctx, dir, remote)
	if len(extra) == 0 {
		return args
	}
	return append(append([]string(nil), extra...), args...)
}
