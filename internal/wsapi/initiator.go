package wsapi

import (
	"context"

	"github.com/spacingmind/smind/internal/workspace"
)

// humanContext returns ctx marked as started by the Human when the request
// reached the daemon on a user surface (a direct click or CLI command), and
// ctx unchanged when the caller is a run-scoped agent connection or rc is
// nil. The handlers of the human RPCs (task.stage, task.commit, task.push,
// task.updateFromBase, task.createPr, task.updatePr) mark their Manager
// call's context with it, so the repository's hooks, filters and merge
// drivers run for a person's action and stay neutralised for everything the
// daemon starts on its own (P0 "daemon-initiated git runs stripped", D3/D4).
func humanContext(ctx context.Context, rc *requestContext) context.Context {
	if rc == nil || rc.runScoped {
		return ctx
	}
	return workspace.WithHumanInitiated(ctx)
}
