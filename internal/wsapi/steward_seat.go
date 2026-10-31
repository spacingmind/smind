package wsapi

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/profiles"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/steward"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/validation"
	"github.com/spacingmind/smind/internal/workspace"
)

// TopicStewardUpdated carries {taskId, reviewId, status}: a review was
// opened ("pending"), its seat started ("running"), a verdict was written
// ("done") or the daemon blocked it ("blocked"; the reason is in the row).
const TopicStewardUpdated = "steward.updated"

type stewardUpdatedPayload struct {
	TaskID   int64  `json:"taskId"`
	ReviewID int64  `json:"reviewId"`
	Status   string `json:"status"`
}

// stewardCollectFunc gathers a task's review inputs at sha (the collector
// lives in its own file; tests inject a fake).
type stewardCollectFunc func(ctx context.Context, taskID int64, sha string) (steward.Inputs, error)

// stewardRecheckMaxWait bounds how long a review stays pending on an
// unknown (pending CI) gate before the daemon gives up on it.
const stewardRecheckMaxWait = 6 * time.Hour

type stewardHostOptions struct {
	Svc     *steward.Service
	Rails   *steward.Rails
	Collect stewardCollectFunc
	WM      *workspace.Manager
	DB      *store.Store
	Reg     *runs.Registry
	Runner  *taskrunner.Runner
	ProfReg *profiles.Registry
	Ctx     context.Context
	// WatchPeriod supervises a running seat, RecheckPeriod polls a pending
	// CI gate. Zero means the default (2s / 30s); a nil Ctx is
	// context.Background().
	WatchPeriod   time.Duration
	RecheckPeriod time.Duration
}

// stewardSeatEntry is one live seat: its review, its one-time verdict token
// (memory only) and its scratch directory.
type stewardSeatEntry struct {
	reviewID int64
	token    string
	scratch  string
}

// stewardHost starts steward reviews, supervises their seats and exposes the
// seat source to the runs registry (ADR-0047 §1, §5). The one-time verdict
// tokens live in memory only and reach their seat's MCP entry through the
// registry hook and nothing else: never the prompt, never a row, never a
// reply. No RPC handler lives here.
type stewardHost struct {
	svc     *steward.Service
	rails   *steward.Rails
	collect stewardCollectFunc
	wm      *workspace.Manager
	db      *store.Store
	reg     *runs.Registry
	runner  *taskrunner.Runner
	profReg *profiles.Registry

	mu    sync.Mutex
	seats map[int64]stewardSeatEntry // by steward task id

	watchPeriod   time.Duration
	recheckPeriod time.Duration
	ctx           context.Context
}

func newStewardHost(o stewardHostOptions) *stewardHost {
	if o.WatchPeriod <= 0 {
		o.WatchPeriod = 2 * time.Second
	}
	if o.RecheckPeriod <= 0 {
		o.RecheckPeriod = 30 * time.Second
	}
	if o.Ctx == nil {
		o.Ctx = context.Background()
	}
	h := &stewardHost{svc: o.Svc, rails: o.Rails, collect: o.Collect, wm: o.WM, db: o.DB, reg: o.Reg, runner: o.Runner, profReg: o.ProfReg,
		seats: map[int64]stewardSeatEntry{}, watchPeriod: o.WatchPeriod, recheckPeriod: o.RecheckPeriod, ctx: o.Ctx}
	o.Reg.SetSeatSource(h.seatSource)
	// The tokens are memory-only, so an open review can never resume after a
	// restart: close every row the previous daemon left open.
	if rows, err := o.DB.ListStaleOpenStewardReviews(); err == nil {
		for _, row := range rows {
			_ = o.Svc.WriteBlocked(row.ID, "crashed", "the daemon restarted while the review was open", 0)
		}
	}
	return h
}

type reviewParams struct {
	TaskID    int64
	CommitSHA string
	Trigger   string // steward.TriggerHuman | steward.TriggerAuto
}

type reviewResult struct {
	ReviewID      int64  `json:"reviewId"`
	TaskID        int64  `json:"taskId"`
	CommitSHA     string `json:"commitSha"`
	Status        string `json:"status"`
	Existing      bool   `json:"existing,omitempty"`
	StewardTaskID int64  `json:"stewardTaskId,omitempty"`
	ChatID        int64  `json:"chatId,omitempty"`
	RunID         string `json:"runId,omitempty"`
}

