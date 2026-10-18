package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func boolp(b bool) *bool { return &b }

func TestTaskModeParse(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    taskModeRequest
		wantErr string
	}{
		{"mode only", []string{"r1", "plan"}, taskModeRequest{RunID: "r1", ModeID: "plan"}, ""},
		{"auto-accept on", []string{"r1", "--auto-accept", "on"}, taskModeRequest{RunID: "r1", AutoAccept: boolp(true)}, ""},
		{"auto-accept=off", []string{"r1", "--auto-accept=off"}, taskModeRequest{RunID: "r1", AutoAccept: boolp(false)}, ""},
		{"flag first", []string{"--auto-accept", "ON", "r1"}, taskModeRequest{RunID: "r1", AutoAccept: boolp(true)}, ""},
		{"both", []string{"r1", "acceptEdits", "--auto-accept", "off"}, taskModeRequest{RunID: "r1", ModeID: "acceptEdits", AutoAccept: boolp(false)}, ""},
		{"no run id", nil, taskModeRequest{}, "expected a run id"},
		{"nothing to change", []string{"r1"}, taskModeRequest{}, "give a mode"},
		{"too many positionals", []string{"r1", "plan", "extra"}, taskModeRequest{}, "at most one mode"},
		{"bad auto-accept value", []string{"r1", "--auto-accept", "maybe"}, taskModeRequest{}, "on or off"},
		{"auto-accept without value", []string{"r1", "--auto-accept"}, taskModeRequest{}, "needs on or off"},
		{"auto-accept twice", []string{"r1", "--auto-accept", "on", "--auto-accept", "off"}, taskModeRequest{}, "twice"},
		{"unknown flag", []string{"r1", "--yes"}, taskModeRequest{}, "unknown flag"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseTaskMode(tt.args)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to contain %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.RunID != tt.want.RunID || got.ModeID != tt.want.ModeID || (got.AutoAccept == nil) != (tt.want.AutoAccept == nil) ||
				(got.AutoAccept != nil && *got.AutoAccept != *tt.want.AutoAccept) {
				t.Fatalf("parsed = %+v, want %+v", got, tt.want)
			}
		})
	}
}

// recorder is a daemonCaller that records what `task mode` sends.
type recorder struct {
	dialed bool
	method string
	params map[string]any
}

func (r *recorder) Call(_ context.Context, method string, params, result any) error {
	r.method = method
	r.params = params.(map[string]any)
	if out, ok := result.(*struct {
		PermissionMode string `json:"permissionMode"`
		AutoAccept     bool   `json:"autoAccept"`
	}); ok {
		out.PermissionMode = "accept_edits"
		if v, ok := r.params["autoAccept"].(bool); ok {
			out.AutoAccept = v
		}
	}
	return nil
}
func (r *recorder) Close() error { return nil }

func (r *recorder) dial(context.Context) (daemonCaller, error) {
	r.dialed = true
	return r, nil
}

func modeRun(args []string, stdin string, interactive bool, dial func(context.Context) (daemonCaller, error)) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = runTaskMode(args, strings.NewReader(stdin), &out, &errOut, interactive, dial)
	return code, out.String(), errOut.String()
}

