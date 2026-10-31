package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/steward"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/validation"
	"github.com/spacingmind/smind/internal/workspace"
)

// The steward.* RPCs (ADR-0047 §5). A review is opened or cancelled only by
// the Human (a local or relay connection, never a run-scoped agent, and so
// never a task an automation started); reads are the Human's too. Only the
// seat's own run may read its digest or submit its verdict, bound to the
// caller's task before the one-time token is even compared. There is no
// merge, approve or accept RPC: a review's verdict is the seat's document
// and nothing else.

// stewardMethods lists every steward RPC; a test walks it.
var stewardMethods = []string{
	"steward.review", "steward.cancel", "steward.get", "steward.list",
	"steward.digest.get", "steward.verdict.submit",
}

// configSteward reads the steward section on every call, so a human edit
// applies without a restart. An unreadable or invalid config means steward
// is OFF, never on. Tests replace it.
var configSteward = func() (config.StewardConfig, error) {
	cfg, err := config.Load()
	return cfg.Steward, err
}

func newStewardService(db *store.Store, bus *eventBus, val *validation.Service) *steward.Service {
	return &steward.Service{
		DB: db,
		Head: func(taskID int64) (string, error) {
			t, err := db.GetTask(taskID)
			if err != nil || t.WorktreePath == nil {
				return "", fmt.Errorf("task %d has no worktree", taskID)
			}
			return workspace.GitHead(context.Background(), *t.WorktreePath)
		},
		ContractSha: func(taskID int64) string {
			rec, err := val.LatestContract(taskID)
			if err != nil {
				return ""
			}
			return rec.Sha256
		},
		Publish: func(taskID, id int64, status string) {
			if bus != nil {
				bus.Publish(Event{Topic: TopicStewardUpdated, Payload: stewardUpdatedPayload{TaskID: taskID, ReviewID: id, Status: status}})
			}
		},
	}
}

// stewardErr maps service errors onto wire codes.
func stewardErr(method string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, steward.ErrNotFound):
		return &rpcCodeError{code: codeNotFound, msg: method + ": " + err.Error(), err: err}
	case errors.Is(err, steward.ErrBadToken), errors.Is(err, steward.ErrWrongRun):
		return forbidden(method + ": " + err.Error())
	case errors.Is(err, steward.ErrVerdictExists), errors.Is(err, store.ErrStewardTokenUsed), errors.Is(err, steward.ErrInvalidVerdict):
		return &rpcCodeError{code: codeInvalid, msg: method + ": " + err.Error(), err: err}
	}
	return fmt.Errorf("%s: %w", method, err)
}

