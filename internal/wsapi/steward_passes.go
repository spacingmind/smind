package wsapi

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/steward"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/validation"
	"github.com/spacingmind/smind/internal/workspace"
)

// This file is the two-axis review's seat side (ADR-0054): one review, two
// passes, each its own seat run (own scratch worktree, task, chat, one-time
// token and Watch). Everything here runs only when steward.twoAxis was on at
// collection time; with it off startReview takes the single-seat path
// untouched.

// passProfile resolves the seat profile a pass runs on: the configured name
// (steward-standards by default), falling back to the steward profile when
// that seat was never seeded. The pass row records the model actually used.
func (h *stewardHost) passProfile(name string) (store.AgentProfile, error) {
	ps, err := h.profReg.List()
	if err != nil {
		return store.AgentProfile{}, err
	}
	var fallback *store.AgentProfile
	for i := range ps {
		switch ps[i].Name {
		case name:
			return ps[i], nil
		case steward.SeatProfileName:
			fallback = &ps[i]
		}
	}
	if fallback != nil {
		return *fallback, nil
	}
	return store.AgentProfile{}, fmt.Errorf("no %q or %q profile: run `smind profile seed roles`", name, steward.SeatProfileName)
}

// openPasses creates the review's two pass rows (pending), each with the
// profile and model it will run on.
func (h *stewardHost) openPasses(reviewID int64, cfg config.StewardConfig) error {
	std, spec := cfg.PassLimits()
	for _, p := range []struct {
		axis   steward.Axis
		limits config.StewardPassLimits
	}{{steward.AxisStandards, std}, {steward.AxisSpec, spec}} {
		prof, err := h.passProfile(p.limits.Profile)
		if err != nil {
			return err
		}
		if _, err := h.db.InsertStewardReviewPass(store.StewardReviewPass{
			ReviewID: reviewID, Axis: string(p.axis), Status: steward.PassPending, Profile: prof.Name, Model: prof.Model,
		}, time.Now()); err != nil {
			return err
		}
	}
	return nil
}

// startPasses starts the open passes of review id once no gate is unknown:
// a Spec pass with no spec to judge is skipped ("no spec available"), a
// Standards pass whose merge-base is unknown is blocked rather than judged
// against the head (D11), and the rest start together.
func (h *stewardHost) startPasses(id int64, lead store.Task, d steward.Digest, dsha, sha string, cfg config.StewardConfig) (reviewResult, error) {
	res := reviewResult{ReviewID: id, TaskID: lead.ID, CommitSHA: sha, Status: "running"}
	std, spec := cfg.PassLimits()
	before, _ := validation.Take(h.ctx, *lead.WorktreePath)
	if err := h.db.SetStewardReviewStatus(id, "running"); err != nil {
		return res, err
	}

	if !d.Spec.HasSpec() {
		if err := h.svc.WritePassSkipped(id, steward.AxisSpec); err != nil && !errors.Is(err, steward.ErrVerdictExists) {
			return res, err
		}
	}
	if d.Standards == nil || d.Standards.BaseRef == "" {
		if err := h.svc.WritePassBlocked(id, steward.AxisStandards, "crashed", "the merge-base could not be resolved, so the standards were not read", 0); err != nil && !errors.Is(err, steward.ErrVerdictExists) {
			return res, err
		}
	}
	for _, p := range []struct {
		axis   steward.Axis
		limits config.StewardPassLimits
	}{{steward.AxisStandards, std}, {steward.AxisSpec, spec}} {
		pass, err := h.db.GetStewardReviewPass(id, string(p.axis))
		if err != nil || pass.Status != steward.PassPending {
			continue
		}
		if err := h.startPassSeat(id, p.axis, p.limits, lead, d, dsha, sha, before); err != nil {
			_ = h.svc.WritePassBlocked(id, p.axis, "crashed", "the steward seat could not be started", 0)
		}
	}
	return res, nil
}

