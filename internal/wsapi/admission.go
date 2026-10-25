package wsapi

// Admission for identified seat callers (ADR-0041 S3).
//
// A connection that identified itself with session.identify (see
// runIdentity) is an agent run. When that run was launched with a MANAGED
// profile (one that carries a seat contract: a childPolicy or an
// orchestrationTools setting), this file applies the profile's policy to the
// calls the connection makes:
//
//	A. launching (task.create, run.start, chat.create) obeys childPolicy; a
//	   run.start must name a profile the seat may launch and pass exactly the
//	   settings that profile holds, so a seat cannot widen permissions by
//	   asking for a raw provider/permissionMode/model;
//	B. every mutating method that names a task, chat or run must target a
//	   strict descendant of the caller's own task;
//	C. profile.list / profile.get show only the profiles the seat may launch.
//
// HONEST LIMIT: this is a drift guard, not a sandbox. It keeps a well-meaning
// agent inside its seat contract and turns a confused or prompt-injected
// call into a clear error. An agent with a shell can read the daemon token
// and speak /ws WITHOUT identifying, which this file does not and cannot
// stop; an unidentified connection is unchanged. The real security boundary
// is the provider's permission mode (ADR-0019).
//
// Where the rules run: A, B and the profile.get refusal live in ONE hook,
// seatAdmission.admit, called from conn.dispatch right after the S2b
// allowlist gate. The hook sees the raw params, so it decodes just the few
// fields it needs (a small struct per method) instead of every handler
// duplicating the checks; the handlers keep decoding their own params, which
// is cheap and only happens for identified connections. profile.list is the
// one exception: it must FILTER a result, so newAPI wraps its handler with
// seatAdmission.wrapProfileList (same policy helper, mayLaunch).
//
// A caller without a profile, or with a profile that is not managed, gets
// NO new rule: it behaves exactly as S2b left it.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/spacingmind/smind/internal/profiles"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
)

// subtreeHopLimit bounds the parent_task_id walk in isInSubtree (same bound
// as runs.treeRootOf).
const subtreeHopLimit = 32

// seatAdmission holds what the admission rules read: the run registry (the
// caller's run, hence its profile), the profile registry and the store (task,
// chat and queue-item lookups).
type seatAdmission struct {
	reg   *runs.Registry
	profs *profiles.Registry
	st    *store.Store
}

func newSeatAdmission(reg *runs.Registry, profs *profiles.Registry, st *store.Store) *seatAdmission {
	return &seatAdmission{reg: reg, profs: profs, st: st}
}

// managed reports whether p carries a seat contract.
func managed(p store.AgentProfile) bool {
	return p.ChildPolicy != "" || p.OrchestrationTools != nil
}

// callerSeat loads the profile the caller's run was launched with. ok is
// false (and no rule applies) for a caller whose run has no profile, whose
// profile no longer exists, or whose profile is not managed. A store error
// fails closed.
func (a *seatAdmission) callerSeat(rc *requestContext) (p store.AgentProfile, ok bool, err error) {
	st, err := a.reg.State(rc.ident.runID)
	if err != nil {
		return store.AgentProfile{}, false, forbidden("your run has ended")
	}
	if st.ProfileID == 0 {
		return store.AgentProfile{}, false, nil
	}
	p, err = a.profs.Get(st.ProfileID)
	if errors.Is(err, sql.ErrNoRows) {
		return store.AgentProfile{}, false, nil
	}
	if err != nil {
		return store.AgentProfile{}, false, forbidden("cannot load your seat profile")
	}
	if !managed(p) {
		return store.AgentProfile{}, false, nil
	}
	return p, true, nil
}

// mayLaunch reports whether seat's childPolicy lets it launch target. A
// managed profile with an empty policy (not reachable through the profile
// registry, which normalises it to "none") is treated as "none".
func mayLaunch(seat, target store.AgentProfile) bool {
	switch seat.ChildPolicy {
	case "leaf":
		return target.OrchestrationTools != nil && !*target.OrchestrationTools
	case "profiles":
		for _, id := range seat.ChildProfileIDs {
			if id == target.ID {
				return true
			}
		}
	}
	return false
}

func notChildTask(id int64) error {
	return fmt.Errorf("task %d is not one of your child tasks", id)
}

func notChildProfile(id int64) error {
	return fmt.Errorf("profile %d is not one of your child profiles", id)
}

func joinIDs(ids []int64) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(parts, ", ")
}

