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

// recorder is a daemonCaller that records every call `task mode` makes and, like
// the daemon, refuses the orchestrator source an auto-approving mode.
type recorder struct {
	dialed bool
	calls  []map[string]any
	// autoApproving names the modes the fake daemon refuses to non-human sources.
	autoApproving map[string]bool
}

func (r *recorder) Call(_ context.Context, method string, params, result any) error {
	p := params.(map[string]any)
	r.calls = append(r.calls, p)
	if p["source"] != "human" && r.autoApproving[p["modeId"].(string)] {
		return errors.New("run.setPermissionMode: a non-human caller may not pick an auto-approving permission mode (ADR-0019 decision 6)")
	}
	if out, ok := result.(*permissionState); ok {
		out.PermissionMode, _ = p["modeId"].(string)
		if v, ok := p["autoAccept"].(bool); ok {
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

// last is the final call: the human-source one that decides the outcome.
func (r *recorder) last() map[string]any { return r.calls[len(r.calls)-1] }

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
	last := rec.last()
	if last["source"] != "human" || last["runId"] != "run-1" || last["modeId"] != "accept_edits" || last["autoAccept"] != false {
		t.Fatalf("final call = %v", last)
	}
	if !strings.Contains(out, `run run-1: permission mode "accept_edits", auto-accept off`) {
		t.Fatalf("stdout = %q", out)
	}
	// The mode-safety probe is the orchestrator source and never carries autoAccept.
	if first := rec.calls[0]; first["source"] != "orchestrator" || first["modeId"] != "accept_edits" || first["autoAccept"] != nil {
		t.Fatalf("probe = %v, want an orchestrator-source mode-only call", first)
	}
	// Mode-less changes send no mode and no probe.
	rec = &recorder{}
	if code, _, errOut := modeRun([]string{"run-1", "--auto-accept", "off"}, "", false, rec.dial); code != 0 || len(rec.calls) != 1 {
		t.Fatalf("code %d calls %v stderr %q", code, rec.calls, errOut)
	}
	if _, has := rec.last()["modeId"]; has {
		t.Fatalf("auto-accept-only call sent a mode: %v", rec.last())
	}
}

// TestTaskModeAutoAcceptOnNeedsATerminalAndAYes: turning auto-accept on, or
// picking a mode the daemon would refuse an agent, needs a person typing yes at
// a terminal; everything else does not ask.
func TestTaskModeAutoAcceptOnNeedsATerminalAndAYes(t *testing.T) {
	escalations := map[string][]string{
		"auto-accept on":      {"run-1", "--auto-accept", "on"},
		"auto-approving mode": {"run-1", "bypass_permissions"},
		"both":                {"run-1", "bypass_permissions", "--auto-accept", "on"},
	}
	for name, args := range escalations {
		t.Run(name, func(t *testing.T) {
			// No terminal (an agent's shell, a pipe): refused, even if the input says yes,
			// and nothing is changed as the human.
			rec := &recorder{autoApproving: map[string]bool{"bypass_permissions": true}}
			code, _, errOut := modeRun(args, "yes\n", false, rec.dial)
			if code != 1 || !strings.Contains(errOut, "person at a terminal") {
				t.Fatalf("no terminal: code %d stderr %q", code, errOut)
			}
			for _, c := range rec.calls {
				if c["source"] == "human" {
					t.Fatalf("a human-source call was made without confirmation: %v", rec.calls)
				}
			}
			// A terminal, but anything other than yes.
			for _, answer := range []string{"", "\n", "y\n", "no\n", "yes please\n"} {
				rec = &recorder{autoApproving: map[string]bool{"bypass_permissions": true}}
				code, out, errOut := modeRun(args, answer, true, rec.dial)
				if code != 1 || !strings.Contains(errOut, "not confirmed") || !strings.Contains(out, `Type "yes"`) {
					t.Errorf("answer %q: code %d out %q stderr %q", answer, code, out, errOut)
				}
				for _, c := range rec.calls {
					if c["source"] == "human" {
						t.Errorf("answer %q: a human-source call was made: %v", answer, rec.calls)
					}
				}
			}
			// Yes at a terminal goes through, as human.
			rec = &recorder{autoApproving: map[string]bool{"bypass_permissions": true}}
			code, out, errOut := modeRun(args, "yes\n", true, rec.dial)
			if code != 0 || rec.last()["source"] != "human" {
				t.Fatalf("confirmed: code %d calls %v out %q stderr %q", code, rec.calls, out, errOut)
			}
		})
	}
	// Turning it off, or a mode the daemon allows an agent, never asks.
	for _, args := range [][]string{{"run-1", "--auto-accept", "off"}, {"run-1", "plan"}, {"run-1", "accept_edits", "--auto-accept=off"}} {
		rec := &recorder{autoApproving: map[string]bool{"bypass_permissions": true}}
		if code, out, errOut := modeRun(args, "", false, rec.dial); code != 0 || strings.Contains(out, "yes") {
			t.Errorf("%v: code %d out %q stderr %q", args, code, out, errOut)
		}
	}
	// A mode that cannot be checked is treated as auto-approving (fail closed).
	rec := &recorder{}
	rec.autoApproving = map[string]bool{"mystery": true}
	if code, _, _ := modeRun([]string{"run-1", "mystery"}, "", false, rec.dial); code != 1 {
		t.Fatalf("an unverifiable mode without a terminal: code %d, want 1", code)
	}
}

func TestTaskModeUsageErrorsExit2WithoutDialing(t *testing.T) {
	rec := &recorder{}
	code, _, errOut := modeRun([]string{"run-1"}, "", true, rec.dial)
	if code != 2 || rec.dialed || !strings.Contains(errOut, "usage: smind task mode") {
		t.Fatalf("code %d dialed %v stderr %q", code, rec.dialed, errOut)
	}
}

// A daemon error is printed, prefixed. A mode that cannot be checked (here the
// whole daemon refuses) is confirmed first, then the human call's error shows.
func TestTaskModePrintsTheDaemonsRefusal(t *testing.T) {
	dial := func(context.Context) (daemonCaller, error) {
		return failing{errors.New("run.setPermissionMode: run not found")}, nil
	}
	if code, _, errOut := modeRun([]string{"run-1", "--auto-accept", "off"}, "", false, dial); code != 1 ||
		!strings.Contains(errOut, "task mode: run.setPermissionMode: run not found") {
		t.Fatalf("auto-accept off: code %d stderr %q", code, errOut)
	}
	code, _, errOut := modeRun([]string{"run-1", "plan"}, "yes\n", true, dial)
	if code != 1 || !strings.Contains(errOut, "task mode: run.setPermissionMode: run not found") {
		t.Fatalf("mode: code %d stderr %q", code, errOut)
	}
	// Without a terminal the refusal still carries the daemon's reason.
	if code, _, errOut := modeRun([]string{"run-1", "plan"}, "", false, dial); code != 1 || !strings.Contains(errOut, "run not found") {
		t.Fatalf("mode without a terminal: code %d stderr %q", code, errOut)
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

	// An auto-approving mode needs the human at a terminal ...
	if code, _, errOut := run("yes\n", false, "bypass_permissions"); code != 1 || !strings.Contains(errOut, "person at a terminal") {
		t.Fatalf("auto-approving mode without a terminal: code %d stderr %q", code, errOut)
	}
	// ... and then a human may pick it.
	if code, out, errOut := run("yes\n", true, "bypass_permissions"); code != 0 || !strings.Contains(out, `permission mode "bypass_permissions"`) {
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
