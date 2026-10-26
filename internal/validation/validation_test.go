package validation

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/store"
)

const sha1x = "1111111111111111111111111111111111111111"
const sha2x = "2222222222222222222222222222222222222222"

const goodDoc = `---
v: 1
milestone: "P1"
base: origin/develop
acs:
  - id: AC1
    text: "A stale-sha verdict is not merge-ready."
    covers: { tests: [TestA], mutations: [M1], commands: [C1] }
  - id: AC2
    text: "Looks right."
    covers: { frames: [F1], tests: [TestA] }
tests:
  - { name: TestA, run: "go test ./x -run '^TestA$'", timeoutSec: 60 }
  - { name: TestQ, run: "go test ./x -run '^TestQ$'", quarantine: true }
mutations:
  - { id: M1, file: internal/x/y.go, change: "flip it", expectFail: [TestA] }
frames:
  - { id: F1, surface: "web /inbox", state: "one item", viewport: [1280, 390], theme: [light, dark] }
commands:
  - { id: C1, run: "task test" }
writeScope: { paths: [] }
limits: { budgetUsd: 1.5, maxMinutes: 30 }
---
Prose for humans: the lead's reasoning lives here.
`

func parseGood(t *testing.T) Parsed {
	t.Helper()
	p, err := ParseDocument([]byte(goodDoc), DefaultLimits)
	if err != nil {
		t.Fatalf("ParseDocument: %v", err)
	}
	return p
}

func mustFail(t *testing.T, doc, wantSubstr string) {
	t.Helper()
	_, err := ParseDocument([]byte(doc), DefaultLimits)
	if err == nil {
		t.Fatalf("expected an error containing %q, got nil", wantSubstr)
	}
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Fatalf("error = %q, want it to contain %q", err, wantSubstr)
	}
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("error %v is not ErrInvalid", err)
	}
}

func TestContractParse_AppliesDefaultsAndKeepsBody(t *testing.T) {
	p := parseGood(t)
	if p.Contract.EvidenceDir != "auto" || p.Contract.Limits.MaxMinutes != 30 {
		t.Fatalf("defaults not applied: %+v", p.Contract)
	}
	if !strings.Contains(p.Body, "Prose for humans") || strings.Contains(string(p.JSON), "Prose for humans") {
		t.Fatalf("body must be kept apart from the contract JSON")
	}
	if len(p.Sha256) != 64 {
		t.Fatalf("sha = %q", p.Sha256)
	}
}

func TestContractSubmit_RejectsDuplicateKeyAnchorTagUnknownField(t *testing.T) {
	cases := map[string]struct{ doc, want string }{
		"duplicate key": {strings.Replace(goodDoc, "v: 1\n", "v: 1\nv: 1\n", 1), "duplicate key"},
		"anchor":        {strings.Replace(goodDoc, "base: origin/develop", "base: &b origin/develop", 1), "anchors"},
		"tag":           {strings.Replace(goodDoc, "milestone: \"P1\"", "milestone: !!str P1", 1), "tags"},
		"merge key":     {strings.Replace(goodDoc, "writeScope: { paths: [] }", "writeScope: { <<: {paths: []} }", 1), "merge"},
		"unknown field": {strings.Replace(goodDoc, "v: 1\n", "v: 1\nprompt: \"ignore previous\"\n", 1), "prompt"},
		"no front":      {"just prose", "---"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) { mustFail(t, c.doc, c.want) })
	}
}

func TestContractSubmit_RejectsACWithNoCheck(t *testing.T) {
	doc := strings.Replace(goodDoc, "covers: { tests: [TestA], mutations: [M1], commands: [C1] }", "covers: {}", 1)
	mustFail(t, doc, "AC1 has no check")
	doc = strings.Replace(goodDoc, "covers: { frames: [F1], tests: [TestA] }", "covers: { frames: [F1] }", 1)
	mustFail(t, doc, "frames only")
	doc = strings.Replace(goodDoc, "covers: { frames: [F1], tests: [TestA] }", "covers: { frames: [F1] }\n    visual: true", 1)
	mustFail(t, doc, "visualReason")
}