// TestTaskModeDeclaresHumanSource: the daemon decides from the declared source,
// so the human CLI must send "human" explicitly rather than lean on a default.
func TestTaskModeDeclaresHumanSource(t *testing.T) {
	rec := &recorder{}
	code, out, errOut := modeRun([]string{"run-1", "accept_edits", "--auto-accept", "off"}, "", false, rec.dial)
	if code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if rec.method != "run.setPermissionMode" || rec.params["source"] != "human" || rec.params["runId"] != "run-1" ||
		rec.params["modeId"] != "accept_edits" || rec.params["autoAccept"] != false {
		t.Fatalf("sent %s %v", rec.method, rec.params)
	}
	if !strings.Contains(out, `run run-1: permission mode "accept_edits", auto-accept off`) {
		t.Fatalf("stdout = %q", out)
	}
	// Mode-only changes do not send an autoAccept key.
	rec = &recorder{}
	if code, _, errOut := modeRun([]string{"run-1", "plan"}, "", false, rec.dial); code != 0 {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if _, has := rec.params["autoAccept"]; has {
		t.Fatalf("mode-only call sent autoAccept: %v", rec.params)
	}
}

// TestTaskModeAutoAcceptOnNeedsATerminalAndAYes: turning auto-accept on is the
// one escalation, and the speed bump is a person typing yes at a terminal.
func TestTaskModeAutoAcceptOnNeedsATerminalAndAYes(t *testing.T) {
	// No terminal (an agent's shell, a pipe): refused before the daemon is
	// even dialed, even if the input says yes.
	rec := &recorder{}
	code, _, errOut := modeRun([]string{"run-1", "--auto-accept", "on"}, "yes\n", false, rec.dial)
	if code != 1 || rec.dialed || !strings.Contains(errOut, "person at a terminal") {
		t.Fatalf("no terminal: code %d dialed %v stderr %q", code, rec.dialed, errOut)
	}
	// A terminal, but anything other than yes.
	for _, answer := range []string{"", "\n", "y\n", "no\n", "yes please\n"} {
		rec = &recorder{}
		code, out, errOut := modeRun([]string{"run-1", "--auto-accept", "on"}, answer, true, rec.dial)
		if code != 1 || rec.dialed || !strings.Contains(errOut, "nothing changed") || !strings.Contains(out, "approve every tool call") {
			t.Errorf("answer %q: code %d dialed %v out %q stderr %q", answer, code, rec.dialed, out, errOut)
		}
	}
	// Yes at a terminal goes through, as human.
	rec = &recorder{}
	code, out, errOut := modeRun([]string{"run-1", "--auto-accept", "on"}, "yes\n", true, rec.dial)
	if code != 0 || rec.params["autoAccept"] != true || rec.params["source"] != "human" || !strings.Contains(out, "auto-accept on") {
		t.Fatalf("confirmed: code %d params %v out %q stderr %q", code, rec.params, out, errOut)
	}
	// Turning it off, or changing to a mode, never asks.
	for _, args := range [][]string{{"run-1", "--auto-accept", "off"}, {"run-1", "plan"}} {
		rec = &recorder{}
		if code, _, errOut := modeRun(args, "", false, rec.dial); code != 0 || !rec.dialed {
			t.Errorf("%v: code %d dialed %v stderr %q", args, code, rec.dialed, errOut)
		}
	}
}

func TestTaskModeUsageErrorsExit2WithoutDialing(t *testing.T) {
	rec := &recorder{}
	code, _, errOut := modeRun([]string{"run-1"}, "", true, rec.dial)
	if code != 2 || rec.dialed || !strings.Contains(errOut, "usage: smind task mode") {
		t.Fatalf("code %d dialed %v stderr %q", code, rec.dialed, errOut)
	}
}

func TestTaskModePrintsTheDaemonsRefusal(t *testing.T) {
	code, _, errOut := modeRun([]string{"run-1", "plan"}, "", false, func(context.Context) (daemonCaller, error) {
		return failing{errors.New("run.setPermissionMode: run not found")}, nil
	})
	if code != 1 || !strings.Contains(errOut, "task mode: run.setPermissionMode: run not found") {
		t.Fatalf("code %d stderr %q", code, errOut)
	}
}

type failing struct{ err error }

func (f failing) Call(context.Context, string, any, any) error { return f.err }
func (f failing) Close() error                                 { return nil }

// TestTaskModeAgainstARealDaemon runs the command against a real daemon and a
// live ACP run: a human may pick an auto-approving mode and flip auto-accept
// (the "43 approvals" fix), and the run's own state shows it.
func TestTaskModeAgainstARealDaemon(t *testing.T) {
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	srvURL := newConfigOptionTestEnvArgs(t, home, "modes:session")
	writeTestConfig(t, home, srvURL)
	runID := startTestRun(t, srvURL, "glm", "hang")

	run := func(stdin string, interactive bool, args ...string) (int, string, string) {
		return modeRun(append([]string{runID}, args...), stdin, interactive, dialDaemonCaller)
	}

	if code, out, errOut := run("", false, "bypass_permissions"); code != 0 || !strings.Contains(out, `permission mode "bypass_permissions"`) {
		t.Fatalf("a human picking an auto-approving mode: code %d out %q stderr %q", code, out, errOut)
	}
	if code, out, errOut := run("yes\n", true, "--auto-accept", "on"); code != 0 || !strings.Contains(out, "auto-accept on") {
		t.Fatalf("auto-accept on: code %d out %q stderr %q", code, out, errOut)
	}
	if code, out, errOut := run("", false, "accept_edits", "--auto-accept", "off"); code != 0 ||
		!strings.Contains(out, `permission mode "accept_edits", auto-accept off`) {
		t.Fatalf("back to asking: code %d out %q stderr %q", code, out, errOut)
	}
	// A run that does not exist is the daemon's error, prefixed.
	if code, _, errOut := modeRun([]string{"no-such-run", "plan"}, "", false, dialDaemonCaller); code != 1 || !strings.Contains(errOut, "task mode:") {
		t.Fatalf("unknown run: code %d stderr %q", code, errOut)
	}
}
