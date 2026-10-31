package serve

import (
	"encoding/json"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spacingmind/smind/internal/fleet/registry"
	"github.com/spacingmind/smind/internal/fleet/wire"
	"github.com/spacingmind/smind/internal/store"
)

// codeRunBusy names the refusal a Backend's StartRun returns when the task's
// chat is busy; it lives here so the write path, the adapter and the tests
// agree on the literal.
const codeRunBusy = "run_busy"

// defaultPollInterval is the fleet.run.wait poll period when
// Options.PollInterval is zero.
const defaultPollInterval = 500 * time.Millisecond

// TaskLookup is the owner and latest-run lookups of the write path: send and
// stop decide on the task rows and whose they are. It is the read Source's
// subset, so one wsapi adapter satisfies both.
type TaskLookup interface {
	GetTask(id int64) (store.Task, error)
	TaskOrigin(taskID int64) (host, trace string, err error)
	LatestRun(taskID int64) (store.Run, bool, error)
}

// write serves one write call. The rate limit and the provenance checks have
// already run; what remains is decode, grant, ownership, idempotency, budget
// and the effect.
func (s *Server) write(principal, method string, raw json.RawMessage, now time.Time, row *store.FleetAuditRow) (any, error) {
	switch method {
	case wire.MethodTaskCreate:
		var p wire.TaskCreateParams
		if err := decodeWrite(raw, &p); err != nil {
			return nil, err
		}
		if err := p.Validate(); err != nil {
			return nil, refuse(wire.CodeInvalid, err.Error())
		}
		return s.createTask(principal, method, p, now, row)
	case wire.MethodTaskSend:
		var p wire.TaskSendParams
		if err := decodeWrite(raw, &p); err != nil {
			return nil, err
		}
		if err := p.Validate(); err != nil {
			return nil, refuse(wire.CodeInvalid, err.Error())
		}
		return s.sendTask(principal, method, p, now, row)
	case wire.MethodTaskStop:
		var p wire.TaskStopParams
		if err := decodeWrite(raw, &p); err != nil {
			return nil, err
		}
		if err := p.Validate(); err != nil {
			return nil, refuse(wire.CodeInvalid, err.Error())
		}
		return s.stopTask(principal, p, row)
	default:
		return nil, refuse(wire.CodeInvalid, "unknown method")
	}
}

// decodeWrite unmarshals the whole request into v; every write params type
// embeds Prov, so one decode covers provenance and params alike.
func decodeWrite(raw json.RawMessage, v any) error {
	if err := json.Unmarshal(raw, v); err != nil {
		return refuse(wire.CodeInvalid, "malformed params")
	}
	return nil
}

// createTask is fleet.task.create: the one method that makes a task, with the
// caller's provider, model and prompt but the callee's own accounts and
// settings (ADR-0048 D6). The prompt body travels to the Backend and nowhere
// else: not into an error, not into the audit row.
func (s *Server) createTask(principal, method string, p wire.TaskCreateParams, now time.Time, row *store.FleetAuditRow) (any, error) {
	g, err := s.opts.Authorizer.Authorize(principal, registry.OpCreate, p.WorkspaceID)
	if err != nil {
		return nil, refuse(wire.CodeNotGranted, "not granted")
	}
	// An empty provider list means every provider the callee configured; a
	// closed list is a human's choice and is enforced before anything else.
	if len(g.Providers) > 0 && !slices.Contains(g.Providers, p.Provider) {
		return nil, refuse(wire.CodeNotGranted, "not granted")
	}
	// Held from the idempotency lookup to the idempotency store, so the
	// budget check and the start cannot interleave between two concurrent
	// calls and both pass maxLiveAgents.
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	res, replayed, err := idemReplay[wire.TaskCreateResult](s.opts.Ledger, principal, method, p.IdempotencyKey, now, row)
	if err != nil {
		return nil, err
	}
	if replayed {
		res.Replayed = true
		return res, nil
	}
	if err := s.checkBudget(principal, g); err != nil {
		return nil, err
	}
	if !s.opts.Backend.ProviderConfigured(p.Provider) {
		return nil, refuse(wire.CodeProviderNotConfigured, "provider is not configured on this host")
	}
	title := p.Title
	if strings.TrimSpace(title) == "" {
		title = "remote task"
	}
	ti, err := s.opts.Backend.CreateTask(p.WorkspaceID, title, p.Branch, p.BaseRef)
	if err != nil {
		return nil, err
	}
	row.TaskID = strconv.FormatInt(ti.ID, 10)
	if err := s.opts.Backend.SetTaskOrigin(ti.ID, principal, p.Prov.Trace); err != nil {
		return nil, err
	}
	runID, err := s.opts.Backend.StartRun(ti.ID, p.Provider, p.Model, p.Prompt, principal)
	if err != nil {
		return nil, err
	}
	row.RunID = runID
	out := wire.TaskCreateResult{TaskID: ti.ID, RunID: runID, Branch: ti.Branch}
	if err := s.storeIdem(principal, method, p.IdempotencyKey, row.TaskID, runID, out, now); err != nil {
		return nil, err
	}
	return out, nil
}