func stewardHandlers(h *stewardHost) map[string]handlerFunc {
	hs := map[string]handlerFunc{}

	hs["steward.review"] = func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		const m = "steward.review"
		if err := refuseAgent(m, rc); err != nil {
			return nil, err
		}
		var p struct {
			TaskID    int64  `json:"taskId"`
			CommitSHA string `json:"commitSha"`
		}
		if err := pluginParams(m, raw, &p); err != nil {
			return nil, err
		}
		if p.TaskID <= 0 {
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": taskId is required"}
		}
		return h.startReview(ctx, reviewParams{TaskID: p.TaskID, CommitSHA: p.CommitSHA, Trigger: steward.TriggerHuman})
	}

	hs["steward.cancel"] = func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		const m = "steward.cancel"
		if err := refuseAgent(m, rc); err != nil {
			return nil, err
		}
		var p struct {
			ReviewID int64 `json:"reviewId"`
		}
		if err := pluginParams(m, raw, &p); err != nil {
			return nil, err
		}
		if err := h.cancel(p.ReviewID); err != nil {
			return nil, err
		}
		v, err := h.svc.View(p.ReviewID)
		return v, stewardErr(m, err)
	}

	hs["steward.get"] = func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		const m = "steward.get"
		if err := refuseAgent(m, rc); err != nil {
			return nil, err
		}
		var p struct {
			ReviewID int64 `json:"reviewId"`
		}
		if err := pluginParams(m, raw, &p); err != nil {
			return nil, err
		}
		v, err := h.svc.View(p.ReviewID)
		return v, stewardErr(m, err)
	}

	hs["steward.list"] = func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		const m = "steward.list"
		if err := refuseAgent(m, rc); err != nil {
			return nil, err
		}
		var p struct {
			TaskID int64 `json:"taskId"`
			Limit  int   `json:"limit"`
		}
		if err := pluginParams(m, raw, &p); err != nil {
			return nil, err
		}
		l, err := h.svc.List(p.TaskID, p.Limit)
		if err != nil {
			return nil, stewardErr(m, err)
		}
		if l == nil {
			l = []steward.View{}
		}
		return l, nil
	}

	hs["steward.digest.get"] = func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		const m = "steward.digest.get"
		if rc == nil || !rc.runScoped {
			return nil, forbidden(m + " is only available to the steward seat's own run")
		}
		var p struct {
			ReviewID int64 `json:"reviewId"`
		}
		if err := pluginParams(m, raw, &p); err != nil {
			return nil, err
		}
		// The row must be this run's own open review: anything else answers
		// forbidden alike, so no other run learns whether a review exists.
		row, err := h.db.GetStewardReview(p.ReviewID)
		if err != nil || row.StewardTaskID != rc.ident.taskID || (row.Status != "pending" && row.Status != "running") {
			return nil, forbidden(m + " is only available to the steward seat's own run")
		}
		d, err := h.svc.DigestJSON(p.ReviewID)
		return d, stewardErr(m, err)
	}

	hs["steward.verdict.submit"] = func(_ context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		const m = "steward.verdict.submit"
		if rc == nil || !rc.runScoped {
			return nil, forbidden(m + " is only available to the steward seat's own run")
		}
		var p struct {
			ReviewID int64           `json:"reviewId"`
			Token    string          `json:"token"`
			Verdict  json.RawMessage `json:"verdict"`
		}
		if err := pluginParams(m, raw, &p); err != nil {
			return nil, err
		}
		if p.ReviewID <= 0 || len(p.Verdict) == 0 {
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": reviewId and verdict are required"}
		}
		// Defence in depth: a Lead's run is refused here, before its token
		// is even compared.
		row, err := h.db.GetStewardReview(p.ReviewID)
		if err != nil || row.StewardTaskID != rc.ident.taskID {
			return nil, forbidden(m + " is only available to the steward seat's own run")
		}
		v, err := h.svc.SubmitVerdict(p.ReviewID, p.Token, rc.ident.runID, p.Verdict)
		return v, stewardErr(m, err)
	}

	return hs
}

// stewardObserver maps a finished run onto the auto trigger (ADR-0047 §4).
// It never blocks: the trigger's Fire spawns the review in its own
// goroutine.
type stewardObserver struct {
	host    *stewardHost
	trigger *steward.AutoTrigger
	db      *store.Store
}

func newStewardObserver(h *stewardHost, db *store.Store) *stewardObserver {
	return &stewardObserver{
		host: h,
		db:   db,
		trigger: steward.NewAutoTrigger(configSteward, func(taskID int64) {
			go func() {
				if _, err := h.startReview(context.Background(), reviewParams{TaskID: taskID, Trigger: steward.TriggerAuto}); err != nil {
					slog.Warn("steward auto review", "task", taskID, "err", err)
				}
			}()
		}),
	}
}

// OnRunFinished hands a finished user run to the auto trigger with the facts
// it judges: the run's profile name, whether the task is a root task with a
// pushed branch and a readable head, and its workspace title.
func (o *stewardObserver) OnRunFinished(f runs.RunFinished) {
	if p := f.Purpose; p != "" && p != store.RunPurposeUser {
		return
	}
	if f.Status != "done" {
		return
	}
	task, err := o.db.GetTask(f.TaskID)
	if err != nil {
		return
	}
	name := ""
	if run, err := o.db.GetRun(f.RunID); err == nil && run.ProfileID != 0 {
		if prof, err := o.db.GetAgentProfile(run.ProfileID); err == nil {
			name = prof.Name
		}
	}
	wsTitle := ""
	if ws, err := o.db.GetWorkspace(task.WorkspaceID); err == nil {
		wsTitle = ws.Title
	}
	hasHead := false
	if task.WorktreePath != nil {
		if _, err := validation.GitHead(context.Background(), *task.WorktreePath); err == nil {
			hasHead = true
		}
	}
	parent := int64(0)
	if task.ParentTaskID != nil {
		parent = *task.ParentTaskID
	}
	branch := ""
	if task.Branch != nil {
		branch = *task.Branch
	}
	o.trigger.OnRunFinished(steward.RunDone{
		RunID: f.RunID, TaskID: f.TaskID, Status: f.Status, ProfileName: name,
		ParentTaskID: parent, Branch: branch, HasHead: hasHead, WorkspaceTitle: wsTitle,
	})
}
