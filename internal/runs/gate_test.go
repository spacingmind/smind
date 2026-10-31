package runs

import (
	"errors"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/gate"
	"github.com/spacingmind/smind/internal/runs/failure"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
)

func gateFn(g *gate.Gate) func(string, time.Time) *gate.Gate {
	return func(provider string, _ time.Time) *gate.Gate {
		if provider != "glm" {
			return nil
		}
		return g
	}
}

func TestRunStart_GatedNonHumanRefusedTyped(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{gate.ReasonNotSignedIn, gate.ReasonUnsupportedByHarness} {
		e := newQueuedTestEnv(t, "")
		e.reg.SetGate(gateFn(&gate.Gate{Reason: reason, Action: &failure.Action{Kind: failure.ActionSignIn, Label: "fix it"}}))
		for _, src := range []string{store.ChatQueueSourceAgent, store.ChatQueueSourceOrchestrator} {
			_, err := e.send(0, "", src, 7, 8)
			var ge *GatedError
			if !errors.As(err, &ge) || ge.Gate.Reason != reason || ge.Provider != "glm" {
				t.Fatalf("%s/%s: err = %v, want *GatedError", reason, src, err)
			}
			if ge.RPCCode() != "gated" {
				t.Fatalf("code = %q", ge.RPCCode())
			}
			if d, ok := ge.RPCData().(gate.Gate); !ok || d.Reason != reason {
				t.Fatalf("data = %#v", ge.RPCData())
			}
		}
	}
}

func TestRunStart_GatedErrorSaysWhatNotToDo(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	e.reg.SetGate(gateFn(&gate.Gate{Reason: gate.ReasonNotSignedIn, Action: &failure.Action{Kind: failure.ActionSignIn, Label: "Add the API key for GLM"}}))
	_, err := e.send(0, "", store.ChatQueueSourceAgent, 7, 8)
	if err == nil {
		t.Fatal("not refused")
	}
	msg := err.Error()
	for _, want := range []string{"not_signed_in", "Add the API key for GLM", "do not retry", "do not fall back"} {
		if !contains(msg, want) {
			t.Fatalf("message %q lacks %q", msg, want)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestRunStart_AdvisoryGateNotRefused(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	e.reg.SetGate(gateFn(&gate.Gate{Reason: gate.ReasonNotSignedIn, Advisory: true}))
	out, err := e.send(0, "", store.ChatQueueSourceAgent, 7, 8)
	if err != nil || out.RunID == "" {
		t.Fatalf("advisory gate refused a send: %+v %v", out, err)
	}
	waitForStatus(t, e.reg, out.RunID, StatusDone, 5*time.Second)
}

func TestRunStart_OutOfPlanNotRefused(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	// Routing and the profile wait/fallback policy own out_of_plan: the gate
	// alone never refuses a send for it.
	e.reg.SetGate(gateFn(&gate.Gate{Reason: gate.ReasonOutOfPlan}))
	out, err := e.send(0, "", store.ChatQueueSourceAgent, 7, 8)
	if err != nil || out.RunID == "" {
		t.Fatalf("out_of_plan refused a send: %+v %v", out, err)
	}
	waitForStatus(t, e.reg, out.RunID, StatusDone, 5*time.Second)
}

func TestRunStart_HumanNotRefusedByGate(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	e.reg.SetGate(gateFn(&gate.Gate{Reason: gate.ReasonUnsupportedByHarness}))
	out, err := e.send(0, "", store.ChatQueueSourceHuman, 0, 0)
	if err != nil || out.RunID == "" {
		t.Fatalf("human send refused: %+v %v", out, err)
	}
	waitForStatus(t, e.reg, out.RunID, StatusDone, 5*time.Second)
}

func TestProviderCooldownError_IsTypedGate(t *testing.T) {
	t.Parallel()
	e := newQueuedTestEnv(t, "")
	until := e.cool(t, "glm", time.Hour)
	u := until
	e.reg.SetGate(gateFn(&gate.Gate{Reason: gate.ReasonCoolingDown, Until: &u}))
	_, err := e.send(0, "", store.ChatQueueSourceAgent, 7, 8)
	var ce *ProviderCooldownError
	if !errors.As(err, &ce) {
		t.Fatalf("err = %v", err)
	}
	if ce.RPCCode() != "gated" {
		t.Fatalf("code = %q", ce.RPCCode())
	}
	if d, ok := ce.RPCData().(gate.Gate); !ok || d.Reason != gate.ReasonCoolingDown {
		t.Fatalf("data = %#v", ce.RPCData())
	}
}

func TestFailureHook_CalledForHumanFixAndPlanLimits(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ err, kind string }{
		{"401 Unauthorized: invalid api key", failure.KindAuthExpired},
		{"429 Usage limit reached for 5 hour", failure.KindPlanWindowExhausted},
		{"plain failure with no class", ""},
	} {
		wm, st := newTestWorkspaceManager(t)
		task := newTestTask(t, wm, "")
		reg := newTestRegistry(t, st)
		var got []string
		reg.SetFailureHook(func(provider, model, kind string) { got = append(got, provider+"|"+kind) })
		if _, err := st.CreateRun(store.Run{ID: "fh", TaskID: task.ID, Provider: "glm", Prompt: "p", Status: "running", StartedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
		r := registerTestRun(reg, "fh")
		r.taskID, r.provider, r.prompt = task.ID, taskrunner.ProviderGLM, "p"
		reg.finish(r, errors.New(tc.err))
		switch {
		case tc.kind == "" && len(got) != 0:
			t.Fatalf("%q: hook called for an unclassified failure: %v", tc.err, got)
		case tc.kind != "" && (len(got) != 1 || got[0] != "glm|"+tc.kind):
			t.Fatalf("%q: hook calls = %v, want glm|%s", tc.err, got, tc.kind)
		}
	}
}
