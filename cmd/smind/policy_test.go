package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spacingmind/smind/internal/permpolicy"
)

func policyPath(home string) string { return filepath.Join(home, "steward", "policy.yaml") }

// policyHostGOOS pins the OS the policy CLI believes it runs on, so the CLI
// tests keep exercising the subcommands on a Windows runner.
func policyHostGOOS(t *testing.T, goos string) {
	t.Helper()
	old := permpolicy.HostGOOS
	permpolicy.HostGOOS = goos
	t.Cleanup(func() { permpolicy.HostGOOS = old })
}

func TestCLI_PolicyInitWritesTemplateWithZeroAllowRules(t *testing.T) {
	policyHostGOOS(t, "linux")
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	var out, errb bytes.Buffer
	if code := runPolicy(context.Background(), []string{"init"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errb.String())
	}
	path := policyPath(home)
	if !strings.Contains(out.String(), path) {
		t.Errorf("output %q does not name %s", out.String(), path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if info.Mode().Perm() != 0o600 {
			t.Errorf("file mode = %v, want 0600", info.Mode().Perm())
		}
		dir, err := os.Stat(filepath.Dir(path))
		if err != nil || dir.Mode().Perm() != 0o700 {
			t.Errorf("dir = %v, %v; want 0700", dir, err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pol, err := permpolicy.Load(data)
	if err != nil {
		t.Fatalf("Load(template): %v", err)
	}
	for _, r := range pol.Rules {
		if r.Action == permpolicy.ActionAllow {
			t.Errorf("template has an active allow rule %q", r.ID)
		}
	}
}

func TestCLI_PolicyInitRefusesOverwrite(t *testing.T) {
	policyHostGOOS(t, "linux")
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	path := policyPath(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	mine := []byte("version: 1\nrules: []\n# mine\n")
	if err := os.WriteFile(path, mine, 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	if code := runPolicy(context.Background(), []string{"init"}, &out, &errb); code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "already exists") || !strings.Contains(errb.String(), path) {
		t.Errorf("stderr = %q", errb.String())
	}
	got, _ := os.ReadFile(path)
	if !bytes.Equal(got, mine) {
		t.Errorf("file was modified: %q", got)
	}
}

func TestCLI_PolicyUsageErrors(t *testing.T) {
	policyHostGOOS(t, "linux")
	for _, args := range [][]string{nil, {"bogus"}, {"init", "x"}, {"test"}, {"test", "--task", "x", "ls"}, {"audit", "--limit", "0"}, {"show", "--nope"}} {
		var out, errb bytes.Buffer
		if code := runPolicy(context.Background(), args, &out, &errb); code != 2 {
			t.Errorf("%q: exit %d, want 2 (stderr %q)", args, code, errb.String())
		}
	}
}

// stubPolicyDaemon answers one method with canned JSON and records the params.
func stubPolicyDaemon(t *testing.T, method, reply string, got *map[string]any) {
	t.Helper()
	prev := policyDial
	t.Cleanup(func() { policyDial = prev })
	policyDial = func(context.Context) (wsCaller, func(), error) {
		return func(_ context.Context, m string, params, result any) error {
			if m != method {
				t.Errorf("method = %q, want %q", m, method)
			}
			b, _ := json.Marshal(params)
			_ = json.Unmarshal(b, got)
			return json.Unmarshal([]byte(reply), result)
		}, func() {}, nil
	}
}

func TestCLI_PolicyShowTestAudit(t *testing.T) {
	policyHostGOOS(t, "linux")
	var params map[string]any
	stubPolicyDaemon(t, "policy.get", `{"enabled":true,"valid":true,"exists":true,"path":"/p","sha":"abc","rules":[{"id":"r1","action":"allow","why":"because"}]}`, &params)
	var out, errb bytes.Buffer
	if code := runPolicy(context.Background(), []string{"show"}, &out, &errb); code != 0 {
		t.Fatalf("show exit %d: %s", code, errb.String())
	}
	for _, want := range []string{"engine:  on", "abc", "/p", "r1", "allow", "because"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("show output %q lacks %q", out.String(), want)
		}
	}

	params = nil
	stubPolicyDaemon(t, "policy.test", `{"decision":{"action":"deny","floorId":"no-stash","why":"w","segments":[{"index":0,"text":"git stash","action":"deny","floorId":"no-stash"}]},"status":{"enabled":false}}`, &params)
	out.Reset()
	if code := runPolicy(context.Background(), []string{"test", "--seat", "lead", "git stash"}, &out, &errb); code != 0 {
		t.Fatalf("test exit %d: %s", code, errb.String())
	}
	if params["tool"] != "Bash" || params["seat"] != "lead" || params["command"] != "git stash" {
		t.Errorf("params = %v", params)
	}
	for _, want := range []string{"deny (floor no-stash)", "git stash", "switched off"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("test output %q lacks %q", out.String(), want)
		}
	}

	params = nil
	stubPolicyDaemon(t, "policy.audit", `{"rows":[{"createdAt":"2026-10-31T00:00:00Z","decision":"ask","ruleId":"","seat":"lead","tool":"Bash","target":"ls"}],"next":0}`, &params)
	out.Reset()
	if code := runPolicy(context.Background(), []string{"audit", "--task", "5", "--decision", "ask", "--limit", "3"}, &out, &errb); code != 0 {
		t.Fatalf("audit exit %d: %s", code, errb.String())
	}
	if params["taskId"] != float64(5) || params["decision"] != "ask" || params["limit"] != float64(3) {
		t.Errorf("params = %v", params)
	}
	if !strings.Contains(out.String(), "DECISION") || !strings.Contains(out.String(), "ls") {
		t.Errorf("audit output = %q", out.String())
	}

	out.Reset()
	if code := runPolicy(context.Background(), []string{"audit", "--json"}, &out, &errb); code != 0 || !json.Valid(out.Bytes()) {
		t.Errorf("audit --json: exit %d, output %q", code, out.String())
	}
}

func TestPolicyCLI_WindowsRefusesEverySubcommand(t *testing.T) {
	policyHostGOOS(t, "windows")
	home := t.TempDir()
	t.Setenv("SMIND_HOME", home)
	prev := policyDial
	t.Cleanup(func() { policyDial = prev })
	policyDial = func(context.Context) (wsCaller, func(), error) {
		t.Fatal("policyDial must not be reached when the OS is unsupported")
		return nil, nil, nil
	}
	for _, args := range [][]string{{"init"}, {"show"}, {"test", "git status"}, {"audit"}} {
		var out, errb bytes.Buffer
		if code := runPolicy(context.Background(), args, &out, &errb); code != 1 {
			t.Errorf("%q: exit %d, want 1 (stderr %q)", args, code, errb.String())
		}
		if !strings.Contains(errb.String(), "not supported on Windows yet") {
			t.Errorf("%q: stderr = %q", args, errb.String())
		}
		if out.Len() != 0 {
			t.Errorf("%q: stdout = %q, want empty", args, out.String())
		}
	}
	if _, err := os.Stat(filepath.Join(home, "steward")); !os.IsNotExist(err) {
		t.Errorf("steward dir exists after refused init: %v", err)
	}
}

func TestPolicyCLI_PosixStillWorks(t *testing.T) {
	policyHostGOOS(t, "linux")
	var params map[string]any
	stubPolicyDaemon(t, "policy.test", `{"decision":{"action":"deny","floorId":"no-stash","why":"w","segments":[{"index":0,"text":"git status","action":"deny","floorId":"no-stash"}]},"status":{"enabled":true}}`, &params)
	var out, errb bytes.Buffer
	if code := runPolicy(context.Background(), []string{"test", "git status"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "deny (floor no-stash)") {
		t.Errorf("output = %q", out.String())
	}
}
