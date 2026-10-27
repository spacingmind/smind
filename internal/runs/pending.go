package runs

import (
	"sort"

	"github.com/spacingmind/smind/internal/taskrunner"
)

// PendingPermission is one permission request a run is waiting on.
type PendingPermission struct {
	RunID     string
	TaskID    int64
	ChatID    int64
	RequestID string
	Summary   string
	Detail    *taskrunner.PermissionDetail
}

// PendingPermissions lists every request a live run is waiting on, ordered by
// run then request id. It only reads: answering stays RespondPermission.
func (reg *Registry) PendingPermissions() []PendingPermission {
	reg.mu.Lock()
	rs := make([]*run, 0, len(reg.runs))
	for _, r := range reg.runs {
		rs = append(rs, r)
	}
	reg.mu.Unlock()

	var out []PendingPermission
	for _, r := range rs {
		r.mu.Lock()
		if len(r.pendingPermissions) > 0 {
			for _, e := range r.history {
				if e.Type != taskrunner.EventTypePermissionRequest {
					continue
				}
				if _, open := r.pendingPermissions[e.PermissionRequestID]; open {
					out = append(out, PendingPermission{RunID: r.id, TaskID: r.taskID, ChatID: r.chatID,
						RequestID: e.PermissionRequestID, Summary: e.PermissionSummary, Detail: e.PermissionDetail})
				}
			}
		}
		r.mu.Unlock()
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].RunID != out[j].RunID {
			return out[i].RunID < out[j].RunID
		}
		return out[i].RequestID < out[j].RequestID
	})
	return out
}