// isInSubtree reports whether target is a STRICT descendant of callerTask,
// walking parent_task_id up from target. The caller's own task is not in its
// subtree. Bounded to subtreeHopLimit hops and cycle-safe.
func (a *seatAdmission) isInSubtree(callerTask, target int64) bool {
	if target == callerTask || target <= 0 {
		return false
	}
	seen := map[int64]bool{target: true}
	cur := target
	for i := 0; i < subtreeHopLimit; i++ {
		t, err := a.st.GetTask(cur)
		if err != nil || t.ParentTaskID == nil || *t.ParentTaskID == 0 {
			return false
		}
		parent := *t.ParentTaskID
		if parent == callerTask {
			return true
		}
		if seen[parent] {
			return false
		}
		seen[parent] = true
		cur = parent
	}
	return false
}

// requireChildTask refuses unless task is in the caller's subtree.
func (a *seatAdmission) requireChildTask(rc *requestContext, task int64) error {
	if !a.isInSubtree(rc.ident.taskID, task) {
		return notChildTask(task)
	}
	return nil
}

func archivedOutOfScope() error {
	return forbidden("archived history outside your own task subtree is not readable by agents")
}

// inOwnScope reports whether task is the caller's own task or in its subtree.
func (a *seatAdmission) inOwnScope(rc *requestContext, task int64) bool {
	return task == rc.ident.taskID || a.isInSubtree(rc.ident.taskID, task)
}

// archivedTask reports whether task exists and is archived; a store error
// counts as archived (fail closed).
func (a *seatAdmission) archivedTask(task int64) bool {
	t, err := a.st.GetTask(task)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	return err != nil || t.ArchivedAt != nil
}

// admitArchivedRead keeps the read-only archived-task history (task.list
// archived, task.tree includeArchived or an archived taskId, run.list and
// run.logs of an archived task's runs, and the persisted history behind
// them) inside the caller's own task subtree, for every run-scoped caller.
// Live reads are untouched. A request that does not decode is left to its
// handler.
func (a *seatAdmission) admitArchivedRead(rc *requestContext, method string, raw json.RawMessage) error {
	switch method {
	case "task.list":
		var p struct {
			ParentTaskID *int64 `json:"parentTaskId"`
			Archived     bool   `json:"archived"`
		}
		if json.Unmarshal(raw, &p) != nil || !p.Archived {
			return nil
		}
		if p.ParentTaskID == nil || !a.inOwnScope(rc, *p.ParentTaskID) {
			return archivedOutOfScope()
		}
	case "task.tree":
		var p struct {
			TaskID          int64 `json:"taskId"`
			IncludeArchived bool  `json:"includeArchived"`
		}
		if json.Unmarshal(raw, &p) != nil {
			return nil
		}
		if p.IncludeArchived && (p.TaskID == 0 || !a.inOwnScope(rc, p.TaskID)) {
			return archivedOutOfScope()
		}
		if p.TaskID != 0 && !a.inOwnScope(rc, p.TaskID) && a.archivedTask(p.TaskID) {
			return archivedOutOfScope()
		}
	case "run.list":
		var p struct {
			TaskID int64 `json:"taskId"`
		}
		if json.Unmarshal(raw, &p) != nil || p.TaskID == 0 {
			return nil
		}
		if !a.inOwnScope(rc, p.TaskID) && a.archivedTask(p.TaskID) {
			return archivedOutOfScope()
		}
	case "run.logs":
		var p struct {
			RunID string `json:"runId"`
		}
		if json.Unmarshal(raw, &p) != nil || p.RunID == "" {
			return nil
		}
		row, err := a.st.GetRun(p.RunID)
		if err != nil {
			return nil // unknown run: the handler reports it
		}
		if !a.inOwnScope(rc, row.TaskID) && a.archivedTask(row.TaskID) {
			return archivedOutOfScope()
		}
	}
	return nil
}

