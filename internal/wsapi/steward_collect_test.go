package wsapi

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/forge"
	"github.com/spacingmind/smind/internal/gitstate"
	"github.com/spacingmind/smind/internal/qaframes"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/taskrunner"
	"github.com/spacingmind/smind/internal/validation"
)

const stewardContractDoc = `---
v: 1
milestone: "collector test"
base: origin/develop
planRef: docs/plans/active/x.md
writeScope:
  paths: [internal/wsapi, docs/x]
acs:
  - { id: AC1, text: "one", covers: { commands: [C1] } }
commands:
  - { id: C1, run: "echo hi" }
---
body
`

// fakeStewardGit fakes the gitstate slice the collector reads.
type fakeStewardGit struct {
	g          gitstate.TaskGit
	details    forge.PRDetails
	detailsErr error
	focused    bool
	refresh    bool
}

func (f *fakeStewardGit) Get(int64) (gitstate.TaskGit, error) { return f.g, nil }

func (f *fakeStewardGit) PRDetails(_ int64, focused, refresh bool) (forge.PRDetails, error) {
	f.focused, f.refresh = focused, refresh
	return f.details, f.detailsErr
}

func newStewardEnv(t *testing.T) (*stewardCollector, *validation.Service, store.Task) {
	t.Helper()
	wm, db := newTestWorkspaceManager(t)
	task := newTestTask(t, wm, "")
	svc := newValidationService(db, nil)
	svc.EvidenceRoot = t.TempDir()
	return newStewardCollector(db, svc, nil), svc, task
}

// commitTaskFile writes and commits one file in the task worktree.
func commitTaskFile(t *testing.T, dir, name, body, subject string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", name)
	gitIn(t, dir, "commit", "-qm", subject)
}

func TestStewardCollect_TaskAndGitFacts(t *testing.T) {
	t.Parallel()
	c, _, task := newStewardEnv(t)
	wt := *task.WorktreePath
	commitTaskFile(t, wt, "one.txt", "one\n", "first")
	commitTaskFile(t, wt, "two.txt", "two\n", "second")
	head := gitIn(t, wt, "rev-parse", "HEAD")

	in, err := c.collect(context.Background(), task.ID, head)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if in.Task.ID != task.ID || in.Task.Title != task.Title || in.Task.HeadSHA != head || in.Task.BaseSHA != task.BaseSha {
		t.Fatalf("task = %+v", in.Task)
	}
	if in.Task.Branch != *task.Branch {
		t.Fatalf("branch = %q, want %q", in.Task.Branch, *task.Branch)
	}
	if len(in.Commits) != 2 {
		t.Fatalf("commits = %+v, want 2 entries", in.Commits)
	}
	if in.Commits[0].Subject != "first" || in.Commits[1].Subject != "second" {
		t.Fatalf("commits not oldest first: %+v", in.Commits)
	}
	if in.Commits[0].SHA == in.Commits[1].SHA {
		t.Fatalf("commits have the same sha: %+v", in.Commits)
	}
	if !strings.Contains(in.Numstat, "one.txt") || !strings.Contains(in.Numstat, "two.txt") {
		t.Fatalf("numstat = %q", in.Numstat)
	}
}

