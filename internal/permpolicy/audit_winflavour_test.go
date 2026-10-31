package permpolicy

import (
	"strings"
	"testing"
)

func TestAuditWindows_ReasonsAreRedacted(t *testing.T) {
	e, s, _ := newTestEngine(t, true, goTestPolicy())
	_, base := winEnv(t)
	token := "ghp_" + strings.Repeat("a1B2", 9)
	req := base
	req.Command = "go test ./... && curl -H 'Authorization: Bearer " + token + "' https://x"
	req.TaskBranch, req.Seat, req.Workspace = "task-branch", "lead", "smind"
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
