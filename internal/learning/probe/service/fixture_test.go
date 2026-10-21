package probesvc

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/learning/approval"
	"github.com/spacingmind/smind/internal/learning/gate"
	"github.com/spacingmind/smind/internal/store"
)

// fx is one workspace over a real git checkout, with a finished, priced,
// clean user run and a pending create proposal citing it.
type fx struct {
	t    *testing.T
	st   *store.Store
	ws   store.Workspace
	task store.Task
	chat int64
	run  store.Run
	prop store.LearnedProposal
	repo string
	base string
	home string
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newFx(t *testing.T) *fx {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	repo := t.TempDir()
	gitRun(t, repo, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "add", "-A")
	gitRun(t, repo, "commit", "-q", "-m", "one")
	base := gitRun(t, repo, "rev-parse", "HEAD")

	ws, err := st.CreateWorkspace(store.Workspace{Path: repo, Title: "w", RoutingPolicy: "hard"})
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
	f.run = f.addRun("src", func(r *store.Run) {})
	f.setCost("src", 0.10)
	f.prop = f.addProposal("create", f.run.ID, gate.EvFailureRecovery, createItems("skill-a"))
	return f
}

// addRun inserts a finished clean user run in the fixture chat; mut may
// change it before the insert.
func (f *fx) addRun(id string, mut func(*store.Run)) store.Run {
	f.t.Helper()
	f.addRunNoProvenance(id, mut)
	dirty := false
	if err := f.st.UpdateRunProvenance(id, store.RunProvenance{BaseSHA: &f.base, Dirty: &dirty}); err != nil {
		f.t.Fatal(err)
	}
	got, err := f.st.GetRun(id)
	if err != nil {
		f.t.Fatal(err)
	}
	return got
}

// addRunNoProvenance inserts the run without recording base sha or dirty.
func (f *fx) addRunNoProvenance(id string, mut func(*store.Run)) store.Run {
	f.t.Helper()
	start := time.Now().UTC().Add(-10 * time.Minute)
	end := start.Add(5 * time.Minute)
	r := store.Run{ID: id, TaskID: f.task.ID, ChatID: f.chat, Provider: "claude-native", Prompt: "fix the build",
		Status: "done", StartedAt: start, FinishedAt: &end, Purpose: store.RunPurposeUser, Model: "claude-sonnet-5-5"}
	mut(&r)
	if _, err := f.st.CreateRun(r); err != nil {
		f.t.Fatal(err)
	}
	return r
}

func (f *fx) setCost(runID string, cost float64) {
	f.t.Helper()
	if err := f.st.UpsertRunUsage(store.RunUsage{RunID: runID, TaskID: f.task.ID, Provider: "claude-native", CostUSD: &cost, Source: "test"}); err != nil {
		f.t.Fatal(err)
	}
}

func createItems(name string) []approval.Item {
	sk := gate.Skill{Name: name, Category: "build", Description: "Use when the build fails with make errors.", Body: "Run make clean first.\n"}
	return []approval.Item{{Intent: gate.Intent{Action: gate.ActionCreate, Name: name}, Result: sk}}
}

func (f *fx) addProposal(kind, runID, evKind string, items []approval.Item) store.LearnedProposal {
	f.t.Helper()
	ev, _ := json.Marshal([]gate.Evidence{{Kind: evKind, RunID: runID, Seq: 0, Quote: "q"}})
	raw, err := approval.EncodeItems(items)
	if err != nil {
		f.t.Fatal(err)
	}
	p, err := f.st.CreateProposal(store.LearnedProposal{WorkspaceID: f.ws.ID, Kind: kind, Source: "reflector",
		Name: items[0].Result.Name, Intents: raw, Evidence: string(ev)})
	if err != nil {
		f.t.Fatal(err)
	}
	return p
}

// svc builds a Service that passes every platform check by default: darwin,
// a CLI path, perl found.
func (f *fx) svc(mut func(*Deps)) *Service {
	d := Deps{Store: f.st, Home: f.home, GOOS: "darwin",
		RealCLI: func() (string, error) { return "/usr/bin/true", nil }}
	if mut != nil {
		mut(&d)
	}
	s := New(d)
	s.lookPath = func(string) (string, error) { return "/usr/bin/perl", nil }
	f.t.Cleanup(s.Close)
	return s
}

func (f *fx) countRows() (probes, arms, runs int) {
	f.t.Helper()
	ps, err := f.st.ListProbes(f.ws.ID, 1000)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, p := range ps {
		as, err := f.st.ListProbeArms(p.ID)
		if err != nil {
			f.t.Fatal(err)
		}
		arms += len(as)
	}
	for _, purpose := range []string{"user", "probe", "reflect", "curate"} {
		rs, err := f.st.ListRunsByPurpose(purpose, 10000)
		if err != nil {
			f.t.Fatal(err)
		}
		runs += len(rs)
	}
	return len(ps), arms, runs
}

// addPurposeRun adds a finished run of purpose with a reported cost.
func (f *fx) addPurposeRun(id, purpose string, cost float64) {
	f.t.Helper()
	f.addRunNoProvenance(id, func(r *store.Run) { r.Purpose = purpose; r.ChatID = 0 })
	f.setCost(id, cost)
}

func (f *fx) seedProbeSpend(cost float64) { f.addPurposeRun("seed-probe", store.RunPurposeProbe, cost) }