// admit applies the admission rules to one request of an identified
// connection. It returns nil for anything it has no rule for (the S2b
// allowlist already ran) and for a caller without a managed profile.
func (a *seatAdmission) admit(rc *requestContext, method string, raw json.RawMessage) error {
	if a == nil || rc == nil || !rc.runScoped {
		return nil
	}
	if err := a.admitArchivedRead(rc, method, raw); err != nil {
		return err
	}
	switch method {
	case "task.create", "chat.create", "run.start", "run.stop", "run.setConfigOption",
		"run.setPermissionMode", "chat.queueCancel", "task.archive", "profile.get":
	default:
		return nil
	}
	seat, ok, err := a.callerSeat(rc)
	if err != nil || !ok {
		return err
	}
	// A request that does not decode is left to its handler to reject.
	switch method {
	case "task.create":
		return launchPolicy(seat)
	case "chat.create":
		var p struct {
			TaskID int64 `json:"taskId"`
		}
		if json.Unmarshal(raw, &p) != nil {
			return nil
		}
		if err := launchPolicy(seat); err != nil {
			return err
		}
		return a.requireChildTask(rc, p.TaskID)
	case "run.start":
		return a.admitRunStart(rc, seat, raw)
	case "run.stop", "run.setConfigOption", "run.setPermissionMode":
		var p struct {
			RunID string `json:"runId"`
		}
		if json.Unmarshal(raw, &p) != nil {
			return nil
		}
		st, err := a.reg.State(p.RunID)
		if errors.Is(err, runs.ErrNotFound) {
			return nil // the handler reports the unknown run
		}
		if err != nil {
			return forbidden("cannot resolve that run")
		}
		return a.requireChildTask(rc, st.TaskID)
	case "chat.queueCancel":
		var p struct {
			ItemID int64 `json:"itemId"`
		}
		if json.Unmarshal(raw, &p) != nil || p.ItemID <= 0 {
			return nil
		}
		item, err := a.st.GetChatQueueItem(p.ItemID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return forbidden("cannot resolve that queue item")
		}
		chat, err := a.st.GetChat(item.ChatID)
		if err != nil {
			return forbidden("cannot resolve that queue item")
		}
		return a.requireChildTask(rc, chat.TaskID)
	case "task.archive":
		var p struct {
			ID     int64 `json:"id"`
			TaskID int64 `json:"taskId"`
		}
		if json.Unmarshal(raw, &p) != nil {
			return nil
		}
		if p.ID == 0 {
			p.ID = p.TaskID
		}
		return a.requireChildTask(rc, p.ID)
	case "profile.get":
		var p struct {
			ID int64 `json:"id"`
		}
		if json.Unmarshal(raw, &p) != nil {
			return nil
		}
		target, err := a.profs.Get(p.ID)
		if err != nil || !mayLaunch(seat, target) {
			// Not-found and not-allowed answer alike: no existence oracle.
			return notChildProfile(p.ID)
		}
	}
	return nil
}

// launchPolicy is rule A's childPolicy "none" refusal.
func launchPolicy(seat store.AgentProfile) error {
	if seat.ChildPolicy == "none" {
		return errors.New("your seat may not launch or message other agents (childPolicy none)")
	}
	return nil
}

// admitRunStart applies rules A and B to run.start.
func (a *seatAdmission) admitRunStart(rc *requestContext, seat store.AgentProfile, raw json.RawMessage) error {
	var p struct {
		TaskID         int64  `json:"taskId"`
		Provider       string `json:"provider"`
		PermissionMode string `json:"permissionMode"`
		AutoAccept     bool   `json:"autoAccept"`
		Model          string `json:"model"`
		ProfileID      int64  `json:"profileId"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return nil
	}
	if err := launchPolicy(seat); err != nil {
		return err
	}
	if p.ProfileID <= 0 {
		return errors.New("a seat caller must pass profileId of a profile it may launch; raw provider, permissionMode and model are not allowed")
	}
	target, err := a.profs.Get(p.ProfileID)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("profile %d not found", p.ProfileID)
	}
	if err != nil {
		return forbidden("cannot load that profile")
	}
	if !mayLaunch(seat, target) {
		if seat.ChildPolicy == "profiles" {
			return fmt.Errorf("profile %d is not one of your child profiles (allowed: %s)", target.ID, joinIDs(seat.ChildProfileIDs))
		}
		return fmt.Errorf("profile %d is not one of your child profiles (childPolicy leaf allows only profiles without orchestration tools)", target.ID)
	}
	if p.Provider != target.Provider || p.PermissionMode != target.PermissionMode || p.AutoAccept != target.AutoAccept ||
		(p.Model != "" && p.Model != target.Model) {
		return fmt.Errorf("a seat caller may launch profile %d only as configured", target.ID)
	}
	return a.requireChildTask(rc, p.TaskID)
}

// wrapProfileList narrows profile.list for a run-scoped caller with a managed
// profile to the profiles it may launch (none: an empty list).
func (a *seatAdmission) wrapProfileList(next handlerFunc) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if a == nil || rc == nil || !rc.runScoped {
			return next(ctx, rc, raw)
		}
		seat, ok, err := a.callerSeat(rc)
		if err != nil {
			return nil, err
		}
		if !ok {
			return next(ctx, rc, raw)
		}
		all, err := a.profs.List()
		if err != nil {
			return nil, err
		}
		out := make([]store.AgentProfile, 0, len(all))
		for _, p := range all {
			if mayLaunch(seat, p) {
				out = append(out, p)
			}
		}
		return out, nil
	}
}
