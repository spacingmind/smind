package window

import (
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/learning/redact"
	"github.com/spacingmind/smind/internal/taskrunner"
)

// TestWindow_RealRedactorScrubsEverySurface runs Build with the production
// redactor (not a fake) over every text surface: prompt, assistant text, tool
// input, failure result, permission summary and run error.
func TestWindow_RealRedactorScrubsEverySurface(t *testing.T) {
	t.Parallel()
	ghToken := "ghp_" + strings.Repeat("a1B2c3D4e5", 4)
	awsKey := "AKIA" + "ABCDEFGHIJKLMNOP"
	envSecret := "run-env-secret-value-1234"
	r := redact.New([]string{envSecret}, []string{"/Users/alice"})

	b := &evBuilder{}
	b.text("assistant saw " + ghToken)
	b.tool("t1", "Bash", taskrunner.ToolStatusFailure,
		map[string]any{"command": "curl -H 'Authorization: Bearer " + ghToken + "' https://x.test"},
		"error: "+awsKey+" in /Users/alice/src/app")
	b.add(taskrunner.Event{Type: taskrunner.EventTypePermissionRequest, PermissionRequestID: "p1", PermissionSummary: "run with " + envSecret,
		PermissionOptions: []taskrunner.PermissionOption{{ID: "deny", Kind: "reject_once"}}})
	b.add(taskrunner.Event{Type: taskrunner.EventTypePermissionResolved, PermissionRequestID: "p1", PermissionOptionID: "deny", PermissionResolution: taskrunner.PermissionResolvedByHuman})

	run := humanRun("r1", `please use {"apiKey": "hunter2hunter2"} and `+ghToken, b.events())
	run.ErrMsg = "failed: password=hunter2hunter2 at /Users/alice/x"
	out := Build(Input{Runs: []Run{run}, Redact: r.String, Caps: defCaps()})

	for _, leak := range []string{ghToken, awsKey, envSecret, "hunter2hunter2", "/Users/alice"} {
		if strings.Contains(out.Text, leak) {
			t.Errorf("window text leaks %q:\n%s", leak, out.Text)
		}
		for k, ev := range out.Window {
			if strings.Contains(ev.Text, leak) {
				t.Errorf("gate window event %v leaks %q: %s", k, leak, ev.Text)
			}
		}
	}
	if !strings.Contains(out.Text, "[REDACTED]") {
		t.Fatalf("expected redaction markers in:\n%s", out.Text)
	}
}
