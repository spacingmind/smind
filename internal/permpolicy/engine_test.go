package permpolicy

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/store"
)

func engineStore(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "smind.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func writePolicy(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
}

func newTestEngine(t *testing.T, on bool, content string) (*Engine, *store.Store, string) {
	t.Helper()
	s := engineStore(t)
	path := filepath.Join(t.TempDir(), "policy.yaml")
	if content != "" {
		writePolicy(t, path, content)
	}
	e := NewEngine(Options{Path: path, Enabled: func() bool { return on }, Audit: s})
	return e, s, path
}

func auditRows(t *testing.T, s *store.Store) []store.PolicyAuditRow {
	t.Helper()
	rows, err := s.ListPolicyAudit(store.PolicyAuditFilter{Limit: 500})
	if err != nil {
		t.Fatalf("ListPolicyAudit: %v", err)
	}
	return rows
}

func goTestPolicy() string {
	return policyDoc(bashRule(goTestAllow, ActionAllow, "go", "test"))
}

func TestDecide_DisabledIsNoop(t *testing.T) {
	e, s, _ := newTestEngine(t, false, goTestPolicy())
	req := dReq(t, "git stash")
	if d, ok := e.Evaluate(req); ok || d.Action != "" {
		t.Fatalf("Evaluate = %+v, %v; want zero, false", d, ok)
	}
	if n := len(auditRows(t, s)); n != 0 {
		t.Fatalf("audit rows = %d, want 0", n)
	}
	wantAction(t, e.Explain(req).Decision, ActionDeny)
	nilEngine := NewEngine(Options{Path: "x"})
	if _, ok := nilEngine.Evaluate(req); ok {
		t.Fatal("nil Enabled must mean disabled")
	}
}

func TestPolicyLoad_InvalidFileMeansFloorPlusAsk(t *testing.T) {
	bad := map[string]string{
		"garbage": "{{{ not yaml",
		"regex":   policyDoc(bashRule(goTestAllow, ActionAllow, "go", "test")) + "  regex: \".*\"\n",
	}
	for name, content := range bad {
		t.Run(name, func(t *testing.T) {
			e, _, _ := newTestEngine(t, true, content)
			st := e.Status()
			if st.Valid || st.Error == "" || !st.Exists {
				t.Fatalf("Status = %+v, want invalid with error", st)
			}
			if e.Rules() != nil {
				t.Fatal("Rules of an invalid policy must be nil")
			}
			d, _ := e.Evaluate(dReq(t, "git stash"))
			wantAction(t, d, ActionDeny)
			d, _ = e.Evaluate(dReq(t, "go test ./..."))
			wantAction(t, d, ActionAsk)
		})
	}
}

func TestPolicyLoad_MissingFileIsEmptyValid(t *testing.T) {
	e, _, _ := newTestEngine(t, true, "")
	st := e.Status()
	if !st.Valid || st.Exists || st.Rules != 0 {
		t.Fatalf("Status = %+v", st)
	}
	d, _ := e.Evaluate(dReq(t, "go test ./..."))
	wantAction(t, d, ActionAsk)
}

func TestPolicyReload_PicksUpEditWithoutRestart(t *testing.T) {
	e, _, path := newTestEngine(t, true, goTestPolicy())
	req := dReq(t, "go test ./...")
	d, _ := e.Evaluate(req)
	wantAction(t, d, ActionAllow)

	writePolicy(t, path, "version: 1\nrules: []\n")
	d, _ = e.Evaluate(req)
	wantAction(t, d, ActionAsk)

	writePolicy(t, path, "garbage: [")
	d, _ = e.Evaluate(req)
	wantAction(t, d, ActionAsk)
	if e.Status().Valid {
		t.Fatal("garbage file must be invalid")
	}

	writePolicy(t, path, goTestPolicy())
	d, _ = e.Evaluate(req)
	wantAction(t, d, ActionAllow)
	if st := e.Status(); !st.Valid || st.Rules != 1 || st.Sha == "" {
		t.Fatalf("Status = %+v", st)
	}
}

func TestAudit_EveryEvaluationAppendsRow(t *testing.T) {
	e, s, _ := newTestEngine(t, true, goTestPolicy())
	cases := []struct {
		cmd, decision, rule, id string
	}{
		{"go test ./...", "allow", goTestAllow, "r-allow"},
		{"git stash", "deny", "no-stash", "r-deny"},
		{"make build", "ask", "", "r-ask"},
	}
	for _, c := range cases {
		req := dReq(t, c.cmd)
		req.RequestID, req.Seat, req.RunID, req.TaskID = c.id, "lead", "run-1", 7
		e.Evaluate(req)
	}
	rows := auditRows(t, s)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3", len(rows))
	}
	wantSha := e.Status().Sha
	for _, c := range cases {
		var got *store.PolicyAuditRow
		for i := range rows {
			if rows[i].RequestID == c.id {
				got = &rows[i]
			}
		}
		if got == nil {
			t.Fatalf("no row for %s", c.id)
		}
		if got.Decision != c.decision || got.RuleID != c.rule || got.PolicySha != wantSha ||
			got.Seat != "lead" || got.RunID != "run-1" || got.TaskID != 7 || got.LatencyMs < 0 {
			t.Errorf("%s: row = %+v", c.id, *got)
		}
	}
}