// startReview opens a review of task p.TaskID at p.CommitSHA ("" = the
// branch head) and starts its seat unless a gate is still unknown, in which
// case the review stays pending and a recheck waits the CI out.
func (h *stewardHost) startReview(ctx context.Context, p reviewParams) (reviewResult, error) {
	const m = "steward.review"
	lead, err := h.db.GetTask(p.TaskID)
	if err != nil || lead.WorktreePath == nil {
		return reviewResult{}, &rpcCodeError{code: codeNotFound, msg: fmt.Sprintf("%s: task %d has no worktree", m, p.TaskID)}
	}
	head, err := validation.GitHead(ctx, *lead.WorktreePath)
	if err != nil {
		return reviewResult{}, fmt.Errorf("%s: read the branch head: %w", m, err)
	}
	sha := p.CommitSHA
	if sha == "" {
		sha = head
	} else {
		if !validation.IsSHA(sha) {
			return reviewResult{}, &rpcCodeError{code: codeInvalid, msg: m + ": commitSha must be a full 40-hex commit id"}
		}
		ok, err := validation.IsAncestor(ctx, *lead.WorktreePath, sha, head)
		if err != nil || !ok {
			return reviewResult{}, &rpcCodeError{code: codeInvalid, msg: m + ": commitSha is not on the task branch"}
		}
	}

	prof, err := h.seatProfile()
	if err != nil {
		return reviewResult{}, &rpcCodeError{code: codeInvalid, msg: m + ": " + err.Error()}
	}

	in, err := h.collect(ctx, p.TaskID, sha)
	if err != nil {
		return reviewResult{}, fmt.Errorf("%s: collect the review inputs: %w", m, err)
	}
	d := steward.Build(in)
	data, dsha, err := d.Marshal()
	if err != nil {
		return reviewResult{}, fmt.Errorf("%s: build the digest: %w", m, err)
	}
	contractSha := h.contractShaOf(p.TaskID)

	if err := h.rails.Check(steward.RailRequest{
		Key:     steward.ReviewKey{TaskID: p.TaskID, HeadSHA: sha, ContractSha: contractSha, CIState: in.CI.State},
		Trigger: p.Trigger,
	}); err != nil {
		var dup *steward.DuplicateError
		if errors.As(err, &dup) {
			status := ""
			if row, gerr := h.db.GetStewardReview(dup.ReviewID); gerr == nil {
				status = row.Status
			}
			return reviewResult{ReviewID: dup.ReviewID, TaskID: p.TaskID, CommitSHA: sha, Status: status, Existing: true}, nil
		}
		if errors.Is(err, steward.ErrDisabled) {
			return reviewResult{}, &rpcCodeError{code: codeForbidden, msg: m + ": " + err.Error()}
		}
		return reviewResult{}, &rpcCodeError{code: codeInvalid, msg: m + ": " + err.Error()}
	}

	row, err := h.db.InsertStewardReview(store.StewardReview{TaskID: p.TaskID, TriggerKind: p.Trigger, HeadSHA: sha, Status: "pending"}, time.Now())
	if err != nil {
		if errors.Is(err, store.ErrStewardReviewOpen) {
			return reviewResult{}, &rpcCodeError{code: codeInvalid, msg: m + ": this task already has an open steward review"}
		}
		return reviewResult{}, fmt.Errorf("%s: open the review: %w", m, err)
	}
	id := row.ID
	validationID := int64(0)
	if in.Validation != nil {
		validationID = in.Validation.ID
	}
	if err := h.db.SetStewardReviewDigest(id, string(data), dsha, in.CI.State, "" /*policySha*/, contractSha, validationID); err != nil {
		_ = h.svc.WriteBlocked(id, "crashed", "the digest could not be stored", 0)
		return reviewResult{}, fmt.Errorf("%s: store the digest: %w", m, err)
	}
	if h.svc.Publish != nil {
		h.svc.Publish(p.TaskID, id, "pending")
	}

	if stewardGateUnknown(d.Gates) {
		go h.recheck(id, p.TaskID, lead, prof, sha)
		return reviewResult{ReviewID: id, TaskID: p.TaskID, CommitSHA: sha, Status: "pending"}, nil
	}
	return h.startSeat(id, lead, prof, d, dsha, sha)
}

