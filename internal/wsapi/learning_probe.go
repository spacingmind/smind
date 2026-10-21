package wsapi

import (
	"context"
	"errors"

	probesvc "github.com/spacingmind/smind/internal/learning/probe/service"
	"github.com/spacingmind/smind/internal/store"
)

// Probe topics (ADR-0037): a hint to refetch learning.probe.get/list. The
// payload carries ids and a status word only, never run output.
const (
	TopicLearnedSkillProbeStarted  = "learnedSkill.probe.started"
	TopicLearnedSkillProbeArm      = "learnedSkill.probe.arm"
	TopicLearnedSkillProbeFinished = "learnedSkill.probe.finished"
	TopicLearnedSkillProbeFailed   = "learnedSkill.probe.failed"
)

// codeFailedPrecondition is the error code of learning.probe.start for a
// proposal that cannot be probed right now (the message is the first reason).
const codeFailedPrecondition = "failed_precondition"

// LearningProbe is the probe service the RPCs call (satisfied by
// *probesvc.Service). Optional: without one the RPCs report "unavailable".
type LearningProbe interface {
	Estimate(ctx context.Context, proposalID int64, quick bool) (probesvc.Estimate, error)
	Start(ctx context.Context, proposalID int64, quick bool) (store.LearningProbe, error)
	Cancel(proposalID int64) error
	Get(proposalID int64) (probesvc.View, bool, error)
	List(workspaceID int64, limit int) ([]probesvc.View, error)
}

type probeParams struct {
	ID    int64 `json:"id"`
	Quick bool  `json:"quick"`
}

type probeListParams struct {
	WorkspaceID int64 `json:"workspaceId"`
	Limit       int   `json:"limit"`
}

type learningProbePayload struct {
	ProbeID     int64  `json:"probeId"`
	ProposalID  int64  `json:"proposalId"`
	WorkspaceID int64  `json:"workspaceId"`
	Status      string `json:"status"`
	Role        string `json:"role,omitempty"`
	ArmStatus   string `json:"armStatus,omitempty"`
	Verdict     string `json:"verdict,omitempty"`
}

func probeService(h *learningHost, method string) (LearningProbe, error) {
	p := h.get().Probe
	if p == nil {
		return nil, &rpcCodeError{code: codeUnavailable, msg: method + ": probes are not available"}
	}
	return p, nil
}

func probeProposalID(method string, id int64) error {
	if id <= 0 {
		return learningInvalid(method, "id is required")
	}
	return nil
}

func estimateProbe(ctx context.Context, h *learningHost, p probeParams) (any, error) {
	const method = "learning.probe.estimate"
	if err := probeProposalID(method, p.ID); err != nil {
		return nil, err
	}
	svc, err := probeService(h, method)
	if err != nil {
		return nil, err
	}
	est, err := svc.Estimate(ctx, p.ID, p.Quick)
	if err != nil {
		return nil, err
	}
	if est.Reasons == nil {
		est.Reasons = []probesvc.Reason{}
	}
	return est, nil
}

func startProbe(ctx context.Context, h *learningHost, p probeParams) (any, error) {
	const method = "learning.probe.start"
	if err := probeProposalID(method, p.ID); err != nil {
		return nil, err
	}
	svc, err := probeService(h, method)
	if err != nil {
		return nil, err
	}
	if _, err := svc.Start(ctx, p.ID, p.Quick); err != nil {
		var ie *probesvc.IneligibleError
		if errors.As(err, &ie) && len(ie.Reasons) > 0 {
			return nil, &rpcCodeError{code: codeFailedPrecondition, msg: method + ": " + ie.Reasons[0].Message, err: err}
		}
		return nil, err
	}
	v, ok, err := svc.Get(p.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, &rpcCodeError{code: codeNotFound, msg: method + ": probe not found"}
	}
	return v, nil
}

func cancelProbe(_ context.Context, h *learningHost, p idParams) (any, error) {
	const method = "learning.probe.cancel"
	if err := probeProposalID(method, p.ID); err != nil {
		return nil, err
	}
	svc, err := probeService(h, method)
	if err != nil {
		return nil, err
	}
	if err := svc.Cancel(p.ID); err != nil {
		return nil, err
	}
	return map[string]any{}, nil
}

func getProbe(_ context.Context, h *learningHost, p idParams) (any, error) {
	const method = "learning.probe.get"
	if err := probeProposalID(method, p.ID); err != nil {
		return nil, err
	}
	svc, err := probeService(h, method)
	if err != nil {
		return nil, err
	}
	v, ok, err := svc.Get(p.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil // JSON null: no probe yet
	}
	return v, nil
}

func listProbes(_ context.Context, h *learningHost, p probeListParams) (any, error) {
	const method = "learning.probe.list"
	if p.WorkspaceID <= 0 {
		return nil, learningInvalid(method, "workspaceId is required")
	}
	svc, err := probeService(h, method)
	if err != nil {
		return nil, err
	}
	views, err := svc.List(p.WorkspaceID, p.Limit)
	if err != nil {
		return nil, err
	}
	if views == nil {
		views = []probesvc.View{}
	}
	return views, nil
}

func probeHandlers(h *learningHost) map[string]handlerFunc {
	return map[string]handlerFunc{
		"learning.probe.estimate": learningMethod(h, "learning.probe.estimate", estimateProbe),
		"learning.probe.start":    learningMethod(h, "learning.probe.start", startProbe),
		"learning.probe.cancel":   learningMethod(h, "learning.probe.cancel", cancelProbe),
		"learning.probe.get":      learningMethod(h, "learning.probe.get", getProbe),
		"learning.probe.list":     learningMethod(h, "learning.probe.list", listProbes),
	}
}

// ProbeStarted publishes learnedSkill.probe.started.
func (h *learningHost) ProbeStarted(workspaceID, probeID, proposalID int64) {
	h.bus.Publish(Event{Topic: TopicLearnedSkillProbeStarted, Payload: learningProbePayload{
		ProbeID: probeID, ProposalID: proposalID, WorkspaceID: workspaceID, Status: "running",
	}})
}

// ProbeArm publishes learnedSkill.probe.arm.
func (h *learningHost) ProbeArm(workspaceID, probeID, proposalID int64, role, status string) {
	h.bus.Publish(Event{Topic: TopicLearnedSkillProbeArm, Payload: learningProbePayload{
		ProbeID: probeID, ProposalID: proposalID, WorkspaceID: workspaceID, Status: "running", Role: role, ArmStatus: status,
	}})
}

// ProbeFinished publishes learnedSkill.probe.finished.
func (h *learningHost) ProbeFinished(workspaceID, probeID, proposalID int64, status, verdict string) {
	h.bus.Publish(Event{Topic: TopicLearnedSkillProbeFinished, Payload: learningProbePayload{
		ProbeID: probeID, ProposalID: proposalID, WorkspaceID: workspaceID, Status: status, Verdict: verdict,
	}})
}

// ProbeFailed publishes learnedSkill.probe.failed (the reason stays in the
// stored probe row, not on the wire).
func (h *learningHost) ProbeFailed(workspaceID, probeID, proposalID int64, _ string) {
	h.bus.Publish(Event{Topic: TopicLearnedSkillProbeFailed, Payload: learningProbePayload{
		ProbeID: probeID, ProposalID: proposalID, WorkspaceID: workspaceID, Status: "failed",
	}})
}

// ProbeNotifier returns the notifier the probe service calls; it satisfies
// probesvc.Notifier.
func (a *API) ProbeNotifier() probesvc.Notifier { return a.learning }
