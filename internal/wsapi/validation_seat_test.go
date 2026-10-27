package wsapi

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/profiles"
	"github.com/spacingmind/smind/internal/runs"
	"github.com/spacingmind/smind/internal/store"
	"github.com/spacingmind/smind/internal/validation"
)

const seatContractDoc = `---
v: 1
milestone: "seat test"
base: origin/develop
acs:
  - { id: AC1, text: "one", covers: { tests: [TestA] } }
tests:
  - { name: TestA, run: "go test ./x" }
limits: { budgetUsd: 1.5, maxMinutes: 30 }
---
SECRET-LEAD-REASONING
`

// seatEnv is an identEnv whose seat task is the Lead, with a committed hang
// scenario (so the validator's worktree at that sha hangs too) and a
// "validator" profile.
type seatEnv struct {
	*identEnv
	seat *validationSeat
	svc  *validation.Service
	head string
	lead *requestContext
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newSeatEnv(t *testing.T) *seatEnv {
	t.Helper()
	e := &seatEnv{identEnv: newIdentEnv(t, "hang")}
	wt := *e.seatTask.WorktreePath
	// The Lead's own (fake) agent writes session-init-method into its worktree when its session starts, on its own
	// goroutine. The scope check compares the Lead worktree before and after a validation, so the baseline must be
	// taken after the Lead has settled: wait for the file instead of racing it (it was an intermittent false
	// scope_violation under load).
	waitForFile(t, filepath.Join(wt, "session-init-method"))
	gitIn(t, wt, "add", "-f", "scenario")
	gitIn(t, wt, "commit", "-qm", "scenario")
	e.head = gitIn(t, wt, "rev-parse", "HEAD")
	if _, err := e.db.CreateAgentProfile(store.AgentProfile{Name: "validator", Provider: "glm"}); err != nil {
		t.Fatal(err)
	}
	e.svc = newValidationService(e.db, nil)
	e.svc.EvidenceRoot = t.TempDir()
	e.seat = newValidationSeat(e.svc, e.wm, e.db, e.api.Runs, newTestRunner(e.wm), profiles.New(e.db))
	e.seat.watchPeriod = 40 * time.Millisecond
	e.api.hs["validation.request"] = e.seat.request
	e.lead = &requestContext{runScoped: true, ident: runIdentity{runID: e.runID, taskID: e.seatTask.ID, chatID: e.chatID}}
	if _, err := budgetCall(t, e.api, e.lead, "validation.contract.submit", map[string]any{"taskId": e.seatTask.ID, "source": "inline", "body": seatContractDoc}); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *seatEnv) request(t *testing.T, rc *requestContext, params map[string]any) (requestResult, error) {
	t.Helper()
	raw, err := budgetCall(t, e.api, rc, "validation.request", params)
	if err != nil {
		return requestResult{}, err
	}
	var out requestResult
	_ = json.Unmarshal(raw, &out)
	return out, nil
}

func (e *seatEnv) validatorRC(r requestResult) *requestContext {
	return &requestContext{runScoped: true, ident: runIdentity{runID: r.RunID, taskID: r.ValidatorTaskID}}
}

func (e *seatEnv) tokenOf(r requestResult) string {
	env, ok := e.seat.runEnv(r.ValidatorTaskID)
	if !ok {
		return ""
	}
	return env["SMIND_VERDICT_TOKEN"]
}

func (e *seatEnv) passVerdict(t *testing.T, r requestResult) json.RawMessage {
	t.Helper()
	ct, _ := e.svc.GetContract(r.ContractID)
	raw, _ := json.Marshal(map[string]any{"v": 1, "contractId": r.ContractID, "contractSha256": ct.Sha256, "commitSha": r.CommitSHA,
		"status": "pass", "summary": "all good", "items": []any{
			map[string]any{"kind": "test", "id": "TestA", "status": "pass", "evidence": []any{map[string]any{"type": "excerpt", "text": "ok"}}},
			map[string]any{"kind": "ac", "id": "AC1", "status": "pass", "via": []string{"TestA"}},
		}})
	return raw
}

func (e *seatEnv) waitDone(t *testing.T, id int64) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if v, err := e.svc.View(id); err == nil && v.Status != "running" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	rows, _ := e.db.ListRunsByTask(func() int64 { v, _ := e.db.GetValidation(id); return *v.ValidatorTaskID }(), 10)
	t.Fatalf("validation %d did not finish; probe=%+v runs=%+v", id, e.seat.probe(func() int64 { v, _ := e.db.GetValidation(id); return *v.ValidatorTaskID }()), rows)
}

func (e *seatEnv) stopValidator(t *testing.T, r requestResult) {
	t.Helper()
	_ = e.api.Runs.Stop(r.RunID)
	waitRunEnd(t, e.api.Runs, r.RunID)
}