// recheck waits out an unknown (pending CI) gate: every recheckPeriod it
// re-collects the inputs and starts the seat once no gate is unknown any
// more. A head that moves, a review that ends, or six hours of pending CI
// end it. It stops with h.ctx.
func (h *stewardHost) recheck(id, taskID int64, lead store.Task, prof store.AgentProfile, sha string) {
	t := time.NewTicker(h.recheckPeriod)
	defer t.Stop()
	started := time.Now()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-t.C:
		}
		row, err := h.db.GetStewardReview(id)
		if err != nil || (row.Status != "pending" && row.Status != "running") {
			return
		}
		if head, err := validation.GitHead(context.Background(), *lead.WorktreePath); err != nil || head != sha {
			_ = h.svc.WriteBlocked(id, "cancelled", "the head moved while waiting for CI", row.CostUSD)
			return
		}
		if time.Since(started) >= stewardRecheckMaxWait {
			_ = h.svc.WriteBlocked(id, "timeout", "CI was still pending after six hours", row.CostUSD)
			return
		}
		in, err := h.collect(context.Background(), taskID, sha)
		if err != nil {
			continue
		}
		d := steward.Build(in)
		data, dsha, err := d.Marshal()
		if err != nil {
			continue
		}
		if stewardGateUnknown(d.Gates) {
			continue
		}
		validationID := int64(0)
		if in.Validation != nil {
			validationID = in.Validation.ID
		}
		if err := h.db.SetStewardReviewDigest(id, string(data), dsha, in.CI.State, "" /*policySha*/, h.contractShaOf(taskID), validationID); err != nil {
			return
		}
		_, _ = h.startSeat(id, lead, prof, d, dsha, sha)
		return
	}
}

