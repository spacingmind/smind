package wsapi

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	probesvc "github.com/spacingmind/smind/internal/learning/probe/service"
	"github.com/spacingmind/smind/internal/store"
)

// fakeProbe records calls; Start/Cancel are the only methods that would
// spend money, so tests assert they are never reached by read-only RPCs.
type fakeProbe struct {
	mu       sync.Mutex
	estimate int
	starts   int
	cancels  int
	startErr error
	view     *probesvc.View
	// cancelWS records CancelWorkspace calls; onCancelWS runs inside it.
	cancelWS   []int64
	onCancelWS func(int64)
}

func (p *fakeProbe) CancelWorkspace(id int64) {
	p.mu.Lock()
	p.cancelWS = append(p.cancelWS, id)
	fn := p.onCancelWS
	p.mu.Unlock()
	if fn != nil {
		fn(id)
	}
}

func (p *fakeProbe) Estimate(_ context.Context, id int64, quick bool) (probesvc.Estimate, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.estimate++
	return probesvc.Estimate{Eligible: true, Quick: quick, Arms: 3, EstUSD: 1.4, EstArmUSD: 0.47, ExpectedMinutes: 8, PerProbeCapUSD: 1.5, DailyCapUSD: 3}, nil
}

func (p *fakeProbe) Start(context.Context, int64, bool) (store.LearningProbe, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.starts++
	return store.LearningProbe{ID: 1}, p.startErr
}

func (p *fakeProbe) Cancel(int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cancels++
	return nil
}

func (p *fakeProbe) Get(int64) (probesvc.View, bool, error) {
	if p.view == nil {
		return probesvc.View{}, false, nil
	}
	return *p.view, true, nil
}

func (p *fakeProbe) List(int64, int) ([]probesvc.View, error) { return nil, nil }

func probeFx(t *testing.T, p LearningProbe) *learnFx {
	t.Helper()
	f := newLearnFx(t)
	f.api.SetLearning(LearningDeps{Probe: p})
	return f
}

func TestProbeRPC_Surface(t *testing.T) {
	t.Parallel()
	fp := &fakeProbe{view: &probesvc.View{ID: 7, ProposalID: 3, Status: "running"}}
	f := probeFx(t, fp)

	res, e := f.call("learning.probe.estimate", map[string]any{"id": 3})
	if e != nil {
		t.Fatalf("estimate: %+v", e)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(res, &keys); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"eligible", "reasons", "quick", "arms", "estUSD", "estArmUSD", "expectedMinutes", "perProbeCapUSD", "dailyCapUSD", "spentTodayUSD", "quotaPercent"} {
		if _, ok := keys[k]; !ok {
			t.Errorf("estimate result lacks %q: %s", k, res)
		}
	}
	if string(keys["reasons"]) != "[]" {
		t.Errorf("reasons = %s, want []", keys["reasons"])
	}

	var started probesvc.View
	f.ok("learning.probe.start", map[string]any{"id": 3, "quick": true}, &started)
	if started.ID != 7 || fp.starts != 1 {
		t.Errorf("start result %+v, starts %d", started, fp.starts)
	}
	f.ok("learning.probe.cancel", map[string]any{"id": 3}, nil)
	if fp.cancels != 1 {
		t.Errorf("cancels = %d", fp.cancels)
	}
	var got probesvc.View
	f.ok("learning.probe.get", map[string]any{"id": 3}, &got)
	if got.ID != 7 {
		t.Errorf("get = %+v", got)
	}
	fp.view = nil
	res, e = f.call("learning.probe.get", map[string]any{"id": 3})
	if e != nil || strings.TrimSpace(string(res)) != "null" {
		t.Errorf("get of nothing = %s %+v, want null", res, e)
	}
	res, e = f.call("learning.probe.list", map[string]any{"workspaceId": f.w.ID})
	if e != nil || strings.TrimSpace(string(res)) != "[]" {
		t.Errorf("list = %s %+v, want []", res, e)
	}
}

func TestProbeRPC_Validation(t *testing.T) {
	t.Parallel()
	f := probeFx(t, &fakeProbe{})
	for _, m := range []string{"learning.probe.estimate", "learning.probe.start", "learning.probe.cancel", "learning.probe.get"} {
		f.fail(m, map[string]any{"id": 0}, codeInvalid)
	}
	f.fail("learning.probe.list", map[string]any{"workspaceId": 0}, codeInvalid)
}

func TestProbeRPC_IneligibleIsFailedPrecondition(t *testing.T) {
	t.Parallel()
	fp := &fakeProbe{startErr: &probesvc.IneligibleError{Reasons: []probesvc.Reason{{Code: probesvc.ReasonBusy, Message: "Another probe is running."}}}}
	f := probeFx(t, fp)
	e := f.fail("learning.probe.start", map[string]any{"id": 3}, codeFailedPrecondition)
	if !strings.Contains(e.Message, "Another probe is running.") {
		t.Errorf("message = %q", e.Message)
	}
}

