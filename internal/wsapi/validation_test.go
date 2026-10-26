package wsapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const vContractDoc = `---
v: 1
milestone: "P1"
base: origin/develop
acs:
  - { id: AC1, text: "one", covers: { tests: [TestA] } }
tests:
  - { name: TestA, run: "go test ./x" }
---
secret reasoning of the lead
`

// vEnv is an identEnv whose seat task plays the Lead.
type vEnv struct {
	*identEnv
	contractID int64
	valID      int64
	token      string
}

const vValidatorRun = "run-validator"

func newVEnv(t *testing.T) *vEnv {
	t.Helper()
	e := &vEnv{identEnv: newIdentEnv(t, "hang")}
	lead := &requestContext{runScoped: true, ident: runIdentity{runID: e.runID, taskID: e.seatTask.ID}}
	res := e.call(t, lead, "validation.contract.submit", map[string]any{"taskId": e.seatTask.ID, "source": "inline", "body": vContractDoc})
	e.contractID = int64(res["id"].(float64))
	svc := newValidationService(e.db, nil)
	val, token, err := svc.CreateValidation(e.seatTask.ID, e.contractID, strings.Repeat("a", 40))
	if err != nil {
		t.Fatal(err)
	}
	scratch := t.TempDir()
	if err := svc.BindRun(val.ID, vValidatorRun, 999, scratch); err != nil {
		t.Fatal(err)
	}
	e.valID, e.token = val.ID, token
	return e
}

func (e *vEnv) call(t *testing.T, rc *requestContext, method string, params any) map[string]any {
	t.Helper()
	raw, err := budgetCall(t, e.api, rc, method, params)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	return out
}

func (e *vEnv) validatorRC() *requestContext {
	return &requestContext{runScoped: true, ident: runIdentity{runID: vValidatorRun, taskID: 999}}
}

func (e *vEnv) verdict(status string) json.RawMessage {
	ct, _ := newValidationService(e.db, nil).GetContract(e.contractID)
	v := map[string]any{"v": 1, "contractId": e.contractID, "contractSha256": ct.Sha256, "commitSha": strings.Repeat("a", 40),
		"status": status, "blockedReason": "no_toolchain", "summary": "no go toolchain installed", "items": []any{}}
	raw, _ := json.Marshal(v)
	return raw
}

func TestVerdictSubmit_LeadCannotWriteVerdict(t *testing.T) {
	t.Parallel()
	e := newVEnv(t)
	params := map[string]any{"validationId": e.valID, "token": e.token, "verdict": e.verdict("blocked")}

	// the Lead's own run, even holding a (leaked) token, is not the validation's run
	lead := &requestContext{runScoped: true, ident: runIdentity{runID: e.runID, taskID: e.seatTask.ID}}
	_, err := budgetCall(t, e.api, lead, "validation.verdict.submit", params)
	wantErrCode(t, err, codeForbidden)
	// without the token
	noTok := map[string]any{"validationId": e.valID, "token": "", "verdict": e.verdict("blocked")}
	_, err = budgetCall(t, e.api, e.validatorRC(), "validation.verdict.submit", noTok)
	wantErrCode(t, err, codeForbidden)
	// a human (not run-scoped) is not the validator path either
	_, err = budgetCall(t, e.api, &requestContext{}, "validation.verdict.submit", params)
	wantErrCode(t, err, codeForbidden)
	// the Lead's identified connection reaches the handler (the method is run-scoped) and is refused by it
	resp := callWS(t, e.identified(t), "validation.verdict.submit", params)
	wantForbidden(t, resp, "")

	if v, _ := e.db.GetValidation(e.valID); v.VerdictJSON != "" {
		t.Fatalf("a refused call wrote a verdict")
	}
	// the validator's run with its token succeeds
	res := e.call(t, e.validatorRC(), "validation.verdict.submit", params)
	if res["status"] != "blocked" {
		t.Fatalf("validator verdict = %v", res)
	}
}