// startSeat mints the one-time token, creates the scratch task at sha and
// starts the seat's run under the watcher. The seat entry is stored before
// reg.Start so the permission floor and the MCP env hook exist the moment
// the run asks for its first permission.
func (h *stewardHost) startSeat(id int64, lead store.Task, prof store.AgentProfile, d steward.Digest, dsha, sha string) (reviewResult, error) {
	res := reviewResult{ReviewID: id, TaskID: lead.ID, CommitSHA: sha, Status: "running"}
	fail := func(err error) (reviewResult, error) {
		_ = h.svc.WriteBlocked(id, "crashed", "the steward seat could not be started", 0)
		return reviewResult{}, err
	}

	before, _ := validation.Take(h.ctx, *lead.WorktreePath)
	token, err := steward.NewToken()
	if err != nil {
		return fail(err)
	}
	if err := h.db.SetStewardReviewToken(id, steward.HashToken(token)); err != nil {
		return fail(err)
	}
	title := fmt.Sprintf("steward review #%d", id)
	if len(title) > 120 {
		title = title[:120]
	}
	task, err := h.wm.CreateTask(lead.WorkspaceID, lead.SpaceID, title, workspace.WithParentTask(lead.ID), workspace.WithBaseRef(sha))
	if err != nil {
		return fail(err)
	}
	if task.WorktreePath == nil {
		return fail(errors.New("the steward scratch worktree was not created"))
	}
	chat, err := h.wm.DefaultChat(task.ID)
	if err != nil {
		return fail(err)
	}
	if err := h.db.SetStewardReviewRun(id, "", task.ID, *task.WorktreePath); err != nil {
		return fail(err)
	}
	h.mu.Lock()
	h.seats[task.ID] = stewardSeatEntry{reviewID: id, token: token, scratch: *task.WorktreePath}
	h.mu.Unlock()
	if err := h.db.SetStewardReviewStatus(id, "running"); err != nil {
		h.forget(task.ID)
		return fail(err)
	}
	opts := []runs.StartOption{runs.WithOrigin(store.ChatQueueSourceHuman), runs.WithProfile(prof.ID)}
	if prof.Model != "" {
		opts = append(opts, runs.WithModel(prof.Model))
	}
	runID, err := h.reg.Start(context.Background(), h.wm, h.runner, task.ID, chat.ID, taskrunner.Provider(prof.Provider),
		steward.BuildPrompt(d, id, dsha),
		// AutoAccept is never on: the seat floor decides every request.
		taskrunner.PermissionSettings{Mode: prof.PermissionMode, AutoAccept: false},
		taskrunner.ThinkingLevel(prof.ThinkingLevel), opts...)
	if err != nil {
		h.forget(task.ID)
		return fail(err)
	}
	_ = h.db.SetStewardReviewRun(id, runID, task.ID, *task.WorktreePath)

	usd, minutes := steward.EffectiveBudget(steward.RailRequest{}, h.rails.Limits())
	w := &steward.Watch{
		Svc: h.svc, ReviewID: id, StewardTaskID: task.ID,
		MaxMinutes: minutes, BudgetUSD: usd,
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
	res.StewardTaskID, res.ChatID, res.RunID = task.ID, chat.ID, runID
	return res, nil
}

// cancel ends an open review as cancelled by the Human, stopping its seat.
func (h *stewardHost) cancel(reviewID int64) error {
	row, err := h.db.GetStewardReview(reviewID)
	if errors.Is(err, sql.ErrNoRows) {
		return &rpcCodeError{code: codeNotFound, msg: fmt.Sprintf("steward.review.cancel: review %d not found", reviewID)}
	}
	if err != nil {
		return err
	}
	if row.Status != "pending" && row.Status != "running" {
		return &rpcCodeError{code: codeInvalid, msg: "steward.review.cancel: review is not open"}
	}
	if row.StewardTaskID != 0 {
		h.stopTask(row.StewardTaskID)
		h.forget(row.StewardTaskID)
	}
	return h.svc.WriteBlocked(reviewID, "cancelled", "cancelled by the Human", row.CostUSD)
}

// seatSource is the runs registry hook: the seat spec of a steward task,
// only while its review is open. The spec is policy, never logged.
func (h *stewardHost) seatSource(taskID int64) (runs.SeatSpec, bool) {
	h.mu.Lock()
	e, ok := h.seats[taskID]
	h.mu.Unlock()
	if !ok {
		return runs.SeatSpec{}, false
	}
	row, err := h.db.GetStewardReview(e.reviewID)
	if err != nil || (row.Status != "pending" && row.Status != "running") {
		return runs.SeatSpec{}, false
	}
	scratch := e.scratch
	return runs.SeatSpec{
		Env:             steward.SeatMCPEnv(e.reviewID, e.token),
		DisallowedTools: append([]string(nil), steward.SeatDisallowedTools...),
		Floor: func(tool string, paths []string) (bool, string) {
			return steward.SeatFloor(steward.SeatRequest{Tool: tool, Paths: paths, ScratchDir: scratch})
		},
	}, true
}

// seatEntryFor reports the review a steward task is the seat of (for the
// RPC handlers, which must bind a verdict submit to the caller's own seat).
func (h *stewardHost) seatEntryFor(taskID int64) (reviewID int64, ok bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	e, ok := h.seats[taskID]
	return e.reviewID, ok
}

func (h *stewardHost) forget(taskID int64) {
	h.mu.Lock()
	delete(h.seats, taskID)
	h.mu.Unlock()
}

func (h *stewardHost) contractShaOf(taskID int64) string {
	if h.svc.ContractSha == nil {
		return ""
	}
	return h.svc.ContractSha(taskID)
}

func (h *stewardHost) seatProfile() (store.AgentProfile, error) {
	ps, err := h.profReg.List()
	if err != nil {
		return store.AgentProfile{}, err
	}
	for _, p := range ps {
		if p.Name == steward.SeatProfileName {
			return p, nil
		}
	}
	return store.AgentProfile{}, fmt.Errorf("no %q profile: run `smind profile seed roles`", steward.SeatProfileName)
}

// stewardGateUnknown reports whether any gate is still unknown (CI pending).
func stewardGateUnknown(gs []steward.Gate) bool {
	for _, g := range gs {
		if g.State == steward.GateUnknown {
			return true
		}
	}
	return false
}

// probe reads the seat task's runs and queue.
func (h *stewardHost) probe(taskID int64) validation.ProbeState {
	var ps validation.ProbeState
	rows, err := h.db.ListRunsByTask(taskID, 50)
	if err != nil {
		return ps
	}
	ps.Started = len(rows) > 0
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
		if r.Status == string(runs.StatusRunning) {
			ps.Running = true
		}
	}
	if usage, err := h.db.ListRunUsage(ids); err == nil {
		for _, u := range usage {
			if u.CostUSD != nil {
				ps.CostUSD += *u.CostUSD
			}
		}
	}
	if chat, err := h.wm.DefaultChat(taskID); err == nil {
		if items, err := h.db.ListChatQueue(chat.ID); err == nil && len(items) > 0 {
			ps.Queued = true
		}
	}
	return ps
}

func (h *stewardHost) stopTask(taskID int64) {
	rows, err := h.db.ListRunsByTask(taskID, 50)
	if err != nil {
		return
	}
	for _, r := range rows {
		if r.Status == string(runs.StatusRunning) {
			_ = h.reg.Stop(r.ID)
		}
	}
}
