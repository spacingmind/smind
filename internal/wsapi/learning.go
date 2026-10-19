package wsapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/spacingmind/smind/internal/config"
	"github.com/spacingmind/smind/internal/learning/approval"
	"github.com/spacingmind/smind/internal/learning/export"
	"github.com/spacingmind/smind/internal/learning/gate"
	learningservice "github.com/spacingmind/smind/internal/learning/service"
	"github.com/spacingmind/smind/internal/skills"
	"github.com/spacingmind/smind/internal/store"
)

// The learning.* RPCs (ADR-0026 §8): everything a human can do to learned
// skills and proposals. They are registered on the /ws table only. No MCP
// tool wraps them and a run-scoped (agent) caller is refused, so the loop
// that writes skills can never be steered by the agents it teaches
// (recursion guard, ADR-0026 §3).

// Wire codes for the learning RPCs (see rpcCodeError).
const (
	codeGateRejected = "gate_rejected"
	codeStale        = "stale"
	codeConflict     = "conflict"
	codeNotFound     = "not_found"
	codeInvalid      = "invalid_params"
	codeUnavailable  = "unavailable"
	codeForbidden    = "forbidden"
)

// exportTargets are the accepted learning.skills.export targets.
var exportTargets = []string{"repo", "claude", "agents", "codex"}

// Learning event topics (ADR-0009 pattern).
const (
	// TopicLearnedSkillProposed: a proposal is waiting for approval
	// (payload learnedSkillProposedPayload).
	TopicLearnedSkillProposed = "learnedSkill.proposed"
	// TopicLearnedSkillChanged: a proposal or skill changed through a human
	// action (payload learnedSkillChangedPayload).
	TopicLearnedSkillChanged = "learnedSkill.changed"
	// TopicLearningJob: a learning job changed status (payload
	// learningJobPayload).
	TopicLearningJob = "learning.job"
)

