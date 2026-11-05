package wsapi

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
)

// Run events retention RPCs (docs/plans/active/run-events-retention.md).
// Human-local only, like debug.memory: refused to run-scoped agents, over the
// relay and on a non-loopback connection; on no method list for either.

// retentionMethods lists them; a test walks it.
var retentionMethods = []string{"runs.prune", "db.compact"}

// requireLocalHuman is the guard debug.memory and the retention RPCs share.
func requireLocalHuman(method string, rc *requestContext) error {
	switch {
	case rc == nil || rc.runScoped:
		return forbidden(method + " is not available to agents")
	case !rc.local:
		return forbidden(method + " is only available on a direct connection, not over the relay")
	case rc.channel != ChannelLoopback:
		return forbidden(method + " needs a local (loopback) connection")
	}
	return nil
}

// retentionDays is runs.historyRetentionDays read from the current config.
func retentionDays() int {
	cfg, err := config.Load()
	if err != nil {
		return config.DefaultRunHistoryRetentionDays
	}
	return cfg.Runs.HistoryRetentionDays
}

func handleRunsPrune(p *runs.Pruner) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if err := requireLocalHuman("runs.prune", rc); err != nil {
			return nil, err
		}
		var params struct {
			DryRun bool `json:"dryRun"`
		}
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &params); err != nil {
				return nil, &rpcCodeError{code: codeInvalid, msg: "runs.prune: invalid params: " + err.Error()}
			}
		}
		if params.DryRun {
			return p.Preview()
		}
		rep, err := p.Run(ctx)
		if errors.Is(err, runs.ErrPruneRunning) {
			return nil, &rpcCodeError{code: codeInvalid, msg: "runs.prune: a prune pass is already running"}
		}
		return rep, err
	}
}

func handleDBCompact(reg *runs.Registry, db *store.Store) handlerFunc {
	return func(ctx context.Context, rc *requestContext, _ json.RawMessage) (any, error) {
		if err := requireLocalHuman("db.compact", rc); err != nil {
			return nil, err
		}
		if n := reg.RunningCount(); n > 0 {
			return nil, &rpcCodeError{code: codeInvalid, msg: "db.compact: refused while runs are running; it holds the write lock for the whole rewrite"}
		}
		return db.Compact(ctx)
	}
}