func TestValidationContractGet_OmitsBodyForValidator(t *testing.T) {
	t.Parallel()
	e := newVEnv(t)
	p := map[string]any{"contractId": e.contractID}
	lead := &requestContext{runScoped: true, ident: runIdentity{runID: e.runID, taskID: e.seatTask.ID}}
	if got := e.call(t, lead, "validation.contract.get", p)["body"]; got == nil || !strings.Contains(got.(string), "secret reasoning") {
		t.Fatalf("the Lead must see the markdown body, got %v", got)
	}
	if got, ok := e.call(t, e.validatorRC(), "validation.contract.get", p)["body"]; ok {
		t.Fatalf("the validator must not receive the markdown body, got %v", got)
	}
	_, err := budgetCall(t, e.api, &requestContext{runScoped: true, ident: runIdentity{runID: "other", taskID: 12345}}, "validation.contract.get", p)
	wantErrCode(t, err, codeForbidden)
}

func TestContractSubmit_RunScopedOnlyOwnTask(t *testing.T) {
	t.Parallel()
	e := newVEnv(t)
	other := &requestContext{runScoped: true, ident: runIdentity{runID: "x", taskID: e.seatTask.ID + 1000}}
	_, err := budgetCall(t, e.api, other, "validation.contract.submit", map[string]any{"taskId": e.seatTask.ID, "source": "inline", "body": vContractDoc})
	wantErrCode(t, err, codeForbidden)
	// an invalid contract is a model-readable invalid_params error
	own := &requestContext{runScoped: true, ident: runIdentity{runID: e.runID, taskID: e.seatTask.ID}}
	_, err = budgetCall(t, e.api, own, "validation.contract.submit", map[string]any{"taskId": e.seatTask.ID, "source": "inline", "body": "no front matter"})
	if r := wantErrCode(t, err, codeInvalid); !strings.Contains(r.Message, "front matter") {
		t.Fatalf("message = %q", r.Message)
	}
}

func TestContractSubmit_FileSourceReadsInsideWorktree(t *testing.T) {
	t.Parallel()
	e := newVEnv(t)
	wt := *e.seatTask.WorktreePath
	if err := os.WriteFile(filepath.Join(wt, "x.contract.md"), []byte(vContractDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	human := &requestContext{}
	if res := e.call(t, human, "validation.contract.submit", map[string]any{"taskId": e.seatTask.ID, "source": "file", "path": "x.contract.md"}); res["version"].(float64) != 2 {
		t.Fatalf("version = %v", res["version"])
	}
	_, err := budgetCall(t, e.api, human, "validation.contract.submit", map[string]any{"taskId": e.seatTask.ID, "source": "file", "path": "../x.md"})
	wantErrCode(t, err, codeInvalid)
}

func TestValidationReads_OpenOverRelayAndScopedForAgents(t *testing.T) {
	t.Parallel()
	e := newVEnv(t)
	relay := &requestContext{local: false}
	if got := e.call(t, relay, "validation.get", map[string]any{"id": e.valID}); got["status"] != "running" {
		t.Fatalf("relay validation.get = %v", got)
	}
	raw, err := budgetCall(t, e.api, relay, "validation.list", map[string]any{"taskId": e.seatTask.ID})
	if err != nil || !strings.Contains(string(raw), `"running"`) {
		t.Fatalf("relay validation.list = %s, %v", raw, err)
	}
	other := &requestContext{runScoped: true, ident: runIdentity{runID: "x", taskID: e.seatTask.ID + 1000}}
	_, err = budgetCall(t, e.api, other, "validation.list", map[string]any{"taskId": e.seatTask.ID})
	wantErrCode(t, err, codeForbidden)
	_, err = budgetCall(t, e.api, other, "validation.get", map[string]any{"id": e.valID})
	wantErrCode(t, err, codeNotFound)
}

func TestNoMergeRPC(t *testing.T) {
	t.Parallel()
	e := newVEnv(t)
	allowed := map[string]bool{}
	for _, m := range validationMethods {
		allowed[m] = true
	}
	for name := range e.api.hs {
		if strings.HasPrefix(name, "validation.") && !allowed[name] {
			t.Errorf("unexpected validation RPC %q", name)
		}
		if strings.HasPrefix(name, "validation.") && (strings.Contains(name, "merge") || strings.Contains(name, "waive") || strings.Contains(name, "approve")) {
			t.Errorf("validation RPC %q must not merge, waive or approve", name)
		}
	}
	for _, m := range validationMethods {
		if e.api.hs[m] == nil {
			t.Errorf("%s is not registered", m)
		}
		if !runScopedSet[m] {
			t.Errorf("%s must be run-scoped: the Lead and the validator are agent runs", m)
		}
	}
}
