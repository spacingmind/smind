package runs

import (
	"encoding/json"

	"github.com/spacingmind/smind/internal/history/native"
)

// HistoryInfo says how a finished run's stored history relates to the full one.
type HistoryInfo struct {
	Source      string // "full" | "skeleton"
	Reason      string // "" when Source is "full"; else a native.Reason value, "rebuild_not_requested" or "excerpt_pruned"
	CanLoadFull bool   // a human may ask for the rebuilt history
}

// HistoryInfo reports whether runID's stored history is the full one or a
// skeleton, and whether a human may ask for the rebuilt history. A run that is
// unknown, running, pruned, or not yet converted is "full": the caller omits
// the label and serves the stored events.
func (reg *Registry) HistoryInfo(runID string) HistoryInfo {
	row, err := reg.storedReadable(runID)
	if err != nil || row.HistoryPrunedAt != nil {
		return HistoryInfo{Source: "full"}
	}
	sk, err := reg.st.GetRunSkeleton(runID)
	if err != nil || sk.SkeletonAt == nil {
		return HistoryInfo{Source: "full"}
	}
	var ref native.Ref
	if json.Unmarshal([]byte(sk.NativeRef), &ref) == nil {
		reg.mu.Lock()
		rb := reg.rebuilder
		reg.mu.Unlock()
		if rb != nil && (ref.Harness == native.HarnessClaudeCode || ref.Harness == native.HarnessCodex) && ref.SessionID != "" {
			reason := string(native.ReasonHumanOnly)
			if sk.ExcerptsPrunedAt != nil {
				// the excerpt tier is gone: the stored history serves summaries
				// for the large payloads, so a rebuild is what restores them
				reason = "excerpt_pruned"
			}
			return HistoryInfo{Source: "skeleton", Reason: reason, CanLoadFull: true}
		}
	}
	return HistoryInfo{Source: "skeleton", Reason: string(native.ReasonCannotRebuild), CanLoadFull: false}
}