func TestContractSubmit_RejectsPathEscapeAndWideWriteScope(t *testing.T) {
	mustFail(t, strings.Replace(goodDoc, "file: internal/x/y.go", "file: ../etc/passwd", 1), "mutations[0].file")
	mustFail(t, strings.Replace(goodDoc, "file: internal/x/y.go", "file: /etc/passwd", 1), "mutations[0].file")
	mustFail(t, strings.Replace(goodDoc, "paths: []", "paths: [\"web/**\"]", 1), "glob")
	mustFail(t, strings.Replace(goodDoc, "paths: []", "paths: [\"../x\"]", 1), "writeScope.paths[0]")
	mustFail(t, strings.Replace(goodDoc, "paths: []", "paths: [\".\"]", 1), "writeScope.paths[0]")
	mustFail(t, strings.Replace(goodDoc, "expectFail: [TestA]", "expectFail: [TestNope]", 1), "not in tests")
	mustFail(t, strings.Replace(goodDoc, "covers: { frames: [F1], tests: [TestA] }", "covers: { tests: [TestA], frames: [F9] }", 1), "F9")
}

func TestContractSubmit_RejectsOverCaps(t *testing.T) {
	var b strings.Builder
	b.WriteString("---\nv: 1\nmilestone: m\nbase: origin/develop\nacs:\n")
	for i := 0; i < MaxACs+1; i++ {
		b.WriteString("  - { id: AC" + strings.Repeat("1", 1) + string(rune('A'+i%26)) + string(rune('A'+i/26)) + ", text: t, covers: { tests: [T] } }\n")
	}
	b.WriteString("tests:\n  - { name: T, run: x }\n---\n")
	mustFail(t, b.String(), "acs: has 41 items")

	big := "---\nv: 1\nmilestone: m\nbase: origin/develop\nacs:\n  - { id: AC1, text: t, covers: { tests: [T] } }\ntests:\n  - { name: T, run: x }\n---\n" + strings.Repeat("x", MaxBodyBytes+1)
	mustFail(t, big, "body")
}

func TestContractSubmit_RejectsBudgetAboveCeiling(t *testing.T) {
	mustFail(t, strings.Replace(goodDoc, "budgetUsd: 1.5", "budgetUsd: 6", 1), "ceiling")
	p, err := ParseDocument([]byte(strings.Replace(goodDoc, "limits: { budgetUsd: 1.5, maxMinutes: 30 }\n", "", 1)), DefaultLimits)
	if err != nil || p.Contract.Limits.BudgetUSD != 1.5 {
		t.Fatalf("default budget: %v %+v", err, p.Contract.Limits)
	}
}

// ---- verdict ----

func restored() *bool { b := true; return &b }

func ex(text string) []Evidence { return []Evidence{{Type: "excerpt", Text: text}} }

func goodVerdict(c Contract) Verdict {
	return Verdict{V: 1, ContractID: 1, CommitSHA: sha1x, Status: StatusPass, Summary: "ok",
		Items: []Item{
			{Kind: KindTest, ID: "TestA", Status: "pass", Evidence: ex("ok")},
			{Kind: KindTest, ID: "TestQ", Status: "pass", Evidence: ex("ok")},
			{Kind: KindMutation, ID: "M1", Status: "pass", Restored: restored(), Evidence: ex("--- FAIL: TestA")},
			{Kind: KindCommand, ID: "C1", Status: "pass", Evidence: ex("exit 0")},
			{Kind: KindFrame, ID: "F1", Status: "pass", Note: "list shows one row", Evidence: []Evidence{{Type: "file", Path: "frames/F1.png"}}},
			{Kind: KindAC, ID: "AC1", Status: "pass", Via: []string{"TestA", "M1"}},
			{Kind: KindAC, ID: "AC2", Status: "pass", Via: []string{"F1", "TestA"}},
		}}
}

func TestVerdictSubmit_PassWithMissingItemRejected(t *testing.T) {
	c := parseGood(t).Contract
	if err := CheckVerdict(c, goodVerdict(c)); err != nil {
		t.Fatalf("complete pass rejected: %v", err)
	}
	v := goodVerdict(c)
	v.Items = v.Items[1:] // drop TestA
	err := CheckVerdict(c, v)
	if err == nil || !strings.Contains(err.Error(), `"TestA"`) {
		t.Fatalf("pass with a missing test item must be rejected naming it, got %v", err)
	}
	v = goodVerdict(c)
	v.Items[2].Restored = nil
	if err := CheckVerdict(c, v); err == nil || !strings.Contains(err.Error(), "restored") {
		t.Fatalf("mutation not restored must be rejected, got %v", err)
	}
	v = goodVerdict(c)
	v.Items[0].Evidence = nil
	if err := CheckVerdict(c, v); err == nil || !strings.Contains(err.Error(), "no evidence") {
		t.Fatalf("item without evidence must be rejected, got %v", err)
	}
	v = goodVerdict(c)
	v.Items[0].Status = "fail"
	if err := CheckVerdict(c, v); err == nil {
		t.Fatalf("a failing test must not allow pass")
	}
}