type learnedSkillProposedPayload struct {
	WorkspaceID int64  `json:"workspaceId"`
	ProposalID  int64  `json:"proposalId"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
}

type learnedSkillChangedPayload struct {
	WorkspaceID int64  `json:"workspaceId"`
	SkillID     int64  `json:"skillId,omitempty"`
	ProposalID  int64  `json:"proposalId,omitempty"`
	Reason      string `json:"reason"`
}

type learningJobPayload struct {
	WorkspaceID int64  `json:"workspaceId"`
	JobID       int64  `json:"jobId"`
	Status      string `json:"status"`
}

// LearningNotifier is what the collector calls to announce learning changes;
// the event bus adapter implements it. Zero ids are omitted from the wire.
type LearningNotifier interface {
	NotifyProposed(workspaceID, proposalID int64, name, kind string)
	NotifyChanged(workspaceID, skillID, proposalID int64, reason string)
	NotifyJob(workspaceID, jobID int64, status string)
}

// LearningTrigger starts reflect/curate jobs on a human's request. It is
// optional; without one the RPCs report "unavailable".
type LearningTrigger interface {
	Reflect(ctx context.Context, chatID int64) error
	Curate(ctx context.Context, workspaceID int64) error
}

// LearningConfig is the slice of config.Learning the RPCs need.
type LearningConfig struct {
	// Enabled is the global kill switch.
	Enabled bool
	// ProbationRuns: a skill offered fewer runs than this is on probation.
	ProbationRuns int
	// DailyBudgetUSD and DailyBudgetTokens are the per-workspace caps the
	// status budget is reported against.
	DailyBudgetUSD    float64
	DailyBudgetTokens int
}

// LearningDeps wires the learning RPCs; every field is optional.
type LearningDeps struct {
	Approval *approval.Service
	// Config is read per request, so a settings toggle takes effect at once.
	Config  func() LearningConfig
	Trigger LearningTrigger
	// OnWorkspaceReset, when set, runs after learning.forget and after
	// learning.setMode to off, so a stale projection never lingers (the
	// daemon removes the workspace's learned-skill projection here).
	OnWorkspaceReset func(workspaceID int64)
	// Home is the user's home directory, the base of the claude/agents/codex
	// export targets. Empty means os.UserHomeDir() (tests set it).
	Home string
}

// learningHost holds the (replaceable) learning collaborators.
type learningHost struct {
	db  *store.Store
	bus *eventBus

	mu   sync.RWMutex
	deps LearningDeps
}

func newLearningHost(db *store.Store, bus *eventBus) *learningHost {
	d := config.DefaultLearning()
	return &learningHost{db: db, bus: bus, deps: LearningDeps{
		Approval: approval.New(approval.Deps{Store: db}),
		Config: func() LearningConfig {
			return LearningConfig{Enabled: d.Enabled, ProbationRuns: d.ProbationRuns, DailyBudgetUSD: d.DailyBudgetUSD, DailyBudgetTokens: d.DailyBudgetTokens}
		},
	}}
}

func (h *learningHost) set(d LearningDeps) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if d.Approval != nil {
		h.deps.Approval = d.Approval
	}
	if d.Config != nil {
		h.deps.Config = d.Config
	}
	h.deps.Trigger = d.Trigger
	h.deps.OnWorkspaceReset = d.OnWorkspaceReset
	h.deps.Home = d.Home
}

// reset runs the optional OnWorkspaceReset hook.
func (h *learningHost) reset(workspaceID int64) {
	if fn := h.get().OnWorkspaceReset; fn != nil {
		fn(workspaceID)
	}
}

func (h *learningHost) get() LearningDeps {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.deps
}

func (h *learningHost) cfg() LearningConfig { return h.get().Config() }

// NotifyProposed publishes learnedSkill.proposed.
func (h *learningHost) NotifyProposed(workspaceID, proposalID int64, name, kind string) {
	h.bus.Publish(Event{Topic: TopicLearnedSkillProposed, Payload: learnedSkillProposedPayload{
		WorkspaceID: workspaceID, ProposalID: proposalID, Name: name, Kind: kind,
	}})
}

// NotifyChanged publishes learnedSkill.changed.
func (h *learningHost) NotifyChanged(workspaceID, skillID, proposalID int64, reason string) {
	h.bus.Publish(Event{Topic: TopicLearnedSkillChanged, Payload: learnedSkillChangedPayload{
		WorkspaceID: workspaceID, SkillID: skillID, ProposalID: proposalID, Reason: reason,
	}})
}

// NotifyJob publishes learning.job, except for a job over a hidden system
// task's chat (those never reach clients).
func (h *learningHost) NotifyJob(workspaceID, jobID int64, status string) {
	if j, err := h.db.GetLearningJob(jobID); err == nil && h.systemChat(j.ChatID) {
		return
	}
	h.bus.Publish(Event{Topic: TopicLearningJob, Payload: learningJobPayload{
		WorkspaceID: workspaceID, JobID: jobID, Status: status,
	}})
}

// systemChat reports whether chatID belongs to a hidden system task.
func (h *learningHost) systemChat(chatID *int64) bool {
	if chatID == nil {
		return false
	}
	c, err := h.db.GetChat(*chatID)
	return err == nil && isSystemTask(h.db, c.TaskID)
}

// SetLearning replaces the learning collaborators (the daemon passes its
// fully configured approval service, config, trigger and reset hook). Nil
// Approval or Config keep the current ones; Trigger and OnWorkspaceReset are
// always replaced.
func (a *API) SetLearning(d LearningDeps) { a.learning.set(d) }

// LearningNotifier returns the notifier the collector calls to announce
// proposals and job changes.
func (a *API) LearningNotifier() LearningNotifier { return a.learning }

// ---- views -----------------------------------------------------------

// LearnedSkillView is one learned skill on the wire. Body is only set when
// the request asked for it.
type LearnedSkillView struct {
	ID                  int64   `json:"id"`
	WorkspaceID         int64   `json:"workspaceId"`
	Name                string  `json:"name"`
	Category            string  `json:"category"`
	Description         string  `json:"description"`
	Body                *string `json:"body,omitempty"`
	Status              string  `json:"status"`
	Pinned              bool    `json:"pinned"`
	Source              string  `json:"source"`
	CreatedByRun        string  `json:"createdByRun"`
	PossibleDuplicateOf string  `json:"possibleDuplicateOf"`
	Similarity          float64 `json:"similarity"`
	Version             int     `json:"version"`
	OfferedRuns         int     `json:"offeredRuns"`
	LoadedRuns          int     `json:"loadedRuns"`
	Loads               int     `json:"loads"`
	LoadsViaSkill       int     `json:"loadsViaSkill"`
	LoadsViaRead        int     `json:"loadsViaRead"`
	Patched             int     `json:"patched"`
	ReusedAfterPatch    int     `json:"reusedAfterPatch"`
	Measured            bool    `json:"measured"`
	Probation           bool    `json:"probation"`
	LoadRate            float64 `json:"loadRate"`
	LastOfferedAt       *string `json:"lastOfferedAt,omitempty"`
	LastLoadedAt        *string `json:"lastLoadedAt,omitempty"`
	CreatedAt           string  `json:"createdAt"`
	UpdatedAt           string  `json:"updatedAt"`
}

func rfc3339Ptr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.UTC().Format(time.RFC3339)
	return &s
}

func skillView(k store.LearnedSkill, probationRuns int, withBody bool) LearnedSkillView {
	v := LearnedSkillView{
		ID: k.ID, WorkspaceID: k.WorkspaceID, Name: k.Name, Category: k.Category, Description: k.Description,
		Status: k.Status, Pinned: k.Pinned, Source: k.Source, CreatedByRun: k.CreatedByRun,
		PossibleDuplicateOf: k.PossibleDuplicateOf, Similarity: k.Similarity, Version: k.Version,
		OfferedRuns: k.OfferedRuns, LoadedRuns: k.LoadedRuns, Loads: k.Loads,
		LoadsViaSkill: k.LoadsViaSkill, LoadsViaRead: k.LoadsViaRead, Patched: k.Patched,
		ReusedAfterPatch: k.ReusedAfterPatch, Measured: k.Measured,
		Probation:     k.OfferedRuns < probationRuns,
		LastOfferedAt: rfc3339Ptr(k.LastOfferedAt), LastLoadedAt: rfc3339Ptr(k.LastLoadedAt),
		CreatedAt: k.CreatedAt.UTC().Format(time.RFC3339), UpdatedAt: k.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if k.OfferedRuns > 0 {
		v.LoadRate = float64(k.LoadedRuns) / float64(k.OfferedRuns)
	}
	if withBody {
		b := k.Body
		v.Body = &b
	}
	return v
}

// EvidenceView is one provenance item of a proposal or ledger row.
type EvidenceView struct {
	Kind  string `json:"kind"`
	RunID string `json:"runId"`
	Seq   int64  `json:"seq"`
	Quote string `json:"quote"`
}

func decodeEvidence(raw string) []EvidenceView {
	out := []EvidenceView{}
	if raw == "" {
		return out
	}
	var items []gate.Evidence
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return out
	}
	for _, e := range items {
		out = append(out, EvidenceView{Kind: e.Kind, RunID: e.RunID, Seq: e.Seq, Quote: e.Quote})
	}
	return out
}

// ProposalItemView is one gated change inside a proposal.
type ProposalItemView struct {
	Action        string `json:"action"`
	Name          string `json:"name"`
	Category      string `json:"category"`
	Description   string `json:"description"`
	Body          string `json:"body"`
	OldString     string `json:"oldString"`
	NewString     string `json:"newString"`
	AbsorbedInto  string `json:"absorbedInto"`
	Reason        string `json:"reason"`
	TargetSkillID int64  `json:"targetSkillId,omitempty"`
	BaseVersion   int    `json:"baseVersion,omitempty"`
}

// ProposalView is one proposal on the wire. A payload that does not decode
// yields Items empty and Malformed true.
type ProposalView struct {
	ID                  int64              `json:"id"`
	WorkspaceID         int64              `json:"workspaceId"`
	Kind                string             `json:"kind"`
	Source              string             `json:"source"`
	Name                string             `json:"name"`
	Status              string             `json:"status"`
	Note                string             `json:"note"`
	JobID               *int64             `json:"jobId,omitempty"`
	PossibleDuplicateOf string             `json:"possibleDuplicateOf"`
	Similarity          float64            `json:"similarity"`
	CreatedAt           string             `json:"createdAt"`
	DecidedAt           *string            `json:"decidedAt,omitempty"`
	Malformed           bool               `json:"malformed,omitempty"`
	Items               []ProposalItemView `json:"items"`
	Evidence            []EvidenceView     `json:"evidence"`
}

func proposalView(p store.LearnedProposal) ProposalView {
	v := ProposalView{
		ID: p.ID, WorkspaceID: p.WorkspaceID, Kind: p.Kind, Source: p.Source, Name: p.Name,
		Status: p.Status, Note: p.Note, JobID: p.JobID,
		PossibleDuplicateOf: p.PossibleDuplicateOf, Similarity: p.Similarity,
		CreatedAt: p.CreatedAt.UTC().Format(time.RFC3339), DecidedAt: rfc3339Ptr(p.DecidedAt),
		Items: []ProposalItemView{}, Evidence: decodeEvidence(p.Evidence),
	}
	items, err := approval.DecodeItems(p.Intents)
	if err != nil {
		v.Malformed = true
		return v
	}
	for _, it := range items {
		name := it.Result.Name
		if name == "" {
			name = it.Intent.Name
		}
		v.Items = append(v.Items, ProposalItemView{
			Action: string(it.Intent.Action), Name: name, Category: it.Result.Category,
			Description: it.Result.Description, Body: it.Result.Body,
			OldString: it.Intent.OldString, NewString: it.Intent.NewString,
			AbsorbedInto: it.Intent.AbsorbedInto, Reason: it.Intent.Reason,
			TargetSkillID: it.TargetSkillID, BaseVersion: it.BaseVersion,
		})
	}
	return v
}

// LedgerEntryView is one audit-ledger row.
type LedgerEntryView struct {
	ID           int64          `json:"id"`
	WorkspaceID  int64          `json:"workspaceId"`
	SkillID      *int64         `json:"skillId,omitempty"`
	SkillName    string         `json:"skillName"`
	Action       string         `json:"action"`
	Reason       string         `json:"reason"`
	Evidence     []EvidenceView `json:"evidence"`
	AbsorbedInto string         `json:"absorbedInto"`
	RunID        string         `json:"runId"`
	Actor        string         `json:"actor"`
	CreatedAt    string         `json:"createdAt"`
}

// VersionView is one stored version of a skill's text.
type VersionView struct {
	Version     int    `json:"version"`
	Description string `json:"description"`
	Body        string `json:"body"`
	Actor       string `json:"actor"`
	CreatedAt   string `json:"createdAt"`
}

// JobView is one reflect/curate job.
type JobView struct {
	ID          int64    `json:"id"`
	WorkspaceID int64    `json:"workspaceId"`
	ChatID      *int64   `json:"chatId,omitempty"`
	Kind        string   `json:"kind"`
	Trigger     string   `json:"trigger"`
	Status      string   `json:"status"`
	SkipReason  string   `json:"skipReason"`
	Error       string   `json:"error"`
	CostUSD     *float64 `json:"costUsd,omitempty"`
	Tokens      *int64   `json:"tokens,omitempty"`
	CreatedAt   string   `json:"createdAt"`
	FinishedAt  *string  `json:"finishedAt,omitempty"`
}

// ---- errors ----------------------------------------------------------

// learningFindings is the data of a gate_rejected error.
type learningFindings struct {
	Findings []learningFinding `json:"findings"`
}

type learningFinding struct {
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

func learningErr(method string, err error) error {
	if err == nil {
		return nil
	}
	var ge *approval.GateError
	switch {
	case errors.As(err, &ge):
		d := learningFindings{Findings: make([]learningFinding, 0, len(ge.Findings))}
		for _, f := range ge.Findings {
			d.Findings = append(d.Findings, learningFinding{Reason: string(f.Reason), Detail: f.Detail})
		}
		return &rpcCodeError{code: codeGateRejected, msg: method + ": " + err.Error(), err: err, data: d}
	case errors.Is(err, approval.ErrStale):
		return &rpcCodeError{code: codeStale, msg: method + ": " + err.Error(), err: err}
	case errors.Is(err, approval.ErrNotPending):
		return &rpcCodeError{code: codeConflict, msg: method + ": " + err.Error(), err: err}
	case errors.Is(err, sql.ErrNoRows), errors.Is(err, approval.ErrNoSuchVersion):
		return &rpcCodeError{code: codeNotFound, msg: method + ": not found", err: err}
	case errors.Is(err, approval.ErrUndoCurrent), errors.Is(err, approval.ErrBadPayload),
		errors.Is(err, approval.ErrMergeIntoHuman), errors.Is(err, approval.ErrMergeTargetUnknown),
		errors.Is(err, approval.ErrMergeKind), errors.Is(err, approval.ErrMergeNeedsBody):
		return &rpcCodeError{code: codeInvalid, msg: method + ": " + err.Error(), err: err}
	}
	return fmt.Errorf("%s: %w", method, err)
}

func learningInvalid(method, format string, args ...any) error {
	return &rpcCodeError{code: codeInvalid, msg: method + ": " + fmt.Sprintf(format, args...)}
}

// ---- handlers --------------------------------------------------------

// learningMethod wraps a typed handler: it refuses a run-scoped (agent)
// caller, decodes params into P and maps service errors to wire codes.
func learningMethod[P any](h *learningHost, method string, fn func(ctx context.Context, h *learningHost, p P) (any, error)) handlerFunc {
	return func(ctx context.Context, rc *requestContext, raw json.RawMessage) (any, error) {
		if rc != nil && rc.runScoped {
			return nil, &rpcCodeError{code: codeForbidden, msg: method + " is not available to agents"}
		}
		var p P
		if len(raw) > 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, learningInvalid(method, "invalid params: %v", err)
			}
		}
		out, err := fn(ctx, h, p)
		if err != nil {
			var rce *rpcCodeError
			if errors.As(err, &rce) {
				return nil, err
			}
			return nil, learningErr(method, err)
		}
		return out, nil
	}
}

type idParams struct {
	ID int64 `json:"id"`
}

type workspaceParams struct {
	WorkspaceID int64 `json:"workspaceId"`
}

// learningHandlers returns the learning.* RPC table.
func learningHandlers(h *learningHost) map[string]handlerFunc {
	return map[string]handlerFunc{
		"learning.status":            learningMethod(h, "learning.status", statusLearning),
		"learning.setMode":           learningMethod(h, "learning.setMode", setModeLearning),
		"learning.forget":            learningMethod(h, "learning.forget", forgetLearning),
		"learning.skills.list":       learningMethod(h, "learning.skills.list", listSkillsLearning),
		"learning.skills.get":        learningMethod(h, "learning.skills.get", getSkillLearning),
		"learning.skills.pin":        skillAction(h, "learning.skills.pin", "pinned", (*approval.Service).Pin),
		"learning.skills.unpin":      skillAction(h, "learning.skills.unpin", "unpinned", (*approval.Service).Unpin),
		"learning.skills.disable":    skillAction(h, "learning.skills.disable", "disabled", (*approval.Service).Disable),
		"learning.skills.enable":     skillAction(h, "learning.skills.enable", "enabled", (*approval.Service).Enable),
		"learning.skills.archive":    skillAction(h, "learning.skills.archive", "archived", archive),
		"learning.skills.restore":    skillAction(h, "learning.skills.restore", "restored", restore),
		"learning.skills.delete":     skillAction(h, "learning.skills.delete", "deleted", (*approval.Service).Delete),
		"learning.skills.undo":       learningMethod(h, "learning.skills.undo", undoLearning),
		"learning.skills.export":     learningMethod(h, "learning.skills.export", exportLearning),
		"learning.proposals.list":    learningMethod(h, "learning.proposals.list", listProposalsLearning),
		"learning.proposals.get":     learningMethod(h, "learning.proposals.get", getProposalLearning),
		"learning.proposals.approve": learningMethod(h, "learning.proposals.approve", approveLearning),
		"learning.proposals.reject":  learningMethod(h, "learning.proposals.reject", rejectLearning),
		"learning.jobs.list":         learningMethod(h, "learning.jobs.list", listJobsLearning),
		"learning.reflect":           learningMethod(h, "learning.reflect", reflectLearning),
		"learning.curate":            learningMethod(h, "learning.curate", curateLearning),
	}
}

func archive(s *approval.Service, id int64, actor string) error { return s.Archive(id, actor, "") }
func restore(s *approval.Service, id int64, actor string) error { return s.Restore(id, actor, "") }

// skillAction builds a pin/disable/... handler: look the skill up (for its
// workspace and a 404), run the action as the human, publish the change.
func skillAction(h *learningHost, method, reason string, act func(*approval.Service, int64, string) error) handlerFunc {
	return learningMethod(h, method, func(_ context.Context, h *learningHost, p idParams) (any, error) {
		k, err := h.db.GetLearnedSkill(p.ID)
		if err != nil {
			return nil, err
		}
		if err := act(h.get().Approval, p.ID, "human"); err != nil {
			return nil, err
		}
		h.NotifyChanged(k.WorkspaceID, k.ID, 0, reason)
		return map[string]any{}, nil
	})
}

func (h *learningHost) workspace(method string, id int64) (store.Workspace, error) {
	if id == 0 {
		return store.Workspace{}, learningInvalid(method, "workspaceId is required")
	}
	return h.db.GetWorkspace(id)
}

type learningStatusWorkspace struct {
	ID               int64  `json:"id"`
	Title            string `json:"title"`
	LearningMode     string `json:"learningMode"`
	PendingProposals int    `json:"pendingProposals"`
	ActiveSkills     int    `json:"activeSkills"`
	DisabledSkills   int    `json:"disabledSkills"`
	// Budget is the workspace's rolling 24h learning spend against the caps.
	Budget learningBudget `json:"budget"`
}

// learningBudgetWindow is the budget's rolling window (the collector's).
const learningBudgetWindow = 24 * time.Hour

// learningBudget mirrors what the collector's budget check sees: usage of the
// workspace's non-user (learning) runs in the last windowHours. UnreportedRuns
// are runs with no cost/token figure; the collector prices those pessimistically,
// so the budget may be spent sooner than usedUsd/usedTokens suggest.
type learningBudget struct {
	WindowHours    int     `json:"windowHours"`
	UsedUSD        float64 `json:"usedUsd"`
	UsedTokens     int64   `json:"usedTokens"`
	CapUSD         float64 `json:"capUsd"`
	CapTokens      int     `json:"capTokens"`
	UnreportedRuns int     `json:"unreportedRuns"`
}

type learningStatusResult struct {
	Enabled    bool                      `json:"enabled"`
	Workspaces []learningStatusWorkspace `json:"workspaces"`
	// Budget is always null; the budget is per workspace (workspaces[].budget).
	// The field is kept so existing clients still find it.
	Budget any `json:"budget"`
}

func statusLearning(_ context.Context, h *learningHost, _ struct{}) (any, error) {
	wss, err := h.db.ListWorkspaces()
	if err != nil {
		return nil, err
	}
	cfg := h.cfg()
	out := learningStatusResult{Enabled: cfg.Enabled, Workspaces: []learningStatusWorkspace{}}
	for _, w := range wss {
		pending, err := h.db.ListProposals(w.ID, "pending")
		if err != nil {
			return nil, err
		}
		skills, err := h.db.ListLearnedSkills(w.ID, store.SkillActive, store.SkillDisabled)
		if err != nil {
			return nil, err
		}
		cost, tokens, unreported, err := h.db.SumLearningUsageForWorkspace(w.ID, time.Now().Add(-learningBudgetWindow))
		if err != nil {
			return nil, err
		}
		row := learningStatusWorkspace{ID: w.ID, Title: w.Title, LearningMode: w.LearningMode, PendingProposals: len(pending),
			Budget: learningBudget{WindowHours: int(learningBudgetWindow / time.Hour), UsedUSD: cost, UsedTokens: tokens,
				CapUSD: cfg.DailyBudgetUSD, CapTokens: cfg.DailyBudgetTokens, UnreportedRuns: unreported}}
		if row.LearningMode == "" {
			row.LearningMode = store.LearningModeOff
		}
		for _, k := range skills {
			if k.Status == store.SkillActive {
				row.ActiveSkills++
			} else {
				row.DisabledSkills++
			}
		}
		out.Workspaces = append(out.Workspaces, row)
	}
	return out, nil
}

func setModeLearning(_ context.Context, h *learningHost, p struct {
	WorkspaceID int64  `json:"workspaceId"`
	Mode        string `json:"mode"`
}) (any, error) {
	const m = "learning.setMode"
	if p.Mode != store.LearningModeOff && p.Mode != store.LearningModePropose {
		return nil, learningInvalid(m, "mode must be %q or %q", store.LearningModeOff, store.LearningModePropose)
	}
	if _, err := h.workspace(m, p.WorkspaceID); err != nil {
		return nil, err
	}
	if err := h.get().Approval.SetLearningMode(p.WorkspaceID, p.Mode); err != nil {
		return nil, err
	}
	if p.Mode == store.LearningModeOff {
		h.reset(p.WorkspaceID)
	}
	h.NotifyChanged(p.WorkspaceID, 0, 0, "modeChanged")
	return map[string]any{}, nil
}

func forgetLearning(_ context.Context, h *learningHost, p workspaceParams) (any, error) {
	if _, err := h.workspace("learning.forget", p.WorkspaceID); err != nil {
		return nil, err
	}
	if err := h.get().Approval.Forget(p.WorkspaceID); err != nil {
		return nil, err
	}
	h.reset(p.WorkspaceID)
	h.NotifyChanged(p.WorkspaceID, 0, 0, "forgotten")
	return map[string]any{}, nil
}

func listSkillsLearning(_ context.Context, h *learningHost, p struct {
	WorkspaceID int64    `json:"workspaceId"`
	Statuses    []string `json:"statuses"`
	IncludeBody bool     `json:"includeBody"`
}) (any, error) {
	const m = "learning.skills.list"
	if _, err := h.workspace(m, p.WorkspaceID); err != nil {
		return nil, err
	}
	for _, s := range p.Statuses {
		if s != store.SkillActive && s != store.SkillDisabled && s != store.SkillArchived {
			return nil, learningInvalid(m, "unknown status %q", s)
		}
	}
	skills, err := h.db.ListLearnedSkills(p.WorkspaceID, p.Statuses...)
	if err != nil {
		return nil, err
	}
	probation := h.cfg().ProbationRuns
	out := make([]LearnedSkillView, 0, len(skills))
	for _, k := range skills {
		out = append(out, skillView(k, probation, p.IncludeBody))
	}
	return out, nil
}

func getSkillLearning(_ context.Context, h *learningHost, p idParams) (any, error) {
	k, err := h.db.GetLearnedSkill(p.ID)
	if err != nil {
		return nil, err
	}
	versions, err := h.db.ListSkillVersions(k.ID)
	if err != nil {
		return nil, err
	}
	ledger, err := h.db.ListLedger(k.WorkspaceID, k.ID, 0)
	if err != nil {
		return nil, err
	}
	res := struct {
		Skill    LearnedSkillView  `json:"skill"`
		Versions []VersionView     `json:"versions"`
		Ledger   []LedgerEntryView `json:"ledger"`
		// Evidence is the provenance the skill was approved with (redacted
		// quotes the gate verified against the window), [] when none.
		Evidence []EvidenceView `json:"evidence"`
	}{Skill: skillView(k, h.cfg().ProbationRuns, true), Versions: []VersionView{}, Ledger: []LedgerEntryView{}, Evidence: decodeEvidence(k.Evidence)}
	for _, v := range versions {
		res.Versions = append(res.Versions, VersionView{
			Version: v.Version, Description: v.Description, Body: v.Body, Actor: v.Actor,
			CreatedAt: v.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	for _, e := range ledger {
		res.Ledger = append(res.Ledger, LedgerEntryView{
			ID: e.ID, WorkspaceID: e.WorkspaceID, SkillID: e.SkillID, SkillName: e.SkillName,
			Action: e.Action, Reason: e.Reason, Evidence: decodeEvidence(e.Evidence),
			AbsorbedInto: e.AbsorbedInto, RunID: e.RunID, Actor: e.Actor,
			CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return res, nil
}

func undoLearning(_ context.Context, h *learningHost, p struct {
	ID      int64 `json:"id"`
	Version int   `json:"version"`
}) (any, error) {
	if p.Version <= 0 {
		return nil, learningInvalid("learning.skills.undo", "version is required")
	}
	k, err := h.get().Approval.Undo(p.ID, p.Version, "human")
	if err != nil {
		return nil, err
	}
	h.NotifyChanged(k.WorkspaceID, k.ID, 0, "undone")
	return skillView(k, h.cfg().ProbationRuns, true), nil
}

func listProposalsLearning(_ context.Context, h *learningHost, p struct {
	WorkspaceID int64  `json:"workspaceId"`
	Status      string `json:"status"`
}) (any, error) {
	const m = "learning.proposals.list"
	if _, err := h.workspace(m, p.WorkspaceID); err != nil {
		return nil, err
	}
	if !slices.Contains([]string{"", "pending", "approved", "rejected"}, p.Status) {
		return nil, learningInvalid(m, "unknown status %q", p.Status)
	}
	ps, err := h.db.ListProposals(p.WorkspaceID, p.Status)
	if err != nil {
		return nil, err
	}
	out := make([]ProposalView, 0, len(ps))
	for _, pr := range ps {
		out = append(out, proposalView(pr))
	}
	return out, nil
}

type proposalMerge struct {
	Target     LearnedSkillView `json:"target"`
	Similarity float64          `json:"similarity"`
}

func getProposalLearning(_ context.Context, h *learningHost, p idParams) (any, error) {
	pr, err := h.db.GetProposal(p.ID)
	if err != nil {
		return nil, err
	}
	res := struct {
		Proposal ProposalView   `json:"proposal"`
		Merge    *proposalMerge `json:"merge,omitempty"`
	}{Proposal: proposalView(pr)}
	if pr.Status == "pending" && pr.Kind == "create" && pr.PossibleDuplicateOf != "" {
		// A human duplicate or a vanished target has no merge offer.
		if d, err := h.get().Approval.MergeDraft(pr.ID, pr.PossibleDuplicateOf); err == nil {
			res.Merge = &proposalMerge{Target: skillView(d.Target, h.cfg().ProbationRuns, true), Similarity: d.Similarity}
		}
	}
	return res, nil
}

func approveLearning(_ context.Context, h *learningHost, p struct {
	ID          int64   `json:"id"`
	Description *string `json:"description"`
	Body        *string `json:"body"`
	MergeInto   string  `json:"mergeInto"`
}) (any, error) {
	k, err := h.get().Approval.Approve(p.ID, approval.ApproveOptions{
		Actor: "human", Description: p.Description, Body: p.Body, MergeInto: p.MergeInto,
	})
	if err != nil {
		return nil, err
	}
	h.NotifyChanged(k.WorkspaceID, k.ID, p.ID, "approved")
	return skillView(k, h.cfg().ProbationRuns, true), nil
}

func rejectLearning(_ context.Context, h *learningHost, p struct {
	ID   int64  `json:"id"`
	Note string `json:"note"`
}) (any, error) {
	pr, err := h.db.GetProposal(p.ID)
	if err != nil {
		return nil, err
	}
	if err := h.get().Approval.Reject(p.ID, p.Note, "human"); err != nil {
		return nil, err
	}
	h.NotifyChanged(pr.WorkspaceID, 0, pr.ID, "rejected")
	return map[string]any{}, nil
}

func listJobsLearning(_ context.Context, h *learningHost, p struct {
	WorkspaceID int64 `json:"workspaceId"`
	Limit       int   `json:"limit"`
}) (any, error) {
	if _, err := h.workspace("learning.jobs.list", p.WorkspaceID); err != nil {
		return nil, err
	}
	limit := p.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	jobs, err := h.db.ListLearningJobs(p.WorkspaceID, limit)
	if err != nil {
		return nil, err
	}
	out := make([]JobView, 0, len(jobs))
	for _, j := range jobs {
		if h.systemChat(j.ChatID) {
			continue
		}
		out = append(out, JobView{
			ID: j.ID, WorkspaceID: j.WorkspaceID, ChatID: j.ChatID, Kind: j.Kind, Trigger: j.TriggerKind,
			Status: j.Status, SkipReason: j.SkipReason, Error: j.Error, CostUSD: j.CostUSD, Tokens: j.Tokens,
			CreatedAt: j.CreatedAt.UTC().Format(time.RFC3339), FinishedAt: rfc3339Ptr(j.FinishedAt),
		})
	}
	return out, nil
}

// trigger returns the optional job trigger, or the "unavailable" error when
// learning is switched off or no collector is wired.
func (h *learningHost) trigger(method string) (LearningTrigger, error) {
	d := h.get()
	if !d.Config().Enabled {
		return nil, &rpcCodeError{code: codeUnavailable, msg: method + ": learning is disabled"}
	}
	if d.Trigger == nil {
		return nil, &rpcCodeError{code: codeUnavailable, msg: method + ": learning jobs are not available"}
	}
	return d.Trigger, nil
}

func reflectLearning(ctx context.Context, h *learningHost, p struct {
	ChatID int64 `json:"chatId"`
}) (any, error) {
	const m = "learning.reflect"
	t, err := h.trigger(m)
	if err != nil {
		return nil, err
	}
	if p.ChatID == 0 {
		return nil, learningInvalid(m, "chatId is required")
	}
	c, err := h.db.GetChat(p.ChatID)
	if err != nil {
		return nil, err
	}
	if isSystemTask(h.db, c.TaskID) {
		return nil, learningInvalid(m, "cannot reflect on a system chat")
	}
	if err := t.Reflect(ctx, p.ChatID); err != nil {
		return nil, triggerErr(m, err)
	}
	return map[string]any{}, nil
}

func curateLearning(ctx context.Context, h *learningHost, p workspaceParams) (any, error) {
	const m = "learning.curate"
	t, err := h.trigger(m)
	if err != nil {
		return nil, err
	}
	if _, err := h.workspace(m, p.WorkspaceID); err != nil {
		return nil, err
	}
	if err := t.Curate(ctx, p.WorkspaceID); err != nil {
		return nil, triggerErr(m, err)
	}
	return map[string]any{}, nil
}

// triggerErr maps a collector refusal that is not a client mistake onto the
// "unavailable" wire code; everything else goes through learningMethod's
// generic mapping.
func triggerErr(method string, err error) error {
	if errors.Is(err, learningservice.ErrNotImplemented) {
		return &rpcCodeError{code: codeUnavailable, msg: method + ": not available yet (the curator job has not shipped)", err: err}
	}
	return err
}

// ExportResultView is the result of learning.skills.export.
type ExportResultView struct {
	Dir     string   `json:"dir"`
	Files   []string `json:"files"`
	Status  string   `json:"status"`
	Skipped []string `json:"skipped"`
}

func exportLearning(_ context.Context, h *learningHost, p struct {
	ID     int64  `json:"id"`
	Target string `json:"target"`
	Force  bool   `json:"force"`
}) (any, error) {
	const m = "learning.skills.export"
	if !slices.Contains(exportTargets, p.Target) {
		return nil, learningInvalid(m, "unknown target %q (want repo, claude, agents or codex)", p.Target)
	}
	k, err := h.db.GetLearnedSkill(p.ID)
	if err != nil {
		return nil, err
	}
	req := export.Request{Skill: k, Target: export.Target(p.Target), Home: h.get().Home, Force: p.Force}
	if p.Target == "repo" {
		ws, err := h.db.GetWorkspace(k.WorkspaceID)
		if err != nil {
			return nil, err
		}
		req.RepoPath = ws.Path
	}
	res, err := export.Export(req)
	if err != nil {
		if errors.Is(err, skills.ErrSymlinkedTarget) {
			return nil, &rpcCodeError{code: codeInvalid, msg: m + ": " + err.Error(), err: err}
		}
		return nil, err
	}
	// The ledger names the target and directory, never the skill text.
	if err := h.db.AppendLedger(store.LearnedLedgerEntry{
		WorkspaceID: k.WorkspaceID, SkillID: &k.ID, SkillName: k.Name, Action: "export", Actor: "human",
		CreatedAt: time.Now().UTC(),
		Reason:    fmt.Sprintf("exported to %s: %s", p.Target, res.Dir),
	}); err != nil {
		return nil, err
	}
	h.NotifyChanged(k.WorkspaceID, k.ID, 0, "exported")
	return ExportResultView{Dir: res.Dir, Files: nonNil(res.Files), Status: res.Status, Skipped: nonNil(res.Skipped)}, nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