func TestAudit_HumanAndTimeoutOutcomeLinkedByRequestID(t *testing.T) {
	e, s, _ := newTestEngine(t, true, goTestPolicy())
	req := dReq(t, "make build")
	req.RequestID = "req-9"
	e.Evaluate(req)
	e.RecordOutcome(req, "ask_timeout_deny")
	e.RecordOutcome(req, "ask_human_allow")
	e.RecordOutcome(req, "allow")
	rows := auditRows(t, s)
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (ask + 2 outcomes)", len(rows))
	}
	got := map[string]bool{}
	for _, r := range rows {
		if r.RequestID != "req-9" {
			t.Errorf("request id = %q", r.RequestID)
		}
		got[r.Decision] = true
	}
	for _, d := range []string{"ask", "ask_timeout_deny", "ask_human_allow"} {
		if !got[d] {
			t.Errorf("missing decision %s", d)
		}
	}
}

func TestAudit_RedactsAndElides(t *testing.T) {
	e, s, _ := newTestEngine(t, true, goTestPolicy())
	token := "ghp_" + strings.Repeat("a1B2", 9)
	bearer := "Zx9Qw8Er7Ty6Ui5Op4As3Df2"
	body := "SECRET-HEREDOC-BODY-" + strings.Repeat("x", 600)
	cmd := "curl -H 'Authorization: Bearer " + bearer + "' -d " + token + " x\ncat <<EOF\n" + body + "\nEOF"
	req := dReq(t, cmd)
	e.Evaluate(req)
	rows := auditRows(t, s)
	if len(rows) != 1 {
		t.Fatalf("rows = %d", len(rows))
	}
	tgt := rows[0].Target
	for _, bad := range []string{token, bearer, "SECRET-HEREDOC-BODY"} {
		if strings.Contains(tgt, bad) {
			t.Errorf("target leaks %q: %q", bad, tgt)
		}
	}
	if !strings.Contains(tgt, "[heredoc") {
		t.Errorf("target lacks heredoc marker: %q", tgt)
	}
	if len(tgt) > 512 {
		t.Errorf("target = %d bytes", len(tgt))
	}
}

type failSink struct{ n int }

func (f *failSink) AppendPolicyAudit(store.PolicyAuditRow) (int64, error) {
	f.n++
	return 0, errors.New("disk full")
}

func TestAudit_AuditFailureDowngradesAllowToAsk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy.yaml")
	writePolicy(t, path, goTestPolicy())
	sink := &failSink{}
	e := NewEngine(Options{Path: path, Enabled: func() bool { return true }, Audit: sink, Now: time.Now})
	d, ok := e.Evaluate(dReq(t, "go test ./..."))
	if !ok || d.Action != ActionAsk || !strings.Contains(strings.Join(d.Reasons, ";"), "audit unavailable") {
		t.Fatalf("allow with failing audit = %+v", d)
	}
	d, _ = e.Evaluate(dReq(t, "git stash"))
	wantAction(t, d, ActionDeny)
	d, _ = e.Evaluate(dReq(t, "make build"))
	wantAction(t, d, ActionAsk)
	if sink.n != 3 {
		t.Fatalf("append attempts = %d, want 3", sink.n)
	}
}

func TestExplain_NoSideEffects(t *testing.T) {
	e, s, _ := newTestEngine(t, true, goTestPolicy())
	x := e.Explain(dReq(t, "go test ./..."))
	wantAction(t, x.Decision, ActionAllow)
	if !x.Status.Enabled || !x.Status.Valid {
		t.Fatalf("Status = %+v", x.Status)
	}
	if n := len(auditRows(t, s)); n != 0 {
		t.Fatalf("rows after Explain = %d", n)
	}
}

func TestAudit_ReasonsAreRedacted(t *testing.T) {
	skipPOSIXPathsOnWindows(t)
	e, s, _ := newTestEngine(t, true, goTestPolicy())
	token := "ghp_" + strings.Repeat("a1B2", 9)
	req := dReq(t, "go test ./... && curl -H 'Authorization: Bearer "+token+"' https://x")
	d, ok := e.Evaluate(req)
	if !ok || d.Action != ActionAsk {
		t.Fatalf("Evaluate = %+v, %v; want ask", d, ok)
	}
	if !strings.Contains(strings.Join(d.Reasons, " "), "not covered") {
		t.Fatalf("setup: Reasons = %q, want the uncovered-segment reason", d.Reasons)
	}
	rows := auditRows(t, s)
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(rows))
	}
	if strings.Contains(rows[0].Reasons, token) || strings.Contains(rows[0].Reasons, "a1B2a1B2") {
		t.Errorf("stored reasons leak the token: %q", rows[0].Reasons)
	}
	if rows[0].Reasons == "" || len(rows[0].Reasons) > 1024 {
		t.Errorf("stored reasons = %q", rows[0].Reasons)
	}
}