func TestVerdictSubmit_FrameWithoutObservationRejected(t *testing.T) {
	c := parseGood(t).Contract
	v := goodVerdict(c)
	v.Items[4].Note = ""
	if err := CheckVerdict(c, v); err == nil || !strings.Contains(err.Error(), "observed") {
		t.Fatalf("frame without a note must be rejected, got %v", err)
	}
}

func TestVerdictSubmit_FrameOnlyACRejectedUnlessVisual(t *testing.T) {
	c := parseGood(t).Contract
	v := goodVerdict(c)
	v.Items[6].Via = []string{"F1"} // AC2 rests only on a frame, but is not visual
	if err := CheckVerdict(c, v); err == nil || !strings.Contains(err.Error(), "only on frames") {
		t.Fatalf("frame-only AC must be rejected, got %v", err)
	}
	c.ACs[1].Visual, c.ACs[1].VisualReason = true, "pure layout"
	if err := CheckVerdict(c, v); err != nil {
		t.Fatalf("visual AC resting on a frame must pass: %v", err)
	}
}

func TestVerdictSubmit_FailNeedsFailingItem(t *testing.T) {
	c := parseGood(t).Contract
	v := Verdict{V: 1, CommitSHA: sha1x, Status: StatusFail, Summary: "x", Items: []Item{{Kind: KindTest, ID: "TestA", Status: "pass", Evidence: ex("ok")}}}
	if err := CheckVerdict(c, v); err == nil {
		t.Fatalf("fail without a failing item must be rejected")
	}
	v.Items[0].Status, v.Items[0].Evidence = "fail", nil
	if err := CheckVerdict(c, v); err == nil {
		t.Fatalf("fail without finding or excerpt must be rejected")
	}
	v.Items[0].Evidence = ex("--- FAIL: TestA")
	if err := CheckVerdict(c, v); err != nil {
		t.Fatalf("fail with a failing item and an excerpt must pass: %v", err)
	}
}

func TestVerdictSubmit_BlockedNeedsReason(t *testing.T) {
	c := parseGood(t).Contract
	v := Verdict{V: 1, CommitSHA: sha1x, Status: StatusBlocked, Summary: "no browser installed", Items: []Item{}}
	if err := CheckVerdict(c, v); err == nil {
		t.Fatalf("blocked without a reason must be rejected")
	}
	v.BlockedReason = ReasonNoBrowser
	if err := CheckVerdict(c, v); err != nil {
		t.Fatalf("blocked no_browser: %v", err)
	}
	for _, r := range []string{ReasonTimeout, ReasonBudget, ReasonCrashed, ReasonScopeViolation} {
		v.BlockedReason = r
		if err := CheckVerdict(c, v); err == nil {
			t.Fatalf("a validator must not claim %s", r)
		}
	}
}

func TestParseVerdict_RejectsUnknownField(t *testing.T) {
	if _, err := ParseVerdict([]byte(`{"v":1,"scopeViolation":false}`)); err == nil {
		t.Fatalf("a daemon-only field must be rejected")
	}
}

func TestMergeReady_PassAtHead(t *testing.T) {
	if ok, why := MergeReady(StatusPass, sha1x, false, sha1x, true); !ok {
		t.Fatalf("not ready: %s", why)
	}
}

func TestMergeReady_StaleShaNotReady(t *testing.T) {
	if ok, _ := MergeReady(StatusPass, sha1x, false, sha2x, true); ok {
		t.Fatalf("a verdict for another sha must not be merge-ready")
	}
	if ok, _ := MergeReady(StatusPass, sha1x, false, "", true); ok {
		t.Fatalf("unknown head must not be merge-ready")
	}
}

func TestMergeReady_SupersededContractNotReady(t *testing.T) {
	if ok, _ := MergeReady(StatusPass, sha1x, false, sha1x, false); ok {
		t.Fatalf("a superseded contract must not be merge-ready")
	}
}