// startPassSeat mints the pass's one-time token, creates its scratch task at
// sha, puts the Standards pass's base-branch copies in it and starts the
// seat's run under the pass's own watcher. The seat entry exists before
// reg.Start so the permission floor and the MCP env hook are there for the
// run's first permission.
func (h *stewardHost) startPassSeat(id int64, axis steward.Axis, limits config.StewardPassLimits, lead store.Task, d steward.Digest, dsha, sha string, before validation.Snapshot) error {
	prof, err := h.passProfile(limits.Profile)
	if err != nil {
		return err
	}
	token, err := steward.NewToken()
	if err != nil {
		return err
	}
	if err := h.db.SetStewardPassToken(id, string(axis), steward.HashToken(token)); err != nil {
		return err
	}
	task, err := h.wm.CreateTask(lead.WorkspaceID, lead.SpaceID, fmt.Sprintf("steward review #%d (%s)", id, axis), workspace.WithParentTask(lead.ID), workspace.WithBaseRef(sha))
	if err != nil {
		return err
	}
	if task.WorktreePath == nil {
		return errors.New("the steward scratch worktree was not created")
	}
	chat, err := h.wm.DefaultChat(task.ID)
	if err != nil {
		return err
	}
	if err := h.db.SetStewardPassRun(id, string(axis), "", task.ID, *task.WorktreePath); err != nil {
		return err
	}
	if axis == steward.AxisStandards {
		// D11: the bar is the merge-base's text, copied into the scratch tree
		// outside the index; the head's standards files are never the bar.
		if err := steward.WriteStandardsCopies(h.ctx, *lead.WorktreePath, d.Standards.BaseRef, *task.WorktreePath, d.Standards.FileInputs()); err != nil {
			return err
		}
	}
	h.mu.Lock()
	h.seats[task.ID] = stewardSeatEntry{reviewID: id, axis: axis, token: token, scratch: *task.WorktreePath}
	h.mu.Unlock()
	if err := h.db.SetStewardPassStatus(id, string(axis), steward.PassRunning); err != nil {
		h.forget(task.ID)
		return err
	}
	opts := []runs.StartOption{runs.WithOrigin(store.ChatQueueSourceHuman), runs.WithProfile(prof.ID)}
	if prof.Model != "" {
		opts = append(opts, runs.WithModel(prof.Model))
	}
	runID, err := h.reg.Start(context.Background(), h.wm, h.runner, task.ID, chat.ID, taskrunner.Provider(prof.Provider),
		steward.BuildAxisPrompt(d, axis, id, dsha),
		// AutoAccept is never on: the seat floor decides every request.
		taskrunner.PermissionSettings{Mode: prof.PermissionMode, AutoAccept: false},
		taskrunner.ThinkingLevel(prof.ThinkingLevel), opts...)
	if err != nil {
		h.forget(task.ID)
		return err
	}
	_ = h.db.SetStewardPassRun(id, string(axis), runID, task.ID, *task.WorktreePath)

	w := &steward.Watch{
		Svc: h.svc, ReviewID: id, StewardTaskID: task.ID, Axis: axis,
		MaxMinutes: limits.MaxMinutes, BudgetUSD: limits.BudgetUSD,
		Probe: h.probe, Stop: func() { h.stopTask(task.ID) },
		Before: before,
		LeadSnapshot: func() (validation.Snapshot, error) {
			return validation.Take(context.Background(), *lead.WorktreePath)
		},
	}
	go func() {
		defer h.forget(task.ID)
		w.Run(h.ctx, h.watchPeriod)
	}()
	return nil
}

// cancelPasses ends a two-axis review's open passes as cancelled and stops
// their runs. The review row is already blocked by the caller, and every pass
// row is written BEFORE any run is stopped: a watch must never see its run
// end first and record a crash, which would also count toward the auto-pause.
func (h *stewardHost) cancelPasses(reviewID int64) {
	passes, err := h.db.ListStewardReviewPasses(reviewID)
	if err != nil {
		return
	}
	now := time.Now()
	for _, p := range passes {
		if p.Status == steward.PassPending || p.Status == steward.PassRunning {
			_ = h.db.SetStewardPassBlocked(reviewID, p.Axis, "cancelled", "cancelled by the Human", p.CostUSD, now)
		}
	}
	for _, p := range passes {
		if p.StewardTaskID != 0 {
			h.stopTask(p.StewardTaskID)
			h.forget(p.StewardTaskID)
		}
	}
}

// passOf reports the pass of review reviewID whose seat is task taskID, while
// that pass is open. It is how the seat's RPCs bind a call to the caller's
// own pass: a task that is no pass's seat gets nothing.
func (h *stewardHost) passOf(reviewID, taskID int64) (steward.Axis, bool) {
	passes, err := h.db.ListStewardReviewPasses(reviewID)
	if err != nil {
		return "", false
	}
	for _, p := range passes {
		if p.StewardTaskID == taskID && taskID != 0 && (p.Status == steward.PassPending || p.Status == steward.PassRunning) {
			return steward.Axis(p.Axis), true
		}
	}
	return "", false
}
