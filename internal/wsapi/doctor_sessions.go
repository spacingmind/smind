package wsapi

import (
	"context"
	"encoding/json"

	"github.com/spacingmind/smind/internal/history/native"
	"github.com/spacingmind/smind/internal/store"
)

// doctorSessionsMax bounds how many runs one doctor.sessions call probes.
const doctorSessionsMax = 20000

// doctorSessionsResult is what `smind doctor` prints about the provider
// sessions behind smind's runs (ADR-0053 H8). Counts only: no path, no run id.
type doctorSessionsResult struct {
	// Runs are the finished runs in skeleton form; WithRef those with a recorded
	// provider session; Readable those whose session file is still there and a
	// rebuild could read; TooLarge those still there but over the rebuild's size
	// bound; Gone is the rest (deleted, moved, or not under a root smind may read).
	Runs     int `json:"runs"`
	WithRef  int `json:"withRef"`
	Readable int `json:"readable"`
	TooLarge int `json:"tooLarge"`
	Gone     int `json:"gone"`
	// Checked is how many of the newest WithRef runs were probed (all of them
	// unless there are more than doctorSessionsMax).
	Checked int `json:"checked"`
}

// sessionRoots is the part of the runner doctor.sessions needs.
type sessionRoots interface {
	SessionRoots(provider, harness string) []string
}

// handleDoctorSessions answers doctor.sessions: a read-only probe of the
// provider session files named by the runs' native_ref, with exactly the path
// rules a rebuild applies. Local human only, like the retention RPCs; it never
// opens a session file, only resolves and stats it.
func handleDoctorSessions(db *store.Store, roots sessionRoots) handlerFunc {
	return func(_ context.Context, rc *requestContext, _ json.RawMessage) (any, error) {
		if err := requireLocalHuman("doctor.sessions", rc); err != nil {
			return nil, err
		}
		rows, skeletonRuns, err := db.ListNativeRefs(doctorSessionsMax)
		if err != nil {
			return nil, err
		}
		res := doctorSessionsResult{Runs: skeletonRuns, WithRef: len(rows), Checked: len(rows)}
		for _, row := range rows {
			var ref native.Ref
			if json.Unmarshal([]byte(row.Ref), &ref) != nil {
				res.Gone++
				continue
			}
			switch p := native.ProbeSession(ref, roots.SessionRoots(ref.Provider, ref.Harness)); {
			case p.Found:
				res.Readable++
			case p.TooLarge:
				res.TooLarge++
			default:
				res.Gone++
			}
		}
		return res, nil
	}
}