func TestMergeReady_ScopeViolationNotReady(t *testing.T) {
	if ok, _ := MergeReady(StatusPass, sha1x, true, sha1x, true); ok {
		t.Fatalf("a scope violation must not be merge-ready")
	}
	if ok, _ := MergeReady(StatusFail, sha1x, false, sha1x, true); ok {
		t.Fatalf("a fail must not be merge-ready")
	}
}

// ---- service ----

type fixture struct {
	svc     *Service
	db      *store.Store
	taskID  int64
	head    *string
	scratch string
	events  *[]string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ws, err := db.CreateWorkspace(store.Workspace{Path: "/repo", Title: "repo", RoutingPolicy: "hard"})
	if err != nil {
		t.Fatal(err)
	}
	task, err := db.CreateTask(store.Task{WorkspaceID: ws.ID, Title: "lead", Status: "created"})
	if err != nil {
		t.Fatal(err)
	}
	head := sha1x
	events := &[]string{}
	scratch := t.TempDir()
	svc := &Service{DB: db, EvidenceRoot: filepath.Join(t.TempDir(), "validation"),
		Head:    func(int64) (string, error) { return head, nil },
		TaskDir: func(int64) (string, error) { return scratch, nil },
		Publish: func(_, _ int64, status string) { *events = append(*events, status) }}
	return fixture{svc: svc, db: db, taskID: task.ID, head: &head, scratch: scratch, events: events}
}

// started returns a validation bound to a run, with its token.
func (f fixture) started(t *testing.T) (store.Validation, string) {
	t.Helper()
	rec, err := f.svc.SubmitContract(f.taskID, []byte(goodDoc))
	if err != nil {
		t.Fatal(err)
	}
	val, token, err := f.svc.CreateValidation(f.taskID, rec.ID, sha1x)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.svc.BindRun(val.ID, "run-v", 99, f.scratch); err != nil {
		t.Fatal(err)
	}
	return val, token
}

func (f fixture) verdictJSON(t *testing.T, val store.Validation, mutate func(*Verdict)) []byte {
	t.Helper()
	ct, _ := f.svc.GetContract(val.ContractID)
	v := goodVerdict(ct.Contract)
	v.ContractID, v.ContractSha256 = ct.ID, ct.Sha256
	if mutate != nil {
		mutate(&v)
	}
	raw, _ := json.Marshal(v)
	return raw
}

func writeFrame(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "frames"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "frames", "F1.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestContractSubmit_ValidStoresVersion1(t *testing.T) {
	f := newFixture(t)
	rec, err := f.svc.SubmitContract(f.taskID, []byte(goodDoc))
	if err != nil || rec.Version != 1 {
		t.Fatalf("SubmitContract = %+v, %v", rec, err)
	}
}

func TestContractSubmit_SecondSubmitIsVersion2(t *testing.T) {
	f := newFixture(t)
	r1, _ := f.svc.SubmitContract(f.taskID, []byte(goodDoc))
	r2, err := f.svc.SubmitContract(f.taskID, []byte(strings.Replace(goodDoc, `milestone: "P1"`, `milestone: "P1b"`, 1)))
	if err != nil || r2.Version != 2 || r2.ID == r1.ID {
		t.Fatalf("second submit = %+v, %v", r2, err)
	}
	latest, _ := f.svc.LatestContract(f.taskID)
	if latest.ID != r2.ID {
		t.Fatalf("latest = %d, want %d", latest.ID, r2.ID)
	}
	old, _ := f.svc.GetContract(r1.ID)
	if old.Contract.Milestone != "P1" {
		t.Fatalf("version 1 was changed: %+v", old.Contract)
	}
}

func TestVerdictSubmit_RequiresToken(t *testing.T) {
	f := newFixture(t)
	val, _ := f.started(t)
	writeFrame(t, f.scratch)
	for _, tok := range []string{"", "nope"} {
		if _, err := f.svc.SubmitVerdict(val.ID, tok, "run-v", f.verdictJSON(t, val, nil)); !errors.Is(err, ErrBadToken) {
			t.Fatalf("token %q: err = %v, want ErrBadToken", tok, err)
		}
	}
}