func TestProbeRPC_UnavailableWithoutService(t *testing.T) {
	t.Parallel()
	f := newLearnFx(t)
	for _, m := range []string{"learning.probe.estimate", "learning.probe.start", "learning.probe.cancel", "learning.probe.get"} {
		f.fail(m, map[string]any{"id": 3}, codeUnavailable)
	}
	f.fail("learning.probe.list", map[string]any{"workspaceId": f.w.ID}, codeUnavailable)
}

func TestProbeRPC_NoModelCall(t *testing.T) {
	t.Parallel()
	fp := &fakeProbe{view: &probesvc.View{ID: 1}}
	f := probeFx(t, fp)
	f.ok("learning.probe.estimate", map[string]any{"id": 3}, nil)
	f.ok("learning.probe.get", map[string]any{"id": 3}, nil)
	f.ok("learning.probe.list", map[string]any{"workspaceId": f.w.ID}, nil)
	if fp.starts != 0 || fp.cancels != 0 || fp.estimate != 1 {
		t.Errorf("read-only RPCs reached Start/Cancel: starts %d cancels %d estimate %d", fp.starts, fp.cancels, fp.estimate)
	}
}

func TestEvents_ProbeLifecycle(t *testing.T) {
	t.Parallel()
	f := newLearnFx(t)
	ec := newEventConn(t, dialWS(t, f.srv, "tok"))
	ec.subscribe("sub", TopicLearnedSkillProbeStarted, TopicLearnedSkillProbeArm, TopicLearnedSkillProbeFinished, TopicLearnedSkillProbeFailed)
	n := f.api.ProbeNotifier()
	n.ProbeStarted(f.w.ID, 5, 3)
	n.ProbeArm(f.w.ID, 5, 3, "A", "running")
	n.ProbeFinished(f.w.ID, 5, 3, "done", "no_effect")
	n.ProbeFailed(f.w.ID, 6, 3, "secret reason that must not reach the wire")
	want := []struct {
		topic string
		check func(learningProbePayload) bool
	}{
		{TopicLearnedSkillProbeStarted, func(p learningProbePayload) bool { return p.ProbeID == 5 && p.Role == "" && p.Verdict == "" }},
		{TopicLearnedSkillProbeArm, func(p learningProbePayload) bool { return p.Role == "A" && p.ArmStatus == "running" }},
		{TopicLearnedSkillProbeFinished, func(p learningProbePayload) bool { return p.Status == "done" && p.Verdict == "no_effect" }},
		{TopicLearnedSkillProbeFailed, func(p learningProbePayload) bool { return p.ProbeID == 6 && p.Status == "failed" }},
	}
	for _, w := range want {
		ev := ec.nextEvent(5 * time.Second)
		if ev.Topic != w.topic {
			t.Fatalf("topic = %q, want %q", ev.Topic, w.topic)
		}
		var p learningProbePayload
		decodePayload(t, ev, &p)
		if p.WorkspaceID != f.w.ID || p.ProposalID != 3 || !w.check(p) {
			t.Errorf("%s payload = %+v", w.topic, p)
		}
		if raw, _ := json.Marshal(ev.Payload); strings.Contains(string(raw), "secret reason") {
			t.Errorf("%s leaked the failure reason: %s", w.topic, raw)
		}
	}
}

// TestProbeRPC_ForgetCancelsProbe: forget, setMode off and workspace.delete
// each cancel the workspace's probe first, while its rows still exist.
func TestProbeRPC_ForgetCancelsProbe(t *testing.T) {
	t.Parallel()
	fp := &fakeProbe{}
	f := probeFx(t, fp)
	stillThere := true
	fp.onCancelWS = func(id int64) {
		if _, err := f.db.GetWorkspace(id); err != nil {
			stillThere = false
		}
	}

	f.ok("learning.setMode", map[string]any{"workspaceId": f.w.ID, "mode": "propose"}, nil)
	if len(fp.cancelWS) != 0 {
		t.Fatalf("turning learning on cancelled a probe: %v", fp.cancelWS)
	}
	f.ok("learning.setMode", map[string]any{"workspaceId": f.w.ID, "mode": "off"}, nil)
	if len(fp.cancelWS) != 1 || fp.cancelWS[0] != f.w.ID {
		t.Fatalf("setMode off cancels = %v", fp.cancelWS)
	}
	f.ok("learning.forget", map[string]any{"workspaceId": f.w.ID}, nil)
	if len(fp.cancelWS) != 2 || fp.cancelWS[1] != f.w.ID {
		t.Fatalf("forget cancels = %v", fp.cancelWS)
	}
	f.ok("workspace.delete", map[string]any{"id": f.w.ID}, nil)
	if len(fp.cancelWS) != 3 || fp.cancelWS[2] != f.w.ID {
		t.Fatalf("workspace.delete cancels = %v", fp.cancelWS)
	}
	if !stillThere {
		t.Fatal("a probe was cancelled after its workspace was already gone")
	}
	if _, err := f.db.GetWorkspace(f.w.ID); err == nil {
		t.Fatal("workspace.delete did not delete")
	}
}