func TestStewardCollect_ContractAndValidation(t *testing.T) {
	t.Parallel()
	c, svc, task := newStewardEnv(t)
	rec, err := svc.SubmitContract(task.ID, []byte(stewardContractDoc))
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("a", 40)
	val, _, err := svc.CreateValidation(task.ID, rec.ID, sha)
	if err != nil {
		t.Fatal(err)
	}
	restored, lost := true, false
	verdict, _ := json.Marshal(validation.Verdict{
		V: 1, ContractID: rec.ID, ContractSha256: rec.Sha256, CommitSHA: sha,
		Status: validation.StatusFail, Summary: "one mutation was not restored",
		Items: []validation.Item{
			{Kind: validation.KindMutation, ID: "m1", Status: validation.StatusPass, Restored: &restored},
			{Kind: validation.KindMutation, ID: "m2", Status: validation.StatusFail, Restored: &lost},
		},
	})
	if err := svc.DB.SetValidationVerdict(val.ID, string(verdict), validation.StatusFail, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	head := gitIn(t, *task.WorktreePath, "rev-parse", "HEAD")

	in, err := c.collect(context.Background(), task.ID, head)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if !in.HasContract {
		t.Fatalf("hasContract = false, contract %d was submitted", rec.ID)
	}
	if got := strings.Join(in.WriteScope, ","); got != "internal/wsapi,docs/x" {
		t.Fatalf("writeScope = %q", got)
	}
	if in.PlanFile != "docs/plans/active/x.md" {
		t.Fatalf("planFile = %q", in.PlanFile)
	}
	if in.Validation == nil {
		t.Fatalf("validation = nil")
	}
	if in.Validation.Status != validation.StatusFail || in.Validation.CommitSHA != sha {
		t.Fatalf("validation = %+v", in.Validation)
	}
	if len(in.Validation.Mutations) != 2 {
		t.Fatalf("mutations = %+v", in.Validation.Mutations)
	}
	if in.Validation.Mutations[0].ID != "m1" || !in.Validation.Mutations[0].Restored {
		t.Fatalf("m1 = %+v, want restored", in.Validation.Mutations[0])
	}
	if in.Validation.Mutations[1].ID != "m2" || in.Validation.Mutations[1].Restored {
		t.Fatalf("m2 = %+v, want not restored", in.Validation.Mutations[1])
	}
}

func TestStewardCollect_NoContractNoValidation(t *testing.T) {
	t.Parallel()
	c, _, task := newStewardEnv(t)
	head := gitIn(t, *task.WorktreePath, "rev-parse", "HEAD")

	in, err := c.collect(context.Background(), task.ID, head)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if in.HasContract || in.WriteScope != nil || in.PlanFile != "" {
		t.Fatalf("contract = %v %v %q", in.HasContract, in.WriteScope, in.PlanFile)
	}
	if in.Validation != nil {
		t.Fatalf("validation = %+v, want nil", in.Validation)
	}
	if in.Frames != nil || in.FramesDir != "" {
		t.Fatalf("frames = %+v in %q, want none", in.Frames, in.FramesDir)
	}
}

func TestStewardCollect_PRAndCIMapped(t *testing.T) {
	t.Parallel()
	_, svc, task := newStewardEnv(t)
	head := gitIn(t, *task.WorktreePath, "rev-parse", "HEAD")
	pr := func(checks *forge.Checks) *forge.PR {
		return &forge.PR{Number: 7, BaseRef: "develop", Title: "T", Checks: checks}
	}
	cases := []struct {
		name        string
		checks      *forge.Checks
		detailsErr  error
		items       []forge.CheckItem
		wantState   string
		wantBody    string
		wantFailing []string
	}{
		{name: "success", checks: &forge.Checks{State: forge.ChecksSuccess}, items: []forge.CheckItem{{Name: "build", State: forge.CheckSuccess}}, wantState: "success", wantBody: "the body"},
		{name: "failure", checks: &forge.Checks{State: forge.ChecksFailure},
			items:     []forge.CheckItem{{Name: "lint", State: forge.CheckFailure}, {Name: "test", State: forge.CheckFailure}, {Name: "build", State: forge.CheckSuccess}},
			wantState: "failure", wantBody: "the body", wantFailing: []string{"lint", "test"}},
		{name: "pending", checks: &forge.Checks{State: forge.ChecksPending}, wantState: "pending", wantBody: "the body"},
		{name: "nil checks", checks: nil, wantState: "none", wantBody: "the body"},
		{name: "details error", checks: &forge.Checks{State: forge.ChecksSuccess}, detailsErr: errors.New("boom"), wantState: "success", wantBody: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeStewardGit{
				g:          gitstate.TaskGit{PR: pr(tc.checks)},
				details:    forge.PRDetails{Body: "the body", Checks: forge.DetailChecks{Checks: forge.Checks{State: tc.wantState}, Items: tc.items}},
				detailsErr: tc.detailsErr,
			}
			c := newStewardCollector(svc.DB, svc, fake)
			in, err := c.collect(context.Background(), task.ID, head)
			if err != nil {
				t.Fatalf("collect: %v", err)
			}
			if in.PR == nil {
				t.Fatalf("pr = nil")
			}
			if in.PR.Number != 7 || in.PR.Base != "develop" || in.PR.Title != "T" || in.PR.Body != tc.wantBody {
				t.Fatalf("pr = %+v", in.PR)
			}
			if in.CI.State != tc.wantState {
				t.Fatalf("ci.state = %q, want %q", in.CI.State, tc.wantState)
			}
			if got := strings.Join(in.CI.Failing, ","); got != strings.Join(tc.wantFailing, ",") {
				t.Fatalf("ci.failing = %q, want %q", got, strings.Join(tc.wantFailing, ","))
			}
			if fake.focused || fake.refresh {
				t.Fatalf("PRDetails called with focused=%v refresh=%v, want both false", fake.focused, fake.refresh)
			}
		})
	}
}

func TestStewardCollect_NilGitMeansNoPR(t *testing.T) {
	t.Parallel()
	c, _, task := newStewardEnv(t)
	head := gitIn(t, *task.WorktreePath, "rev-parse", "HEAD")

	in, err := c.collect(context.Background(), task.ID, head)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if in.PR != nil {
		t.Fatalf("pr = %+v, want nil", in.PR)
	}
	if in.CI.State != "none" || in.CI.Failing != nil {
		t.Fatalf("ci = %+v, want state none and no failing list", in.CI)
	}
}

