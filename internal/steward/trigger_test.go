package steward

import (
	"errors"
	"strconv"
	"testing"

	"github.com/spacingmind/smind/internal/config"
)

func onConfig() config.StewardConfig {
	return config.StewardConfig{Enabled: true, AutoReview: []string{"smind"}}
}

func rootLeadDone(runID string, taskID int64) RunDone {
	return RunDone{
		RunID:          runID,
		TaskID:         taskID,
		Status:         "done",
		ProfileName:    "lead",
		ParentTaskID:   0,
		Branch:         "smind/task-x",
		HasHead:        true,
		WorkspaceTitle: "smind",
	}
}

func TestAutoReview_OffByDefault(t *testing.T) {
	fired := 0
	a := NewAutoTrigger(fixedConfig(config.StewardConfig{}), func(int64) { fired++ })
	if a.OnRunFinished(rootLeadDone("run-1", 7)) {
		t.Fatal("zero config must not fire")
	}

	a = NewAutoTrigger(fixedConfig(config.StewardConfig{Enabled: true}), func(int64) { fired++ })
	if a.OnRunFinished(rootLeadDone("run-1", 7)) {
		t.Fatal("enabled with an empty autoReview list must not fire")
	}
	if fired != 0 {
		t.Fatalf("fire calls: got %d, want 0", fired)
	}
}

func TestAutoReview_FiresOnceForRootLeadDone(t *testing.T) {
	var fired []int64
	a := NewAutoTrigger(fixedConfig(onConfig()), func(taskID int64) { fired = append(fired, taskID) })

	if !a.OnRunFinished(rootLeadDone("run-1", 7)) {
		t.Fatal("a root lead done run must fire")
	}
	if a.OnRunFinished(rootLeadDone("run-1", 7)) {
		t.Fatal("the same run id must fire only once")
	}
	if !a.OnRunFinished(rootLeadDone("run-2", 7)) {
		t.Fatal("a different run id must fire again")
	}
	if len(fired) != 2 || fired[0] != 7 || fired[1] != 7 {
		t.Fatalf("fire calls: got %v, want [7 7]", fired)
	}
}

func TestAutoReview_NotForErrorStoppedOrPeer(t *testing.T) {
	base := rootLeadDone("run-1", 7)
	cases := map[string]RunDone{
		"error status":        func(ev RunDone) RunDone { ev.Status = "error"; return ev }(base),
		"stopped status":      func(ev RunDone) RunDone { ev.Status = "stopped"; return ev }(base),
		"peer profile":        func(ev RunDone) RunDone { ev.ProfileName = "peer-sonnet"; return ev }(base),
		"validator profile":   func(ev RunDone) RunDone { ev.ProfileName = "validator"; return ev }(base),
		"steward profile":     func(ev RunDone) RunDone { ev.ProfileName = "steward"; return ev }(base),
		"child run":           func(ev RunDone) RunDone { ev.ParentTaskID = 7; return ev }(base),
		"no branch":           func(ev RunDone) RunDone { ev.Branch = ""; return ev }(base),
		"no head":             func(ev RunDone) RunDone { ev.HasHead = false; return ev }(base),
		"workspace not opted": func(ev RunDone) RunDone { ev.WorkspaceTitle = "other"; return ev }(base),
	}
	for name, ev := range cases {
		a := NewAutoTrigger(fixedConfig(onConfig()), func(int64) { t.Errorf("%s: fire must not be called", name) })
		if a.OnRunFinished(ev) {
			t.Errorf("%s: must not fire", name)
		}
	}

	a := NewAutoTrigger(errConfig(errors.New("bad config")), func(int64) { t.Error("invalid config: fire must not be called") })
	if a.OnRunFinished(base) {
		t.Error("invalid config: must not fire")
	}
}

func TestAutoReview_SeenSetBounded(t *testing.T) {
	fired := 0
	a := NewAutoTrigger(fixedConfig(onConfig()), func(int64) { fired++ })
	oldest := rootLeadDone("run-0", 7)
	if !a.OnRunFinished(oldest) {
		t.Fatal("the first run must fire")
	}
	for i := 1; i < 1100; i++ {
		if !a.OnRunFinished(rootLeadDone("run-"+strconv.Itoa(i), 7)) {
			t.Fatalf("run %d must fire", i)
		}
	}
	if fired != 1100 {
		t.Fatalf("fire calls: got %d, want 1100", fired)
	}
	a.mu.Lock()
	seen := len(a.seen)
	a.mu.Unlock()
	if seen > seenRunCap {
		t.Fatalf("seen set size: got %d, want <= %d", seen, seenRunCap)
	}

	// The oldest id was dropped, so it fires again; the newest is remembered.
	if !a.OnRunFinished(oldest) {
		t.Fatal("the oldest run id must have been dropped and fire again")
	}
	if a.OnRunFinished(rootLeadDone("run-1099", 7)) {
		t.Fatal("the newest run id must still be remembered")
	}
}

func TestAutoReview_FireOutsideLock(t *testing.T) {
	var fired []int64
	var a *AutoTrigger
	a = NewAutoTrigger(fixedConfig(onConfig()), func(taskID int64) {
		fired = append(fired, taskID)
		if taskID == 7 {
			// Fire may re-enter the trigger; a lock held over Fire would
			// deadlock here.
			if !a.OnRunFinished(rootLeadDone("run-inner", 8)) {
				t.Error("the inner run must fire")
			}
		}
	})
	if !a.OnRunFinished(rootLeadDone("run-outer", 7)) {
		t.Fatal("the outer run must fire")
	}
	if len(fired) != 2 || fired[0] != 7 || fired[1] != 8 {
		t.Fatalf("fire calls: got %v, want [7 8]", fired)
	}
}