// sendTask is fleet.task.send: a follow-up prompt on a task this host
// created, re-using the provider and model of the run that created it -- a
// remote caller cannot retarget a task onto another provider.
func (s *Server) sendTask(principal, method string, p wire.TaskSendParams, now time.Time, row *store.FleetAuditRow) (any, error) {
	task, err := s.opts.Tasks.GetTask(p.TaskID)
	if err != nil {
		if isNotFound(err) {
			return nil, refuse(wire.CodeNotFound, "not found")
		}
		return nil, err
	}
	row.TaskID = strconv.FormatInt(task.ID, 10)
	g, err := s.opts.Authorizer.Authorize(principal, registry.OpSend, task.WorkspaceID)
	if err != nil {
		return nil, refuse(wire.CodeNotGranted, "not granted")
	}
	// The task must be this principal's: a local task (empty origin) is not
	// sendable either, it was never the fleet's to drive.
	host, _, _ := s.opts.Tasks.TaskOrigin(task.ID)
	if host != principal {
		return nil, refuse(wire.CodeNotOwner, "task was not created by this host")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	res, replayed, err := idemReplay[wire.TaskSendResult](s.opts.Ledger, principal, method, p.IdempotencyKey, now, row)
	if err != nil {
		return nil, err
	}
	if replayed {
		res.Replayed = true
		return res, nil
	}
	if err := s.checkBudget(principal, g); err != nil {
		return nil, err
	}
	prev, ok, err := s.opts.Tasks.LatestRun(task.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, refuse(wire.CodeInvalid, "task has no run to continue")
	}
	if !s.opts.Backend.ProviderConfigured(prev.Provider) {
		return nil, refuse(wire.CodeProviderNotConfigured, "provider is not configured on this host")
	}
	runID, err := s.opts.Backend.StartRun(task.ID, prev.Provider, prev.Model, p.Prompt, principal)
	if err != nil {
		return nil, err
	}
	row.RunID = runID
	out := wire.TaskSendResult{TaskID: task.ID, RunID: runID}
	if err := s.storeIdem(principal, method, p.IdempotencyKey, row.TaskID, runID, out, now); err != nil {
		return nil, err
	}
	return out, nil
}

// stopTask is fleet.task.stop: only the host that created the task may stop
// it, and without a runId only its live run.
func (s *Server) stopTask(principal string, p wire.TaskStopParams, row *store.FleetAuditRow) (any, error) {
	task, err := s.opts.Tasks.GetTask(p.TaskID)
	if err != nil {
		if isNotFound(err) {
			return nil, refuse(wire.CodeNotFound, "not found")
		}
		return nil, err
	}
	row.TaskID = strconv.FormatInt(task.ID, 10)
	if _, err := s.opts.Authorizer.Authorize(principal, registry.OpStop, task.WorkspaceID); err != nil {
		return nil, refuse(wire.CodeNotGranted, "not granted")
	}
	host, _, _ := s.opts.Tasks.TaskOrigin(task.ID)
	if host != principal {
		return nil, refuse(wire.CodeNotOwner, "task was not created by this host")
	}
	latest, ok, err := s.opts.Tasks.LatestRun(task.ID)
	if err != nil {
		return nil, err
	}
	runID := p.RunID
	if runID == "" {
		if ok && latest.Status == "running" {
			runID = latest.ID
		}
	} else if !ok || latest.ID != runID {
		// TaskLookup has no GetRun, so a given runId can only be checked
		// against the task's live run; anything else is not this task's.
		return nil, refuse(wire.CodeInvalid, "runId does not belong to the task's live run")
	}
	if runID == "" {
		return wire.TaskStopResult{Stopped: false}, nil
	}
	row.RunID = runID
	if err := s.opts.Backend.StopRun(runID); err != nil {
		return nil, err
	}
	return wire.TaskStopResult{Stopped: true}, nil
}

// idemReplay returns the stored response of a previous create or send for
// the same key, with the caller's audit row pointed at the stored ids. A key
// past the window is a miss, so the request re-executes; the bool says the
// response was replayed and nothing was done.
func idemReplay[T any](l Ledger, principal, method, key string, now time.Time, row *store.FleetAuditRow) (T, bool, error) {
	var zero T
	idem, ok, err := l.GetFleetIdem(principal, key)
	if err != nil {
		return zero, false, err
	}
	if !ok || now.Sub(idem.CreatedAt) > wire.IdempotencyWindow {
		return zero, false, nil
	}
	if idem.Method != method {
		return zero, false, refuse(wire.CodeInvalid, "idempotencyKey was used for another method")
	}
	var res T
	if err := json.Unmarshal([]byte(idem.Response), &res); err != nil {
		return zero, false, err
	}
	row.TaskID, row.RunID = idem.TaskID, idem.RunID
	return res, true, nil
}

// storeIdem records a response after its effect. created=false means a
// same-key row already won; its response stands and this one is dropped.
func (s *Server) storeIdem(principal, method, key, taskID, runID string, res any, now time.Time) error {
	b, err := json.Marshal(res)
	if err != nil {
		return err
	}
	_, _, err = s.opts.Ledger.PutFleetIdem(store.FleetIdem{
		HostID: principal, Key: key, Method: method, TaskID: taskID, RunID: runID,
		Response: string(b), CreatedAt: now,
	})
	return err
}
