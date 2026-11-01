package runs

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spacingmind/smind/internal/permpolicy"
	"github.com/spacingmind/smind/internal/taskrunner"
)

const askNpmPolicy = `version: 1
rules:
  - id: ask-npm
    action: ask
    why: a human reads every npm run
    match:
      tool: Bash
      command: {argv0: npm}
`

// TestRegistry_PermissionCommandScenarios_CarryPolicyBlock proves the fake
// agent's "permission-command" scenarios reach the phone the way the mobile
// smoke needs them: an execute request whose detail.command is the elided
// multi-line command (the body is out of band, listed in detail.heredocs),
// judged on the RAW command (the rule still matches the npm after the heredoc),
// and whose detail.policy carries the matched rule id and
// the normalised command (heredoc elided) for one, and an empty rule id for
// the other (no rule matched: ask).
func TestRegistry_PermissionCommandScenarios_CarryPolicyBlock(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		scenario string
		wantRule string
		wantCmd  string
	}{
		{"permission-command", "ask-npm", "cat <<EOF [heredoc 1056 bytes] > notes/release-checklist.md\n&& git status && npm test -- --run src/some/very/long/path/to/a/test/file.test.ts"},
		{"permission-command-nomatch", "", "cat <<EOF [heredoc 1056 bytes] > notes/release-checklist.md\n&& git status && ./scripts/zzz-unlisted-check --run src/some/very/long/path/to/a/test/file.sh"},
	} {
		t.Run(tc.scenario, func(t *testing.T) {
			t.Parallel()
			wm, st := newTestWorkspaceManager(t)
			task := newTestTask(t, wm, tc.scenario)
			reg := newTestRegistry(t, st)
			path := filepath.Join(t.TempDir(), "policy.yaml")
			if err := os.WriteFile(path, []byte(askNpmPolicy), 0o600); err != nil {
				t.Fatal(err)
			}
			reg.SetPolicyEngine(permpolicy.NewEngine(permpolicy.Options{
				Path: path, Enabled: func() bool { return true }, Audit: st,
				Home: t.TempDir(), SmindHome: t.TempDir(),
			}))
			runID, err := reg.Start(context.Background(), wm, newTestRunner(wm), task.ID, 0, taskrunner.ProviderGLM, "hi", taskrunner.PermissionSettings{}, "")
			if err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			req := waitForPermissionRequest(t, reg, runID, 5*time.Second)
			d := req.PermissionDetail
			if d == nil || d.Kind != "execute" {
				t.Fatalf("detail = %+v, want an execute detail", d)
			}
			if d.Command != tc.wantCmd {
				t.Fatalf("detail.command = %q, want the elided command %q", d.Command, tc.wantCmd)
			}
			if len(d.Heredocs) != 1 || d.Heredocs[0].Index != 0 || d.Heredocs[0].Bytes != 1056 {
				t.Fatalf("detail.heredocs = %+v, want the one 1056-byte body", d.Heredocs)
			}
			p := d.Policy
			if p == nil {
				t.Fatalf("detail.policy is nil: %+v", d)
			}
			if p.RuleID != tc.wantRule || p.Action != "ask" {
				t.Fatalf("detail.policy = %+v, want rule %q and action ask", p, tc.wantRule)
			}
			if !strings.Contains(p.Normalized, "[heredoc") || strings.Contains(p.Normalized, "step 07") {
				t.Fatalf("detail.policy.normalized = %q, want the command with the heredoc body elided", p.Normalized)
			}
			if err := reg.RespondPermission(runID, req.PermissionRequestID, "deny-1"); err != nil {
				t.Fatalf("RespondPermission() error = %v", err)
			}
			waitForStatus(t, reg, runID, StatusDone, 5*time.Second)
		})
	}
}
