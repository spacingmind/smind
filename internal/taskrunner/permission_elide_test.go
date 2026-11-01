package taskrunner

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/codex"
)

const elideSecret = "SECRET_TOKEN_123"

func elideCmd() string {
	return "cat <<'EOF' > notes.md\nkey=" + elideSecret + " ghp_abcdefghijklmnopqrstuvwxyz0123456789\nEOF\n&& git status"
}

func assertElided(t *testing.T, d *PermissionDetail) {
	t.Helper()
	if d == nil || d.Kind != "execute" {
		t.Fatalf("detail = %+v", d)
	}
	if strings.Contains(d.Command, elideSecret) || !strings.Contains(d.Command, "[heredoc ") || !strings.Contains(d.Command, "&& git status") {
		t.Errorf("Command = %q", d.Command)
	}
	if len(d.Heredocs) != 1 || d.Heredocs[0].Index != 0 || d.Heredocs[0].Bytes == 0 {
		t.Errorf("Heredocs = %+v", d.Heredocs)
	}
	raw, _ := json.Marshal(d)
	if strings.Contains(string(raw), elideSecret) {
		t.Errorf("serialised detail carries the body: %s", raw)
	}
	if !strings.Contains(d.PolicyCommand, elideSecret) || strings.Contains(d.PolicyCommand, "[heredoc ") {
		t.Errorf("PolicyCommand must be the raw command: %q", d.PolicyCommand)
	}
	if len(d.RevealBodies) != 1 || !strings.Contains(d.RevealBodies[0], elideSecret) {
		t.Errorf("the daemon must keep the raw body: %+v", d)
	}
}

func TestExecuteDetailElidesHeredoc_ACP(t *testing.T) {
	t.Parallel()
	tc, _ := json.Marshal(map[string]any{"kind": "execute", "title": "Run " + elideCmd(), "rawInput": map[string]any{"command": elideCmd()}})
	assertElided(t, BuildPermissionDetail(t.TempDir(), tc))
	if s := summarizeACPToolCallIn(t.TempDir(), tc); strings.Contains(s, elideSecret) {
		t.Errorf("ACP summary carries the body: %q", s)
	}
}

func TestExecuteDetailElidesHeredoc_ACPTruncatedTitle(t *testing.T) {
	t.Parallel()
	tc, _ := json.Marshal(map[string]any{"kind": "execute", "title": "Run cat <<'EOF' > notes.md\nkey=" + elideSecret + "…", "rawInput": map[string]any{"command": elideCmd()}})
	if s := summarizeACPToolCallIn(t.TempDir(), tc); strings.Contains(s, elideSecret) {
		t.Errorf("a title cut mid-body still carries it: %q", s)
	}
}

func TestExecuteDetailElidesHeredoc_Claude(t *testing.T) {
	t.Parallel()
	assertElided(t, claudeBashDetail(t.TempDir(), claudeBashReq(elideCmd())))
}

type recDecider struct{ summary string }

func (r *recDecider) Decide(_ context.Context, summary string, _ []PermissionOption) (string, error) {
	r.summary = summary
	return "allow", nil
}

func TestExecuteDetailElidesHeredoc_CodexSummary(t *testing.T) {
	t.Parallel()
	rec := &recDecider{}
	a := codexDeciderAdapter{decider: rec}
	if _, err := a.DecideCommandExecution(context.Background(), codex.CommandExecutionApprovalRequest{Command: elideCmd()}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rec.summary, elideSecret) || !strings.Contains(rec.summary, "[heredoc ") {
		t.Errorf("codex summary = %q", rec.summary)
	}
}

func TestExecuteDetail_NoHeredocUnchanged(t *testing.T) {
	t.Parallel()
	d := &PermissionDetail{Kind: "execute"}
	d.setExecuteCommand("go test ./...")
	if d.Command != "go test ./..." || len(d.Heredocs) != 0 {
		t.Errorf("detail = %+v", d)
	}
}

// A command whose raw text exceeds the detail cap stays PolicyTruncated even
// when its elided form is short: the policy engine treats a truncated command
// as "ask" before any rule or floor, and eliding a long body must not change
// that decision. The card itself is not marked truncated: what it shows is whole.
func TestExecuteDetail_LongBodyStaysTruncated(t *testing.T) {
	t.Parallel()
	d := &PermissionDetail{Kind: "execute"}
	d.setExecuteCommand("cat <<EOF\n" + strings.Repeat("x", detailCommandMaxBytes*2) + "\nEOF\n")
	if len(d.Command) >= detailCommandMaxBytes || d.Truncated || !d.PolicyTruncated {
		t.Errorf("Command %d bytes, Truncated=%v PolicyTruncated=%v", len(d.Command), d.Truncated, d.PolicyTruncated)
	}
}