func waitRunEnd(t *testing.T, reg *runs.Registry, runID string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if st, err := reg.State(runID); err == nil && st.Status != runs.StatusRunning {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("run %s did not end", runID)
}

func TestValidatorSeat_E2E_FakeLLM(t *testing.T) {
	t.Parallel()
	e := newSeatEnv(t)
	r, err := e.request(t, e.lead, map[string]any{"taskId": e.seatTask.ID})
	if err != nil {
		t.Fatal(err)
	}
	if r.CommitSHA != e.head || r.RunID == "" {
		t.Fatalf("request = %+v", r)
	}
	// the validator task is a child of the Lead, at exactly the sha, in its own worktree
	vt, _ := e.db.GetTask(r.ValidatorTaskID)
	if vt.ParentTaskID == nil || *vt.ParentTaskID != e.seatTask.ID || vt.WorktreePath == nil || *vt.WorktreePath == *e.seatTask.WorktreePath {
		t.Fatalf("validator task = %+v", vt)
	}
	if got := gitIn(t, *vt.WorktreePath, "rev-parse", "HEAD"); got != e.head {
		t.Fatalf("validator worktree at %s, want %s", got, e.head)
	}
	// the fake validator submits its verdict with the one-time token
	tok := e.tokenOf(r)
	if tok == "" {
		t.Fatalf("the validator's run must carry a verdict token")
	}
	res := e.call(t, e.validatorRC(r), "validation.verdict.submit", map[string]any{"validationId": r.ValidationID, "token": tok, "verdict": e.passVerdict(t, r)})
	if res["status"] != "pass" || res["mergeReady"] != true {
		t.Fatalf("verdict result = %v", res)
	}
	// the Lead reads it, merge-ready at this sha
	got := e.call(t, e.lead, "validation.get", map[string]any{"id": r.ValidationID})
	if got["mergeReady"] != true {
		t.Fatalf("validation.get = %v", got)
	}
	// a new commit makes it stale
	gitIn(t, *e.seatTask.WorktreePath, "commit", "-q", "--allow-empty", "-m", "more")
	got = e.call(t, e.lead, "validation.get", map[string]any{"id": r.ValidationID})
	if got["mergeReady"] == true || got["stale"] != true {
		t.Fatalf("after a push the verdict must be stale: %v", got)
	}
	e.stopValidator(t, r)
	// the token is gone once the verdict is in
	if _, ok := e.seat.runEnv(r.ValidatorTaskID); ok {
		t.Fatalf("a finished validation must not hand out its token")
	}
}

func TestValidatorPrompt_E2E_HasNoLeadReasoning(t *testing.T) {
	t.Parallel()
	e := newSeatEnv(t)
	r, err := e.request(t, e.lead, map[string]any{"taskId": e.seatTask.ID})
	if err != nil {
		t.Fatal(err)
	}
	run, err := e.db.GetRun(r.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(run.Prompt, "SECRET-LEAD-REASONING") || !strings.Contains(run.Prompt, "validation #") || !strings.Contains(run.Prompt, "TestA") {
		t.Fatalf("validator prompt = %q", run.Prompt)
	}
	e.stopValidator(t, r)
	e.waitDone(t, r.ValidationID)
}

func TestRequest_OnlyOwnTaskOrHuman(t *testing.T) {
	t.Parallel()
	e := newSeatEnv(t)
	other := &requestContext{runScoped: true, ident: runIdentity{runID: "x", taskID: e.seatTask.ID + 1000}}
	_, err := e.request(t, other, map[string]any{"taskId": e.seatTask.ID})
	wantErrCode(t, err, codeForbidden)
	r, err := e.request(t, &requestContext{}, map[string]any{"taskId": e.seatTask.ID})
	if err != nil {
		t.Fatalf("a human may request: %v", err)
	}
	e.stopValidator(t, r)
	e.waitDone(t, r.ValidationID)
}

func TestRequest_SingleFlightPerTask(t *testing.T) {
	t.Parallel()
	e := newSeatEnv(t)
	r, err := e.request(t, e.lead, map[string]any{"taskId": e.seatTask.ID})
	if err != nil {
		t.Fatal(err)
	}
	_, err = e.request(t, e.lead, map[string]any{"taskId": e.seatTask.ID})
	wantErrCode(t, err, codeInvalid)
	e.stopValidator(t, r)
	e.waitDone(t, r.ValidationID)
}

func TestRequest_RefusesNonAncestorSha(t *testing.T) {
	t.Parallel()
	e := newSeatEnv(t)
	_, err := e.request(t, e.lead, map[string]any{"taskId": e.seatTask.ID, "commitSha": strings.Repeat("0", 40)})
	wantErrCode(t, err, codeInvalid)
	_, err = e.request(t, e.lead, map[string]any{"taskId": e.seatTask.ID, "commitSha": "HEAD"})
	wantErrCode(t, err, codeInvalid)
	if l, _ := e.svc.List(e.seatTask.ID); len(l) != 0 {
		t.Fatalf("a refused request left a validation: %v", l)
	}
}

func TestRequest_OverBudgetRefusedForAgentNotHuman(t *testing.T) {
	t.Parallel()
	e := newSeatEnv(t)
	if err := e.db.SetTaskBudget(e.seatTask.ID, fptr(0.5)); err != nil {
		t.Fatal(err)
	}
	spendOn(t, e.db, e.seatTask.ID, "spent-1", fptr(1))
	_, err := e.request(t, e.lead, map[string]any{"taskId": e.seatTask.ID})
	wantErrCode(t, err, "budget_exceeded")
	r, err := e.request(t, &requestContext{}, map[string]any{"taskId": e.seatTask.ID})
	if err != nil {
		t.Fatalf("a human is never refused by the budget: %v", err)
	}
	e.stopValidator(t, r)
	e.waitDone(t, r.ValidationID)
}

func TestRequest_NeedsContractAndProfile(t *testing.T) {
	t.Parallel()
	e := newIdentEnv(t, "hang")
	svc := newValidationService(e.db, nil)
	seat := newValidationSeat(svc, e.wm, e.db, e.api.Runs, newTestRunner(e.wm), profiles.New(e.db))
	lead := &requestContext{runScoped: true, ident: runIdentity{runID: e.runID, taskID: e.seatTask.ID, chatID: e.chatID}}
	raw, _ := json.Marshal(map[string]any{"taskId": e.seatTask.ID})
	_, err := seat.request(context.Background(), lead, raw)
	wantErrCode(t, err, codeNotFound) // no contract yet
	if _, err := svc.SubmitContract(e.seatTask.ID, []byte(seatContractDoc)); err != nil {
		t.Fatal(err)
	}
	_, err = seat.request(context.Background(), lead, raw)
	if r := wantErrCode(t, err, codeInvalid); !strings.Contains(r.Message, "profile seed roles") {
		t.Fatalf("message = %q", r.Message)
	}
}

func TestValidatorEnv_OnlyForTheValidatorTask(t *testing.T) {
	t.Parallel()
	e := newSeatEnv(t)
	r, err := e.request(t, e.lead, map[string]any{"taskId": e.seatTask.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := e.seat.runEnv(e.seatTask.ID); ok {
		t.Fatalf("the Lead's task must never get a verdict token")
	}
	// the Lead cannot use anything it can see to write the verdict
	_, err = budgetCall(t, e.api, e.lead, "validation.verdict.submit", map[string]any{"validationId": r.ValidationID, "token": "guess", "verdict": e.passVerdict(t, r)})
	wantErrCode(t, err, codeForbidden)
	_, err = budgetCall(t, e.api, e.validatorRC(r), "validation.verdict.submit", map[string]any{"validationId": r.ValidationID, "token": "", "verdict": e.passVerdict(t, r)})
	wantErrCode(t, err, codeForbidden)
	e.stopValidator(t, r)
	e.waitDone(t, r.ValidationID)
}

func TestPostCheck_ValidatorWritingLeadWorktreeIsScopeViolation(t *testing.T) {
	t.Parallel()
	e := newSeatEnv(t)
	r, err := e.request(t, e.lead, map[string]any{"taskId": e.seatTask.ID})
	if err != nil {
		t.Fatal(err)
	}
	if st, err := e.api.Runs.State(r.RunID); err != nil || st.Status != runs.StatusRunning {
		rr, _ := e.db.GetRun(r.RunID)
		t.Fatalf("validator run is %v (%v) %+v", st.Status, err, rr)
	}
	// the "validator" writes into the Lead's worktree
	if err := os.WriteFile(filepath.Join(*e.seatTask.WorktreePath, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.stopValidator(t, r)
	e.waitDone(t, r.ValidationID)
	v, _ := e.svc.View(r.ValidationID)
	if !v.ScopeViolation || v.MergeReady || v.Verdict.BlockedReason != "scope_violation" {
		t.Fatalf("view = %+v", v)
	}
}

func TestRunEndWithoutVerdictIsBlockedCrashed_E2E(t *testing.T) {
	t.Parallel()
	e := newSeatEnv(t)
	r, err := e.request(t, e.lead, map[string]any{"taskId": e.seatTask.ID})
	if err != nil {
		t.Fatal(err)
	}
	e.stopValidator(t, r)
	e.waitDone(t, r.ValidationID)
	v, _ := e.svc.View(r.ValidationID)
	if v.Status != "blocked" || v.Verdict.BlockedReason != "crashed" || v.MergeReady {
		t.Fatalf("view = %+v", v)
	}
}

func (e *seatEnv) call(t *testing.T, rc *requestContext, method string, params any) map[string]any {
	t.Helper()
	raw, err := budgetCall(t, e.api, rc, method, params)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

// waitForFile polls until path exists (bounded, no fixed sleep).
func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s never appeared", path)
}
