package validation

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidatorPrompt_ContainsOnlyStructuredFields(t *testing.T) {
	rec := ContractRecord{ID: 12, Sha256: "abcd", Contract: parseGood(t).Contract, Body: "SECRET lead reasoning"}
	p := BuildPrompt(rec, 5, sha1x)
	for _, want := range []string{"validation #5", sha1x, "AC1", "TestA", "M1", "internal/x/y.go", "C1", "F1", "validation_verdict_submit", "restored", "git checkout -- <file>"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	if strings.Contains(p, "SECRET") {
		t.Fatalf("the prompt must never carry the contract's markdown body")
	}
}

func TestValidatorPrompt_NeverContainsBodyOrLeadNotes(t *testing.T) {
	// The prompt is a pure function of the structured contract, the ids and
	// the commit: two records differing only in the body or in prose-only
	// fields (planRef is a pointer, not read by the validator) are identical.
	a := ContractRecord{ID: 1, Sha256: "s", Contract: parseGood(t).Contract, Body: "one"}
	b := a
	b.Body = "completely different reasoning the lead wrote"
	b.Contract.PlanRef = "docs/plans/active/secret.md#lead-notes"
	if BuildPrompt(a, 1, sha1x) != BuildPrompt(b, 1, sha1x) {
		t.Fatalf("the prompt depends on the body or the plan pointer")
	}
}

type fakeProbe struct{ st ProbeState }

func newWatch(t *testing.T, f fixture, max int, budget float64) (*Watch, *fakeProbe, *bool, int64) {
	t.Helper()
	val, _ := f.started(t)
	vt := int64(99)
	_ = f.db.Close // keep
	fp := &fakeProbe{}
	stopped := false
	w := &Watch{Svc: f.svc, ValidationID: val.ID, ValidatorTaskID: vt, MaxMinutes: max, BudgetUSD: budget,
		Probe: func(int64) ProbeState { return fp.st }, Stop: func() { stopped = true }}
	return w, fp, &stopped, val.ID
}

func TestTimeout_WritesBlockedTimeout(t *testing.T) {
	f := newFixture(t)
	w, fp, stopped, id := newWatch(t, f, 30, 0)
	fp.st = ProbeState{Running: true}
	if w.Tick(29 * time.Minute) {
		t.Fatalf("done too early")
	}
	if !w.Tick(2 * time.Minute) {
		t.Fatalf("must finish after the wall clock")
	}
	v, _ := f.svc.View(id)
	if v.Status != StatusBlocked || v.Verdict.BlockedReason != ReasonTimeout || !*stopped || v.MergeReady {
		t.Fatalf("view = %+v stopped=%v", v, *stopped)
	}
}

func TestRateLimitWaitDoesNotConsumeWallClock(t *testing.T) {
	f := newFixture(t)
	w, fp, _, id := newWatch(t, f, 30, 0)
	fp.st = ProbeState{Running: true}
	w.Tick(20 * time.Minute)
	fp.st = ProbeState{Queued: true} // waiting for the rate limit to reset
	for i := 0; i < 10; i++ {
		if w.Tick(time.Hour) {
			t.Fatalf("a queued (rate-limited) validator must not time out or count as crashed")
		}
	}
	fp.st = ProbeState{Running: true}
	if w.Tick(5 * time.Minute) {
		t.Fatalf("25 running minutes in total must not time out")
	}
	if v, _ := f.svc.View(id); v.Status != "running" {
		t.Fatalf("status = %s", v.Status)
	}
}

func TestBudget_WritesBlockedBudget(t *testing.T) {
	f := newFixture(t)
	w, fp, stopped, id := newWatch(t, f, 30, 1.5)
	fp.st = ProbeState{Running: true, CostUSD: 1.6}
	if !w.Tick(time.Second) {
		t.Fatalf("over budget must finish")
	}
	if v, _ := f.svc.View(id); v.Verdict.BlockedReason != ReasonBudget || !*stopped {
		t.Fatalf("view = %+v", v)
	}
}

func TestCrash_RunEndedWithoutVerdictIsBlockedCrashed(t *testing.T) {
	f := newFixture(t)
	w, fp, _, id := newWatch(t, f, 30, 0)
	fp.st = ProbeState{Running: true}
	w.Tick(time.Second)
	fp.st = ProbeState{}
	if !w.Tick(time.Second) {
		t.Fatalf("a finished run without a verdict must end the watch")
	}
	if v, _ := f.svc.View(id); v.Status != StatusBlocked || v.Verdict.BlockedReason != ReasonCrashed {
		t.Fatalf("view = %+v", v)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func TestPostCheck_LeadWorktreeChangedIsScopeViolation(t *testing.T) {
	f := newFixture(t)
	lead := t.TempDir()
	gitRun(t, lead, "init", "-q")
	if err := os.WriteFile(filepath.Join(lead, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, lead, "add", ".")
	gitRun(t, lead, "commit", "-qm", "a")
	before, err := Take(context.Background(), lead)
	if err != nil {
		t.Fatal(err)
	}
	w, fp, _, id := newWatch(t, f, 30, 0)
	w.Before = before
	w.LeadSnapshot = func() (Snapshot, error) { return Take(context.Background(), lead) }
	fp.st = ProbeState{Running: true}
	w.Tick(time.Second)
	// the "validator" edits a file in the LEAD's worktree
	if err := os.WriteFile(filepath.Join(lead, "a.txt"), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	fp.st = ProbeState{}
	if !w.Tick(time.Second) {
		t.Fatalf("expected done")
	}
	v, _ := f.svc.View(id)
	if !v.ScopeViolation || v.MergeReady {
		t.Fatalf("a changed lead worktree must be a scope violation: %+v", v)
	}
}

func TestPostCheck_UnchangedLeadWorktreeIsClean(t *testing.T) {
	f := newFixture(t)
	lead := t.TempDir()
	gitRun(t, lead, "init", "-q")
	if err := os.WriteFile(filepath.Join(lead, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, lead, "add", ".")
	gitRun(t, lead, "commit", "-qm", "a")
	before, _ := Take(context.Background(), lead)
	w, fp, _, id := newWatch(t, f, 30, 0)
	w.Before, w.LeadSnapshot = before, func() (Snapshot, error) { return Take(context.Background(), lead) }
	fp.st = ProbeState{Running: true}
	w.Tick(time.Second)
	fp.st = ProbeState{}
	w.Tick(time.Second)
	if v, _ := f.svc.View(id); v.ScopeViolation {
		t.Fatalf("an untouched lead worktree must not be flagged")
	}
}

func TestIsAncestor(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "commit", "-q", "--allow-empty", "-m", "one")
	first := strings.TrimSpace(mustOut(t, dir, "rev-parse", "HEAD"))
	gitRun(t, dir, "commit", "-q", "--allow-empty", "-m", "two")
	if ok, err := IsAncestor(context.Background(), dir, first, "HEAD"); err != nil || !ok {
		t.Fatalf("first is an ancestor: %v %v", ok, err)
	}
	if ok, _ := IsAncestor(context.Background(), dir, strings.Repeat("0", 40), "HEAD"); ok {
		t.Fatalf("an unknown commit is not an ancestor")
	}
	if _, err := IsAncestor(context.Background(), dir, "HEAD", "HEAD"); err == nil {
		t.Fatalf("a non-sha must be refused before reaching git")
	}
}

func mustOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitOut(context.Background(), dir, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// ---- scope snapshot ----

func newLeadRepo(t *testing.T) string {
	t.Helper()
	lead := t.TempDir()
	gitRun(t, lead, "init", "-q")
	if err := os.WriteFile(filepath.Join(lead, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, lead, "add", ".")
	gitRun(t, lead, "commit", "-qm", "a")
	return lead
}

func fileBytes(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestSnapshot_DoesNotRewriteTheIndex: a plain `git status` refreshes and rewrites .git/index (and takes index.lock),
// so a "read-only" scope check would itself change the lead's worktree state and race the lead's own git commands.
// Take must leave the index byte for byte as it found it.
func TestSnapshot_DoesNotRewriteTheIndex(t *testing.T) {
	lead := newLeadRepo(t)
	index := filepath.Join(lead, ".git", "index")
	stale := func() {
		// A newer mtime than the index recorded: the stat cache is stale, the case a plain status refreshes.
		later := time.Now().Add(2 * time.Second)
		if err := os.Chtimes(filepath.Join(lead, "a.txt"), later, later); err != nil {
			t.Fatal(err)
		}
	}
	stale()
	before := fileBytes(t, index)
	if _, err := Take(context.Background(), lead); err != nil {
		t.Fatal(err)
	}
	if fileBytes(t, index) != before {
		t.Fatalf("Take rewrote .git/index")
	}
	// The control: the same state under a plain git status DOES rewrite it, so the assertion above is meaningful.
	gitRun(t, lead, "status", "--porcelain")
	if fileBytes(t, index) == before {
		t.Fatalf("control: a plain git status was expected to refresh the index")
	}
}

func TestSnapshotChanged_NamesWhatChanged(t *testing.T) {
	lead := newLeadRepo(t)
	base, err := Take(context.Background(), lead)
	if err != nil {
		t.Fatal(err)
	}
	if got := base.Changed(base); len(got) != 0 {
		t.Fatalf("identical snapshots differ: %v", got)
	}
	if err := os.WriteFile(filepath.Join(lead, "b.txt"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lead, "a.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, lead, "commit", "-qam", "more")
	after, _ := Take(context.Background(), lead)
	got := strings.Join(base.Changed(after), ",")
	if got != "HEAD,b.txt" {
		t.Fatalf("Changed = %q, want HEAD,b.txt (a.txt was committed, b.txt is untracked)", got)
	}
}

// TestPostCheck_LateWriteByAnotherWriterIsNamed documents the check's by-design limit: it sees that the lead's
// worktree changed, not WHO changed it. Something the lead's own tools write after the baseline (here a file, like
// the fake agent's session-init-method) is reported as a scope violation, and the verdict names the path so a person
// can tell it from a validator edit.
func TestPostCheck_LateWriteByAnotherWriterIsNamed(t *testing.T) {
	f := newFixture(t)
	lead := newLeadRepo(t)
	before, _ := Take(context.Background(), lead)
	w, fp, _, id := newWatch(t, f, 30, 0)
	w.Before, w.LeadSnapshot = before, func() (Snapshot, error) { return Take(context.Background(), lead) }
	fp.st = ProbeState{Running: true}
	w.Tick(time.Second)
	if err := os.WriteFile(filepath.Join(lead, "session-init-method"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fp.st = ProbeState{}
	w.Tick(time.Second)
	v, _ := f.svc.View(id)
	if !v.ScopeViolation || v.Verdict.BlockedReason != ReasonScopeViolation || !strings.Contains(v.Verdict.Summary, "session-init-method") {
		t.Fatalf("view = %+v", v)
	}
}

// TestPostCheck_BaselineAfterSettleIsClean is the mechanism behind the old flake, made deterministic: the same late
// writer is NOT a violation when it finished before the baseline was taken.
func TestPostCheck_BaselineAfterSettleIsClean(t *testing.T) {
	f := newFixture(t)
	lead := newLeadRepo(t)
	if err := os.WriteFile(filepath.Join(lead, "session-init-method"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	before, _ := Take(context.Background(), lead) // taken after the writer settled
	w, fp, _, id := newWatch(t, f, 30, 0)
	w.Before, w.LeadSnapshot = before, func() (Snapshot, error) { return Take(context.Background(), lead) }
	fp.st = ProbeState{Running: true}
	w.Tick(time.Second)
	fp.st = ProbeState{}
	w.Tick(time.Second)
	if v, _ := f.svc.View(id); v.ScopeViolation || v.Verdict.BlockedReason != ReasonCrashed {
		t.Fatalf("view = %+v", v)
	}
}