func TestStewardCollect_PolicyAuditCounts(t *testing.T) {
	t.Parallel()
	c, _, task := newStewardEnv(t)
	for _, decision := range []string{"deny", "ask", "ask_human_deny", "allow"} {
		if _, err := c.db.AppendPolicyAudit(store.PolicyAuditRow{TaskID: task.ID, Decision: decision, Tool: "Bash"}); err != nil {
			t.Fatal(err)
		}
	}
	head := gitIn(t, *task.WorktreePath, "rev-parse", "HEAD")

	in, err := c.collect(context.Background(), task.ID, head)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if in.PolicyAudit.Denied != 2 || in.PolicyAudit.Escalated != 1 {
		t.Fatalf("policyAudit = %+v, want denied 2 escalated 1", in.PolicyAudit)
	}
}

func TestStewardCollect_FramesFromEvidenceReport(t *testing.T) {
	t.Parallel()
	c, svc, task := newStewardEnv(t)
	rec, err := svc.SubmitContract(task.ID, []byte(stewardContractDoc))
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.Repeat("b", 40)
	val, _, err := svc.CreateValidation(task.ID, rec.ID, sha)
	if err != nil {
		t.Fatal(err)
	}
	dir := svc.EvidenceDir(val.ID)
	report, _ := json.Marshal(qaframes.Report{V: 1, Status: qaframes.StatusCaptured, EvidenceDir: dir,
		Frames: []qaframes.FrameResult{{ID: "f1", Variants: []qaframes.Variant{{Viewport: []int{1280, 800}, Theme: "dark", Files: []string{"a.png"}}}}}})
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "report.json"), report, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.png"), []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	head := gitIn(t, *task.WorktreePath, "rev-parse", "HEAD")

	in, err := c.collect(context.Background(), task.ID, head)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if in.FramesDir != dir {
		t.Fatalf("framesDir = %q, want %q", in.FramesDir, dir)
	}
	if len(in.Frames) != 1 {
		t.Fatalf("frames = %+v, want one", in.Frames)
	}
	f := in.Frames[0]
	if f.Path != "a.png" || f.SizeBytes != 3 || f.Viewport != 1280 || f.Theme != "dark" || f.CapturedSHA != sha {
		t.Fatalf("frame = %+v", f)
	}
}

func TestStewardCollect_HandoffsFromChildren(t *testing.T) {
	t.Parallel()
	c, _, task := newStewardEnv(t)
	parent := task.ID
	newChild := func(title string) store.Task {
		t.Helper()
		ch, err := c.db.CreateTask(store.Task{WorkspaceID: task.WorkspaceID, ParentTaskID: &parent, Title: title, Status: "done"})
		if err != nil {
			t.Fatal(err)
		}
		return ch
	}
	finishRun := func(name string, taskID int64, text string) {
		t.Helper()
		runID := "run-" + name
		if _, err := c.db.CreateRun(store.Run{ID: runID, TaskID: taskID, Provider: "glm", Status: "done", StartedAt: time.Now()}); err != nil {
			t.Fatal(err)
		}
		block := "```json\n" + `{"status":"done","not_verified":["gofmt"],"notes":"kept write ownership"}` + "\n```\n"
		textEv, _ := json.Marshal(map[string]any{"type": taskrunner.EventTypeText, "text": text + block})
		doneEv, _ := json.Marshal(map[string]any{"type": taskrunner.EventTypeDone, "stopReason": "end_turn"})
		if _, err := c.db.AppendRunEvent(runID, 1, string(textEv)); err != nil {
			t.Fatal(err)
		}
		if _, err := c.db.AppendRunEvent(runID, 2, string(doneEv)); err != nil {
			t.Fatal(err)
		}
	}
	peer := newChild("S2 collector")
	validatorChild := newChild("validate #3: collector test")
	finishRun("peer", peer.ID, "done, one commit. ")
	finishRun("validator", validatorChild.ID, "done. ")

	head := gitIn(t, *task.WorktreePath, "rev-parse", "HEAD")
	in, err := c.collect(context.Background(), task.ID, head)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if len(in.Handoffs) != 1 {
		t.Fatalf("handoffs = %+v, want exactly the peer child's", in.Handoffs)
	}
	h := in.Handoffs[0]
	if h.Status != "done" || h.Notes != "kept write ownership" || len(h.NotVerified) != 1 || h.NotVerified[0] != "gofmt" {
		t.Fatalf("handoff = %+v", h)
	}
}

func TestStewardCollect_UnknownTaskErrors(t *testing.T) {
	t.Parallel()
	c, _, task := newStewardEnv(t)
	if _, err := c.collect(context.Background(), task.ID+9999, strings.Repeat("a", 40)); err == nil {
		t.Fatalf("collect of an unknown task must error")
	}
}

func TestStewardCollect_DoesNotMutateWorktree(t *testing.T) {
	t.Parallel()
	c, _, task := newStewardEnv(t)
	wt := *task.WorktreePath
	commitTaskFile(t, wt, "one.txt", "one\n", "first")
	before := gitIn(t, wt, "status", "--porcelain")
	head := gitIn(t, wt, "rev-parse", "HEAD")

	if _, err := c.collect(context.Background(), task.ID, head); err != nil {
		t.Fatalf("collect: %v", err)
	}
	if after := gitIn(t, wt, "status", "--porcelain"); after != before {
		t.Fatalf("worktree changed: before %q after %q", before, after)
	}
}
