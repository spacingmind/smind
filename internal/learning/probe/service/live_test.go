//go:build liveprobe && !windows

package probesvc

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/learning/executor"
	"github.com/spacingmind/smind/internal/learning/projection"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/workspace"
)

type liveNotifier struct {
	mu     sync.Mutex
	events []string
}

func (n *liveNotifier) add(s string)                { n.mu.Lock(); n.events = append(n.events, s); n.mu.Unlock() }
func (n *liveNotifier) ProbeStarted(_, id, _ int64) { n.add("started") }
func (n *liveNotifier) ProbeArm(_, _, _ int64, role, status string) {
	n.add("arm:" + role + ":" + status)
}
func (n *liveNotifier) ProbeFinished(_, _, _ int64, status, verdict string) {
	n.add("finished:" + status + ":" + verdict)
}
func (n *liveNotifier) ProbeFailed(_, _, _ int64, reason string) { n.add("failed:" + reason) }

// TestLiveProbe_EndToEnd runs ONE full probe (canary + arms A, A2, B) against
// the real claude CLI on a throwaway repo and an isolated SMIND_HOME-like temp
// home: the one live end-to-end of ADR-0037 (AC: tripwires run clean, dirs and
// processes cleaned, rows stored, nothing outside touched). Haiku, cost-capped
// by the service (per-arm cap = 2 x estimate). Gated on SMIND_LIVE_PROBE=1;
// never run by CI. Never touches ~/.smind or port 4648.
func TestLiveProbe_EndToEnd(t *testing.T) {
	if os.Getenv("SMIND_LIVE_PROBE") != "1" {
		t.Skip("set SMIND_LIVE_PROBE=1 to run the live end-to-end probe (about 4 haiku sessions)")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("claude CLI not on PATH")
	}
	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	repo, _ := filepath.EvalSymlinks(t.TempDir())
	gitRun(t, repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "notes.txt"), []byte("probe live repo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-q", "-m", "one")
	base := gitRun(t, repo, "rev-parse", "HEAD")

	ws, err := st.CreateWorkspace(store.Workspace{Path: repo, Title: "live", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetWorkspaceLearningMode(ws.ID, "propose"); err != nil {
		t.Fatal(err)
	}
	task, err := st.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "t", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	chat, err := st.CreateChat(task.ID, "c")
	if err != nil {
		t.Fatal(err)
	}
	f := &fx{t: t, st: st, ws: ws, task: task, chat: chat.ID, repo: repo, base: base, home: t.TempDir()}
	f.run = f.addRun("src", func(r *store.Run) {
		r.Prompt = "Using the Bash tool, create a file named result.txt in the current directory containing the single word ok, then reply done."
		r.Model = "claude-haiku-5-5"
	})
	f.setCost("src", 0.03)
	// user_message evidence has no failure signature: the verdict can reach
	// no_effect, never_loaded or inconclusive, never helped.
	f.prop = f.addProposal("create", f.run.ID, "user_message", createItems("learned-write-result"))

	wm := workspace.New(st)
	reg, err := runs.New(st)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reg.CloseAll)
	runner := taskrunner.New(wm)
	home := f.home
	n := &liveNotifier{}
	svc := New(Deps{Store: st, WM: wm, Runner: runner, Reg: reg, Exec: executor.New(st, wm, runner, reg, home),
		Home: home, Notify: n,
		RealCLI: func() (string, error) {
			p, err := exec.LookPath("claude")
			if err != nil {
				return "", err
			}
			return filepath.EvalSymlinks(p)
		},
	})
	t.Cleanup(svc.Close)
	t.Cleanup(func() { _ = projection.Remove(filepath.Join(home, "learning", "probe-projections"), ws.ID) })

	est, err := svc.Estimate(context.Background(), f.prop.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("estimate: eligible=%v reasons=%+v est=%.4f arms=%d", est.Eligible, est.Reasons, est.EstUSD, est.Arms)
	if !est.Eligible {
		t.Fatalf("not eligible: %+v", est.Reasons)
	}
	if _, err := svc.Start(context.Background(), f.prop.ID, false); err != nil {
		t.Fatal(err)
	}
	var v View
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		got, ok, err := svc.Get(f.prop.ID)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			v = got
			if v.Status != "queued" && v.Status != "running" {
				break
			}
		}
		time.Sleep(2 * time.Second)
	}
	pretty, _ := json.MarshalIndent(v, "", "  ")
	t.Logf("final view:\n%s", clipText(string(pretty), 6000))
	t.Logf("events: %v", n.events)
	t.Logf("TOTAL probe cost USD %.4f (est %.4f)", v.CostUSD, est.EstUSD)

	if v.Status != "done" {
		t.Fatalf("probe status = %q (verdict %v, reason %q)", v.Status, v.Verdict, v.VerdictReason)
	}
	if len(v.Arms) != 3 {
		t.Fatalf("arms = %d, want 3", len(v.Arms))
	}
	for _, a := range v.Arms {
		if a.Status != "done" {
			t.Errorf("arm %s status = %s", a.Role, a.Status)
		}
	}
	if v.Verdict == nil || *v.Verdict == "unsafe" || *v.Verdict == "probe_failed" || *v.Verdict == "helped" {
		t.Errorf("verdict = %v (helped is unreachable without a signature; unsafe/probe_failed mean the run itself broke)", v.Verdict)
	}
	// Cleanup and isolation.
	for _, d := range []string{filepath.Join(home, "probe", "arms"), filepath.Join(home, "probe", "launch")} {
		if ents, _ := os.ReadDir(d); len(ents) > 0 {
			t.Errorf("%s not empty after the probe: %d entries", d, len(ents))
		}
	}
	if st := gitRun(t, repo, "status", "--porcelain"); st != "" {
		t.Errorf("the real checkout changed: %q", st)
	}
	if head := gitRun(t, repo, "rev-parse", "HEAD"); head != base {
		t.Errorf("real HEAD moved")
	}
	if strings.Contains(string(pretty), os.Getenv("HOME")+"/.ssh") {
		t.Errorf("a stored view mentions ~/.ssh")
	}
}