func TestVerdictSubmit_WrongRunRefused(t *testing.T) {
	f := newFixture(t)
	val, token := f.started(t)
	writeFrame(t, f.scratch)
	if _, err := f.svc.SubmitVerdict(val.ID, token, "lead-run", f.verdictJSON(t, val, nil)); !errors.Is(err, ErrWrongRun) {
		t.Fatalf("err = %v, want ErrWrongRun", err)
	}
}

func TestVerdictSubmit_TokenOnceOnly(t *testing.T) {
	f := newFixture(t)
	val, token := f.started(t)
	writeFrame(t, f.scratch)
	v, err := f.svc.SubmitVerdict(val.ID, token, "run-v", f.verdictJSON(t, val, nil))
	if err != nil || v.Status != StatusPass {
		t.Fatalf("first submit = %+v, %v", v, err)
	}
	if _, err := f.svc.SubmitVerdict(val.ID, token, "run-v", f.verdictJSON(t, val, nil)); !errors.Is(err, ErrVerdictExists) {
		t.Fatalf("second submit err = %v, want ErrVerdictExists", err)
	}
}

func TestVerdictSubmit_RejectedVerdictKeepsTokenUsable(t *testing.T) {
	f := newFixture(t)
	val, token := f.started(t)
	writeFrame(t, f.scratch)
	bad := f.verdictJSON(t, val, func(v *Verdict) { v.Items = v.Items[1:] })
	if _, err := f.svc.SubmitVerdict(val.ID, token, "run-v", bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
	if _, err := f.svc.SubmitVerdict(val.ID, token, "run-v", f.verdictJSON(t, val, nil)); err != nil {
		t.Fatalf("a corrected verdict must be accepted after a rejected one: %v", err)
	}
}

func TestVerdictSubmit_EvidencePathEscapeRejected(t *testing.T) {
	f := newFixture(t)
	val, token := f.started(t)
	secret := filepath.Join(filepath.Dir(f.scratch), "secret.txt")
	if err := os.WriteFile(secret, []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFrame(t, f.scratch)
	// a symlink inside the scratch worktree pointing outside it
	if err := os.Symlink(secret, filepath.Join(f.scratch, "frames", "link.png")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	raw := f.verdictJSON(t, val, func(v *Verdict) { v.Items[4].Evidence[0].Path = "frames/link.png" })
	if _, err := f.svc.SubmitVerdict(val.ID, token, "run-v", raw); err == nil || !strings.Contains(err.Error(), "leaves the directory") {
		t.Fatalf("symlink escape err = %v", err)
	}
	for _, p := range []string{"../secret.txt", "/etc/passwd"} {
		p := p
		raw := f.verdictJSON(t, val, func(v *Verdict) { v.Items[4].Evidence[0].Path = p })
		if _, err := f.svc.SubmitVerdict(val.ID, token, "run-v", raw); !errors.Is(err, ErrInvalid) {
			t.Fatalf("path %q err = %v, want ErrInvalid", p, err)
		}
	}
}

func TestVerdictSubmit_CopiesEvidenceAndRehashes(t *testing.T) {
	f := newFixture(t)
	val, token := f.started(t)
	writeFrame(t, f.scratch)
	raw := f.verdictJSON(t, val, func(v *Verdict) { v.Items[4].Evidence[0].Sha256 = "forged" })
	v, err := f.svc.SubmitVerdict(val.ID, token, "run-v", raw)
	if err != nil {
		t.Fatal(err)
	}
	ev := v.Verdict.Items[4].Evidence[0]
	if ev.Sha256 == "forged" || len(ev.Sha256) != 64 {
		t.Fatalf("hash not recomputed: %q", ev.Sha256)
	}
	if _, err := os.Stat(filepath.Join(f.svc.EvidenceDir(val.ID), "frames", "F1.png")); err != nil {
		t.Fatalf("evidence not copied: %v", err)
	}
}

func TestVerdictSubmit_WrongCommitOrContractRejected(t *testing.T) {
	f := newFixture(t)
	val, token := f.started(t)
	writeFrame(t, f.scratch)
	if _, err := f.svc.SubmitVerdict(val.ID, token, "run-v", f.verdictJSON(t, val, func(v *Verdict) { v.CommitSHA = sha2x })); !errors.Is(err, ErrInvalid) {
		t.Fatalf("other commit err = %v", err)
	}
	if _, err := f.svc.SubmitVerdict(val.ID, token, "run-v", f.verdictJSON(t, val, func(v *Verdict) { v.ContractSha256 = "x" })); !errors.Is(err, ErrInvalid) {
		t.Fatalf("other contract err = %v", err)
	}
}

func TestView_MergeReadyFollowsHeadAndContract(t *testing.T) {
	f := newFixture(t)
	val, token := f.started(t)
	writeFrame(t, f.scratch)
	v, err := f.svc.SubmitVerdict(val.ID, token, "run-v", f.verdictJSON(t, val, nil))
	if err != nil || !v.MergeReady {
		t.Fatalf("fresh pass must be merge-ready: %+v %v", v, err)
	}
	*f.head = sha2x // a push
	v, _ = f.svc.View(val.ID)
	if v.MergeReady || !v.Stale {
		t.Fatalf("after a push the verdict must be stale: %+v", v)
	}
	*f.head = sha1x
	if _, err := f.svc.SubmitContract(f.taskID, []byte(strings.Replace(goodDoc, `milestone: "P1"`, `milestone: "P1c"`, 1))); err != nil {
		t.Fatal(err)
	}
	if v, _ = f.svc.View(val.ID); v.MergeReady || !v.Stale {
		t.Fatalf("a newer contract must make it stale: %+v", v)
	}
}

func TestView_OnlyNewestValidationCanBeReady(t *testing.T) {
	f := newFixture(t)
	val, token := f.started(t)
	writeFrame(t, f.scratch)
	if _, err := f.svc.SubmitVerdict(val.ID, token, "run-v", f.verdictJSON(t, val, nil)); err != nil {
		t.Fatal(err)
	}
	ct, _ := f.svc.LatestContract(f.taskID)
	if _, _, err := f.svc.CreateValidation(f.taskID, ct.ID, sha1x); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.svc.View(val.ID); v.MergeReady {
		t.Fatalf("an older validation must not be merge-ready")
	}
}

func TestCreateValidation_SingleFlightPerTask(t *testing.T) {
	f := newFixture(t)
	val, _ := f.started(t)
	if _, _, err := f.svc.CreateValidation(f.taskID, val.ContractID, sha1x); !errors.Is(err, ErrValidationOpen) {
		t.Fatalf("err = %v, want ErrValidationOpen", err)
	}
}

func TestWriteBlocked_OnlyDaemonReasonsAndNeverPass(t *testing.T) {
	f := newFixture(t)
	val, _ := f.started(t)
	if err := f.svc.WriteBlocked(val.ID, ReasonNoBrowser, "x"); err == nil {
		t.Fatalf("the daemon must not write a validator-only reason")
	}
	if err := f.svc.WriteBlocked(val.ID, ReasonTimeout, "ran out of time"); err != nil {
		t.Fatal(err)
	}
	v, _ := f.svc.View(val.ID)
	if v.Status != StatusBlocked || v.MergeReady || v.Verdict.BlockedReason != ReasonTimeout {
		t.Fatalf("view = %+v", v)
	}
	if err := f.svc.WriteBlocked(val.ID, ReasonCrashed, "again"); !errors.Is(err, ErrVerdictExists) {
		t.Fatalf("a second daemon verdict must be refused: %v", err)
	}
}

func TestFlagScopeViolation_MakesPassNotReady(t *testing.T) {
	f := newFixture(t)
	val, token := f.started(t)
	writeFrame(t, f.scratch)
	if _, err := f.svc.SubmitVerdict(val.ID, token, "run-v", f.verdictJSON(t, val, nil)); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.FlagScopeViolation(val.ID); err != nil {
		t.Fatal(err)
	}
	if v, _ := f.svc.View(val.ID); v.MergeReady || !v.ScopeViolation {
		t.Fatalf("view = %+v", v)
	}
}

func TestReadContractFile_InsideWorktreeOnly(t *testing.T) {
	f := newFixture(t)
	if err := os.WriteFile(filepath.Join(f.scratch, "c.contract.md"), []byte(goodDoc), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := f.svc.ReadContractFile(f.taskID, "c.contract.md"); err != nil || len(b) == 0 {
		t.Fatalf("read = %d bytes, %v", len(b), err)
	}
	if _, err := f.svc.ReadContractFile(f.taskID, "../x"); err == nil {
		t.Fatalf("a path outside the worktree must be refused")
	}
}
