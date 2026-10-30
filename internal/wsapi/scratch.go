package wsapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/spacingmind/smind/internal/scratch"
)

// scratchUsageResult is the wire shape of scratch.usage: sizes of the
// per-task scratch root, counts only, no paths.
type scratchUsageResult struct {
	TotalBytes       int64 `json:"totalBytes"`
	TotalFiles       int   `json:"totalFiles"`
	Dirs             int   `json:"dirs"`
	ReclaimableDirs  int   `json:"reclaimableDirs"`
	ReclaimableBytes int64 `json:"reclaimableBytes"`
}

// scratchCleanResult is the wire shape of scratch.clean: what the sweep of
// closed tasks' dirs removed.
type scratchCleanResult struct {
	Files int   `json:"files"`
	Bytes int64 `json:"bytes"`
	Dirs  int   `json:"dirs"`
}

// AttachScratch wires the scratch dirs into the API: it registers the
// scratch.usage and scratch.clean RPCs over m. Both are human-only like
// provider.setSecret: a run-scoped (agent) caller is refused, and plugin
// sessions are denied by the "scratch." never-list prefix. Neither takes
// params. Call it once, before the API serves any connection (the handler
// table is read when a connection starts), like AttachUsage.
func (a *API) AttachScratch(m *scratch.Manager, live func(taskID int64) bool) {
	a.hs["scratch.usage"] = handleScratchUsage(m, live)
	a.hs["scratch.clean"] = handleScratchClean(m, live)
}

func handleScratchUsage(m *scratch.Manager, live func(taskID int64) bool) handlerFunc {
	return func(_ context.Context, rc *requestContext, _ json.RawMessage) (any, error) {
		if rc != nil && rc.runScoped {
			return nil, &rpcCodeError{code: codeForbidden, msg: "scratch.usage is not available to agents"}
		}
		u, err := m.Usage(live)
		if err != nil {
			return nil, fmt.Errorf("scratch.usage: %w", err)
		}
		return scratchUsageResult{
			TotalBytes:       u.Total.Bytes,
			TotalFiles:       u.Total.Files,
			Dirs:             u.Dirs,
			ReclaimableDirs:  u.ReclaimableDirs,
			ReclaimableBytes: u.Reclaimable.Bytes,
		}, nil
	}
}

// handleScratchClean sweeps the closed tasks' dirs (live ones are kept,
// busy ones skipped for the next sweep). The dir count is measured just
// before the sweep; the log carries counts only.
func handleScratchClean(m *scratch.Manager, live func(taskID int64) bool) handlerFunc {
	return func(_ context.Context, rc *requestContext, _ json.RawMessage) (any, error) {
		if rc != nil && rc.runScoped {
			return nil, &rpcCodeError{code: codeForbidden, msg: "scratch.clean is not available to agents"}
		}
		u, err := m.Usage(live)
		if err != nil {
			return nil, fmt.Errorf("scratch.clean: %w", err)
		}
		rem, err := m.Sweep(live)
		if err != nil {
			return nil, fmt.Errorf("scratch.clean: %w", err)
		}
		log.Printf("scratch: cleaned %d file(s), %d bytes", rem.Files, rem.Bytes)
		return scratchCleanResult{Files: rem.Files, Bytes: rem.Bytes, Dirs: u.ReclaimableDirs}, nil
	}
}
