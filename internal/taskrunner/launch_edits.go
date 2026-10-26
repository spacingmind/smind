package taskrunner

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spacingmind/smind/internal/store"
)

// LaunchEdits are the changes plugin hooks (ADR-0042 PB.3) may make to one
// run's launch, already validated by the host: environment variables to add
// and MCP servers to add. The runner still never lets them replace anything
// that already exists.
type LaunchEdits struct {
	Env map[string]string
	Mcp []store.McpServer
}

type launchEditsKey struct{}

// WithLaunchEdits returns ctx carrying e.
func WithLaunchEdits(ctx context.Context, e LaunchEdits) context.Context {
	return context.WithValue(ctx, launchEditsKey{}, e)
}

// LaunchEditsFrom returns the edits attached to ctx (zero if none).
func LaunchEditsFrom(ctx context.Context) LaunchEdits {
	e, _ := ctx.Value(launchEditsKey{}).(LaunchEdits)
	return e
}

// hookEnvEntries returns the KEY=value entries to add to a launch whose
// environment already holds existing (plus the daemon's own): a key that is
// already set, by the task, the provider or the daemon, is skipped and named in
// the note, never overridden.
func hookEnvEntries(ctx context.Context, existing []string) (add []string, note string) {
	env := LaunchEditsFrom(ctx).Env
	if len(env) == 0 {
		return nil, ""
	}
	have := map[string]bool{}
	for _, e := range existing {
		k, _, _ := strings.Cut(e, "=")
		have[k] = true
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var skipped []string
	for _, k := range keys {
		if _, set := os.LookupEnv(k); set || have[k] {
			skipped = append(skipped, k)
			continue
		}
		add = append(add, k+"="+env[k])
	}
	if len(skipped) > 0 {
		note = fmt.Sprintf("plugin env not applied (already set): %s", strings.Join(skipped, ", "))
	}
	return add, note
}

// withHookMcp adds the hook-named servers to rows, never over a name that is
// already there.
func withHookMcp(ctx context.Context, rows []store.McpServer) []store.McpServer {
	extra := LaunchEditsFrom(ctx).Mcp
	if len(extra) == 0 {
		return rows
	}
	have := map[string]bool{}
	for _, r := range rows {
		have[r.Name] = true
	}
	out := append([]store.McpServer(nil), rows...)
	for _, r := range extra {
		if !have[r.Name] {
			have[r.Name] = true
			out = append(out, r)
		}
	}
	return out
}
