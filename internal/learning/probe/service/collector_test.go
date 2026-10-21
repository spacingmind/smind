package probesvc

import (
	"context"
	"testing"

	"github.com/spacingmind/smind/internal/learning/approval"
	"github.com/spacingmind/smind/internal/learning/executor"
	lsvc "github.com/spacingmind/smind/internal/learning/service"
	"github.com/spacingmind/smind/internal/runs"
)

type noExec struct{ calls int }

func (e *noExec) Run(context.Context, executor.Request) (executor.Result, error) {
	e.calls++
	return executor.Result{FinalText: `{"intents":[]}`}, nil
}

// A finished probe run must not feed learning: no cadence counter moves, no
// reflect job is queued, no reflector call is made, and no skill gets an
// offered/loaded count.
func TestProbeRuns_ExcludedFromCollectorAndCounters(t *testing.T) {
	t.Parallel()
	p := newPfx(t, nil)
	ex := &noExec{}
	learn := lsvc.New(lsvc.Deps{Store: p.st, Exec: ex, Approval: approval.New(approval.Deps{Store: p.st}), Home: t.TempDir()})
	t.Cleanup(learn.Close)

	pr := p.start(false)
	p.wait(pr.ID)
	rows, _ := p.st.ListProbeArms(pr.ID)
	sysChat := func() int64 {
		_, c, err := p.st.CreateSystemTask(p.ws.ID, "other", t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return c.ID
	}()
	for _, a := range rows {
		// As the registry would announce them: purpose probe, over the
		// cadence, in a chat that otherwise qualifies, and in a system chat.
		for _, chat := range []int64{p.chat, sysChat, 0} {
			learn.OnRunFinished(runs.RunFinished{RunID: a.RunID, TaskID: p.task.ID, ChatID: chat, Provider: "claude-native", Purpose: "probe", ToolCalls: 500})
		}
	}
	learn.Close() // drains the observer queue
	if c, err := p.st.GetChat(p.chat); err != nil || c.LearnPending != 0 {
		t.Fatalf("cadence counter moved: %+v %v", c, err)
	}
	if jobs, err := p.st.ListLearningJobs(p.ws.ID, 10); err != nil || len(jobs) != 0 || ex.calls != 0 {
		t.Fatalf("jobs=%d reflector calls=%d err=%v", len(jobs), ex.calls, err)
	}
	skills, _ := p.st.ListLearnedSkills(p.ws.ID)
	for _, k := range skills {
		if k.OfferedRuns != 0 || k.LoadedRuns != 0 || k.Loads != 0 {
			t.Fatalf("skill counters moved: %+v", k)
		}
	}
	// The probe runs are hidden from the user run lists.
	if user, _ := p.st.ListRecentRuns(100); len(user) != 1 { // the source run only
		t.Fatalf("recent user runs = %d, want 1", len(user))
	}
}
