package taskrunner

import (
	"context"
	"fmt"
	"runtime"

	"github.com/spacingmind/smind/internal/scratch"
)

// scratchCtxKey isolates the scratch launch-env entries RunPrompt stashes
// in ctx from every other context value.
type scratchCtxKey struct{}

// withScratchEnv returns ctx carrying entries for this run's launches; a
// copy is stored so the caller can't mutate what launches later see.
func withScratchEnv(ctx context.Context, entries []string) context.Context {
	return context.WithValue(ctx, scratchCtxKey{}, append([]string(nil), entries...))
}

// scratchEnvFrom returns the scratch launch-env entries ctx carries, nil
// when it carries none.
func scratchEnvFrom(ctx context.Context) []string {
	entries, _ := ctx.Value(scratchCtxKey{}).([]string)
	return entries
}

// WithScratch gives the Runner a per-task scratch dir manager
// (internal/scratch): every agent run then gets TMPDIR (plus TMP/TEMP on
// Windows) pointing at scratch/<taskID>/, created before the launch.
func WithScratch(m *scratch.Manager) Option {
	return func(r *Runner) { r.scratch = m }
}

// WithSharedCaches adds shared build/cache dirs (GOMODCACHE, GOCACHE, ...)
// to every agent launch env, prepended so a provider- or task-pinned value
// wins. A nil or empty SharedCaches turns the feature off.
func WithSharedCaches(c scratch.SharedCaches) Option {
	return func(r *Runner) {
		if len(c) == 0 {
			c = nil
		}
		r.sharedCaches = c
	}
}

// scratchLaunchEnv returns the scratch-related env entries for a run of
// taskID: the task's scratch dir as TMPDIR (when a scratch Manager is set)
// plus the shared caches. Entries are meant to be prepended, so anything
// the provider/task env pins wins (last entry wins). nil, nil when there
// is nothing to add.
func (r *Runner) scratchLaunchEnv(taskID int64) ([]string, error) {
	var entries []string
	if r.scratch != nil {
		dir, err := r.scratch.Ensure(taskID)
		if err != nil {
			return nil, fmt.Errorf("scratch dir: %w", err)
		}
		entries = append(entries, scratch.TempEnv(dir, runtime.GOOS)...)
	}
	entries = append(entries, r.sharedCaches.Entries()...)
	if len(entries) == 0 {
		return nil, nil
	}
	return entries, nil
}
